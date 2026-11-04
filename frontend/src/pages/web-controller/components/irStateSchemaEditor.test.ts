import { describe, expect, it } from "vitest";
import fixtures from "../../../../../backend/services/testdata/ir_state_schema_contract.json";
import type { IRStateSchema } from "../../../api/controller";
import { extendField, planFieldTypeChange, schemaFieldsFromSchema, schemaFromDrafts, validateStateSchema, resolveStateField, matchesDecimalStep, stateSchemaIssues } from "./irStateSchemaEditor";

describe("conditional Schema shared contract", () => {
  for (const [name, fixture] of Object.entries(fixtures)) {
    const schema = fixture.schema as IRStateSchema;
    it(`${name} preserves the complete Schema and matches all shared State cases`, () => {
      expect(validateStateSchema(schema)).toEqual([]);
      expect(schemaFromDrafts(schemaFieldsFromSchema(schema))).toEqual({ schema, errors: {} });
      for (const state of fixture.states) {
        const issues = stateSchemaIssues(schema, state.value);
        expect(issues.length === 0).toBe(state.valid);
        if ("code" in state) expect(issues.map((issue) => issue.code)).toContain(state.code);
      }
    });
  }
  it("preserves legacy irrelevant attributes and explicit empty values without defaults", () => {
    const schema: IRStateSchema = { note: { type: "string", min: 0, label: "" }, n: { type: "number", values: ["unused"] }, choice: { type: "enum", values: ["", " a,b ", "x", "x"] } };
    expect(schemaFromDrafts(schemaFieldsFromSchema(schema))).toEqual({ schema, errors: {} });
    expect(extendField(schema.note)).toEqual({ type: "string", label: "", presence: "required" });
  });
  it("rejects invalid references, values, self references, overlaps and type constraints", () => {
    const base: IRStateSchema = { enabled: { type: "boolean" }, level: { type: "number", presence: "absent", cases: [{ when: { all: [{ field: "enabled", in: [true] }] }, presence: "required", min: 0 }] } };
    for (const field of ["unknown", "level"]) {
      const schema = structuredClone(base); schema.level.cases![0].when.all[0].field = field;
      expect(validateStateSchema(schema)[0]).toMatchObject({ field_id: "level", case_index: 0 });
    }
    const badBoolean = structuredClone(base); badBoolean.level.cases![0].when.all[0].in = ["true"];
    expect(validateStateSchema(badBoolean).length).toBeGreaterThan(0);
    const overlap = structuredClone(base); overlap.level.cases!.push(structuredClone(overlap.level.cases![0]));
    expect(validateStateSchema(overlap).some((i) => i.message.includes("重複"))).toBe(true);
    for (const schema of [{ note: { type: "string", min: 0, presence: "required" } }, { n: { type: "number", step: 0 } }, { n: { type: "number", presence: "absent", min: 0 } }] as IRStateSchema[]) expect(validateStateSchema(schema).length).toBeGreaterThan(0);
  });
  it("replaces constraints in full and inherits only display attributes", () => {
    const base: IRStateSchema[ string ] = { type: "number", min: 0, max: 10, step: 2, label: "量", unit: "単位", cases: [{ when: { all: [{ field: "profile", in: ["special"] }] }, presence: "required", min: -2, unit: "" }] };
    expect(resolveStateField(base, { profile: "special" })).toMatchObject({ min: -2, label: "量", unit: "" });
    expect(resolveStateField(base, { profile: "special" })).not.toHaveProperty("max");
    expect(resolveStateField(base, { profile: "special" })).not.toHaveProperty("step");
    expect(resolveStateField(base, { profile: "normal" })).toEqual(base);
  });
  it("uses exact decimal grids rather than a float epsilon", () => {
    for (const [min, step, good, bad] of [[0, 0.1, 0.3, 0.30000000000000004], [-2, 0.5, -0.5, 0.25], [0.05, 0.1, 0.15, 0.1]] as const) {
      expect(matchesDecimalStep(good, min, step)).toBe(true);
      expect(matchesDecimalStep(bad, min, step)).toBe(false);
    }
    expect(matchesDecimalStep(-1, undefined, 0.5)).toBe(true);
  });
  it("plans type changes without mutating inputs or adding legacy defaults", () => {
    for (const [sourceType, attrs] of [["number", { min: 0, max: 10, step: 1 }], ["enum", { values: [""] }], ["string", { allow_empty: false }]] as const) {
      const source = { type: sourceType, presence: "optional", ...attrs, cases: [{ when: { all: [{ field: "flag", in: [false] }] }, presence: "required", ...attrs, unit: "" }] } as IRStateSchema[string];
      const original = structuredClone(source);
      const { next, removed } = planFieldTypeChange(source, "boolean");
      expect(source).toEqual(original);
      expect(next).toEqual({ type: "boolean", presence: "optional", cases: [{ when: { all: [{ field: "flag", in: [false] }] }, presence: "required", unit: "" }] });
      expect(removed.some((key) => key.startsWith("case 1."))).toBe(true);
    }
    expect(planFieldTypeChange({ type: "number", min: 0 }, "string").next).toEqual({ type: "string" });
  });

});
