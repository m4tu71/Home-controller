import type { IRState, IRStateFieldSchema, IRStateSchema } from "../../../api/controller";
import { resolveStateField, stateSchemaIssues, validateStateSchema } from "./irStateSchemaEditor";

export function registeredState(state?: IRState): boolean {
  if (!state) return false;
  if (state.format === "raw_samples") return Boolean(state.count || state.timings_us?.length || state.captures?.length);
  if (state.format === "raw") return Boolean(state.count || state.timings_us?.length);
  if (state.format === "code") return Boolean(state.protocol || state.code);
  return Boolean(state.pico_command);
}
export function parseStateDraft(schema: IRStateSchema, values: Record<string, string>) {
  const state: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  const resolved: Record<string, IRStateFieldSchema> = {};
  const pending = new Set<string>();
  const discarded: Record<string, string> = {};
  const schemaErrors = validateStateSchema(schema);
  if (schemaErrors.length) return { state, errors: { schema: schemaErrors.map((i) => `${i.field_id}: ${i.message}`).join(" / ") }, resolved, pending, discarded };
  const raw: Record<string, unknown> = {};
  const parseErrors: Record<string, string> = {};
  for (const [key, value] of Object.entries(values)) {
    const field = schema[key];
    if (!field) { discarded[key] = value; continue; }
    if (field.type === "number") {
      if (value.trim() === "" || !Number.isFinite(Number(value))) parseErrors[key] = "数値を入力してください";
      else raw[key] = Number(value);
    } else if (field.type === "boolean") {
      if (value !== "true" && value !== "false") parseErrors[key] = "trueまたはfalseを選択してください";
      else raw[key] = value === "true";
    } else raw[key] = value;
  }
  const sourceIssues = stateSchemaIssues(schema, raw);
  for (const [key, base] of Object.entries(schema)) {
    if (base.cases?.some((c) => c.when.all.some((s) => !Object.hasOwn(raw, s.field) || parseErrors[s.field] || sourceIssues.some((i) => i.field_id === s.field)))) {
      pending.add(key); errors[key] = "selectorの入力待ちです"; continue;
    }
    const field = resolveStateField(base, raw); resolved[key] = field;
    if (field.presence === "absent") { if (Object.hasOwn(values, key)) discarded[key] = values[key]; continue; }
    if (field.presence === "optional" && !Object.hasOwn(values, key)) continue;
    if (parseErrors[key]) errors[key] = parseErrors[key];
    else if (Object.hasOwn(raw, key)) state[key] = raw[key];
    else if (field.type === "string") state[key] = "";
  }
  for (const issue of stateSchemaIssues(schema, state)) {
    if (!pending.has(issue.field_id) && !errors[issue.field_id]) errors[issue.field_id] = `${issue.field_id}: ${issue.code}`;
  }
  return { state, errors, resolved, pending, discarded };
}
export function stateOperationReason(schema: IRStateSchema | undefined, state: IRState): string {
  if (!schema) return "State Schema未取得・未作成のため利用できません";
  const invalid = validateStateSchema(schema);
  if (invalid.length) return `Schema不整合: ${invalid.map((i) => `${i.field_id}: ${i.message}`).join(" / ")}`;
  const issues = stateSchemaIssues(schema, state.state);
  return issues.length ? `保存State不整合: ${issues.map((i) => `${i.field_id}: ${i.code}`).join(" / ")}` : "";
}
export function stateSummary(schema: IRStateSchema | undefined, state: Record<string, unknown>): string {
  if (!schema || validateStateSchema(schema).length) return "Schema未取得・不整合（保存値は情報欄を確認）";
  return Object.entries(schema).flatMap(([key, base]) => {
    const field = resolveStateField(base, state);
    if (field.presence === "absent" || !Object.hasOwn(state, key)) return [];
    const value = state[key] === "" ? "空文字" : String(state[key]);
    return [`${field.label || key}=${value}${field.unit ? ` ${field.unit}` : ""}`];
  }).join(" · ") || "適用値なし";
}
