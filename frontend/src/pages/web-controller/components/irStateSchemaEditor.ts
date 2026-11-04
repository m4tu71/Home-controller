import type { IRStateFieldCase, IRStateFieldSchema, IRStateFieldType, IRStateSchema, IRStateSchemaIssue } from "../../../api/controller";
import type { IRStateSchemaFieldDraft } from "./IRStateSchemaForm";

export const isExtendedField = (field: IRStateFieldSchema) =>
  ["presence", "step", "allow_empty", "cases"].some((key) => Object.hasOwn(field, key));

export function schemaFieldsFromSchema(schema: IRStateSchema): IRStateSchemaFieldDraft[] {
  return Object.entries(schema).map(([key, field]) => ({ key, type: field.type,
    values: field.values?.join(", ") ?? "", min: field.min === undefined ? "" : String(field.min),
    max: field.max === undefined ? "" : String(field.max), definition: JSON.stringify(field) }));
}
export function fieldFromDraft(field: IRStateSchemaFieldDraft): IRStateFieldSchema {
  if (field.definition) return JSON.parse(field.definition) as IRStateFieldSchema;
  return { type: field.type,
    ...(field.type === "enum" ? { values: field.values.split(",").map((v) => v.trim()).filter(Boolean) } : {}),
    ...(field.min !== "" ? { min: Number(field.min) } : {}),
    ...(field.max !== "" ? { max: Number(field.max) } : {}) };
}
export function schemaFromDrafts(fields: IRStateSchemaFieldDraft[]) {
  const schema: IRStateSchema = Object.create(null);
  const errors: Record<number, string> = {};
  const keys = new Map<string, number>();
  fields.forEach((field, index) => {
    const key = field.key.trim();
    if (!key) { errors[index] = "field名を入力してください"; return; }
    if (keys.has(key)) { errors[index] = errors[keys.get(key)!] = `field名が重複しています: ${key}`; return; }
    keys.set(key, index); schema[key] = fieldFromDraft(field);
  });
  for (const issue of validateStateSchema(schema)) {
    const index = keys.get(issue.field_id);
    if (index !== undefined) errors[index] = [errors[index], issue.case_index === undefined ? issue.message : `case ${issue.case_index + 1}: ${issue.message}`].filter(Boolean).join(" / ");
  }
  return { schema, errors };
}

export function incompatibleConstraintKeys(field: IRStateFieldSchema): string[] {
  return ["values", "min", "max", "step", "allow_empty"].filter((key) => Object.hasOwn(field, key) &&
    ((field.presence === "absent") || (key === "values" ? field.type !== "enum" : key === "allow_empty" ? field.type !== "string" : field.type !== "number")));
}
export function extendField(field: IRStateFieldSchema): IRStateFieldSchema {
  const result = { ...field, presence: field.presence ?? "required" };
  for (const key of incompatibleConstraintKeys(result)) delete result[key as keyof IRStateFieldSchema];
  return result;
}

// Use this exact proposal for both the confirmation and the saved draft.
// A type change does not implicitly opt a legacy field into extension defaults.
export function planFieldTypeChange(field: IRStateFieldSchema, type: IRStateFieldType) {
  const next: IRStateFieldSchema = { ...field, type };
  const removed: string[] = [];
  const clean = (target: IRStateFieldSchema, scope: string) => {
    for (const key of incompatibleConstraintKeys(target)) {
      removed.push(`${scope}.${key}=${JSON.stringify(target[key as keyof IRStateFieldSchema])}`);
      delete target[key as keyof IRStateFieldSchema];
    }
  };
  clean(next, "base");
  if (field.cases !== undefined) {
    next.cases = field.cases.map((c, index) => {
      const candidate = { ...c, type };
      clean(candidate, `case ${index + 1}`);
      const { type: candidateType, ...result } = candidate;
      void candidateType;
      return result;
    });
  }
  return { next, removed };
}

function casesOverlap(a: IRStateFieldCase, b: IRStateFieldCase) {
  return a.when.all.every((left) => {
    const right = b.when.all.find((r) => r.field === left.field);
    return !right || left.in.some((value) => right.in.includes(value));
  });
}
// Decimal arithmetic follows the Backend shortest-decimal contract.
export function decimalParts(value: number): [bigint, number] {
  const [mantissa, exponent = "0"] = String(value).toLowerCase().split("e");
  const [whole, fraction = ""] = mantissa.split(".");
  const scale = fraction.length - Number(exponent);
  return scale < 0 ? [BigInt(whole + fraction) * 10n ** BigInt(-scale), 0] : [BigInt(whole + fraction), scale];
}
export function matchesDecimalStep(value: number, min: number | undefined, step: number) {
  const parts = [value, min ?? 0, step].map(decimalParts);
  const scale = Math.max(...parts.map((p) => p[1]));
  const ints = parts.map(([n, s]) => n * 10n ** BigInt(scale - s));
  return (ints[0] - ints[1]) % ints[2] === 0n;
}
function stepHasFinitePoint(max: number, step: number) {
  const parts = [max, step, -Number.MAX_VALUE].map(decimalParts);
  const scale = Math.max(...parts.map((p) => p[1]));
  const [m, s, limit] = parts.map(([n, p]) => n * 10n ** BigInt(scale - p));
  const quotient = m / s - (m % s === 0n ? 0n : 1n);
  return quotient * s >= limit;
}
export function validateStateSchema(schema: IRStateSchema): IRStateSchemaIssue[] {
  const issues: IRStateSchemaIssue[] = [];
  if (!Object.keys(schema).length) return [{ field_id: "", code: "invalid_schema", message: "fieldを1つ以上追加してください" }];
  for (const [id, field] of Object.entries(schema)) {
    const fail = (message: string, case_index?: number) => issues.push({ field_id: id, code: "invalid_schema", message, ...(case_index !== undefined ? { case_index } : {}) });
    if (!/^[a-z0-9]+(?:_[a-z0-9]+)*$/.test(id)) fail("field名は小文字英数字を_で区切ってください");
    if (!["enum", "number", "boolean", "string"].includes(field.type)) fail("型が不正です");
    const constraints = (f: IRStateFieldSchema, extended: boolean, index?: number) => {
      if ([f.min, f.max, f.step].some((n) => n !== undefined && !Number.isFinite(n))) fail("数値は有限値にしてください", index);
      if (f.min !== undefined && f.max !== undefined && f.min > f.max) fail("minをmax以下にしてください", index);
      if (extended && !["required", "optional", "absent"].includes(f.presence ?? "required")) fail("適用状態が不正です", index);
      if (extended && incompatibleConstraintKeys(f).length) fail(`型・適用状態に合わない制約: ${incompatibleConstraintKeys(f).join(", ")}`, index);
      if (f.type === "enum" && (!extended || f.presence !== "absent")) {
        if (!f.values?.length) fail("enum valuesを1つ以上入力してください", index);
        if (extended && f.values && (f.values.some((v) => typeof v !== "string") || new Set(f.values).size !== f.values.length)) fail("enum候補は重複のない文字列にしてください", index);
      }
      if (f.step !== undefined && (f.step <= 0 || (Number.isFinite(f.step) && f.min === undefined && f.max !== undefined && Number.isFinite(f.max) && f.max < 0 && !stepHasFinitePoint(f.max, f.step)))) fail("stepは正数で到達可能な値が必要です", index);
    };
    constraints(field, isExtendedField(field));
    field.cases?.forEach((c, index) => {
      if (!Object.hasOwn(c, "presence")) fail("caseには適用状態を指定してください", index);
      constraints({ ...c, type: field.type }, true, index);
      if (!c.when.all.length) fail("条件を1つ以上追加してください", index);
      const seen = new Set<string>();
      for (const selector of c.when.all) {
        const source = schema[selector.field];
        if (!source || selector.field === id || seen.has(selector.field) || source.cases !== undefined || (source.presence ?? "required") !== "required" || !["enum", "boolean"].includes(source.type)) { fail("selectorは条件なしの必須enum/boolean fieldを選んでください", index); continue; }
        seen.add(selector.field);
        if (!selector.in.length || new Set(selector.in).size !== selector.in.length || selector.in.some((v) => source.type === "boolean" ? typeof v !== "boolean" : typeof v !== "string" || !source.values?.includes(v))) fail("条件値はselectorの有効な候補から選んでください", index);
      }
      if (field.cases!.slice(0, index).some((prior) => casesOverlap(prior, c))) fail("同じfieldのcase条件が重複しています", index);
    });
  }
  return issues;
}

export function resolveStateField(base: IRStateFieldSchema, state: Record<string, unknown>): IRStateFieldSchema {
  const matched = base.cases?.find((c) => c.when.all.every((selector) => Object.hasOwn(state, selector.field) && selector.in.includes(state[selector.field] as string | boolean)));
  if (!matched) return base;
  // Only display attributes inherit; constraints are a complete replacement.
  return { type: base.type, ...matched, label: matched.label ?? base.label,
    description: matched.description ?? base.description, unit: matched.unit ?? base.unit };
}

export function stateSchemaIssues(schema: IRStateSchema, state: Record<string, unknown>): IRStateSchemaIssue[] {
  const issues: IRStateSchemaIssue[] = [];
  const valueIssue = (value: unknown, f: IRStateFieldSchema): string => {
    if (f.type === "enum") return typeof value !== "string" ? "type" : f.values?.includes(value) ? "" : "enum";
    if (f.type === "boolean") return typeof value === "boolean" ? "" : "type";
    if (f.type === "string") return typeof value !== "string" ? "type" : value === "" && f.allow_empty === false ? "empty" : "";
    if (typeof value !== "number" || !Number.isFinite(value)) return "type";
    if ((f.min !== undefined && value < f.min) || (f.max !== undefined && value > f.max)) return "range";
    return f.step !== undefined && !matchesDecimalStep(value, f.min, f.step) ? "step" : "";
  };
  const add = (field_id: string, code: string) => issues.push({ field_id, code, message: `${field_id}: ${code}` });
  for (const key of Object.keys(state)) if (!Object.hasOwn(schema, key)) add(key, "unknown_field");
  for (const [id, base] of Object.entries(schema)) {
    if (base.cases?.some((c) => c.when.all.some((selector) => !Object.hasOwn(state, selector.field) || !schema[selector.field] || valueIssue(state[selector.field], schema[selector.field])))) continue;
    const f = resolveStateField(base, state);
    const presence = f.presence ?? "required";
    const exists = Object.hasOwn(state, id);
    if (presence === "absent") { if (exists) add(id, "absent"); continue; }
    if (!exists) { if (presence === "required") add(id, "required"); continue; }
    const issue = valueIssue(state[id], f);
    if (issue) add(id, issue);
  }
  return issues;
}
