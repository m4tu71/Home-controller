import { describe, expect, it } from "vitest";
import fixtures from "../../../../../backend/services/testdata/ir_state_schema_contract.json";
import type { IRState, IRStateSchema } from "../../../api/controller";
import { parseStateDraft, registeredState, stateSummary, stateOperationReason } from "./irStateDraft";

describe("Step 4E State draft contract", () => {
  for (const [name, fixture] of Object.entries(fixtures)) it(`${name} matches every shared fixture and retains typed zero/false`, () => {
    const schema = fixture.schema as IRStateSchema;
    for (const test of fixture.states) {
      const values = Object.fromEntries(Object.entries(test.value).map(([k, v]) => [k, String(v)]));
      const draft = parseStateDraft(schema, values);
      // Inapplicable input is explicitly discarded; a saved State containing it is incompatible.
      if (test.valid) { expect(draft.errors).toEqual({}); expect(draft.state).toEqual(test.value); }
      else if ("code" in test && test.code === "absent") { expect(Object.keys(draft.discarded).length).toBeGreaterThan(0); }
      else if ("code" in test && test.code === "type" && Object.values(test.value).includes("false")) {
        // The UI draft string "false" intentionally represents a typed boolean.
        expect(draft.state.enabled).toBe(false);
      } else expect(Object.keys(draft.errors).length).toBeGreaterThan(0);
      const saved = { id: "fixture", name: "Fixture", state: test.value, format: "raw_samples" } as IRState;
      expect(stateOperationReason(schema, saved) === "").toBe(test.valid);
    }
  });
  it("keeps empty object, empty enum/string, optional omission, zero and false distinct", () => {
    const schema: IRStateSchema = { n: { type: "number", presence: "optional", step: 0.1 }, b: { type: "boolean", presence: "optional" }, s: { type: "string", presence: "optional" }, e: { type: "enum", presence: "optional", values: [""] } };
    expect(parseStateDraft(schema, {}).state).toEqual({});
    const draft = parseStateDraft(schema, { n: "0", b: "false", s: "", e: "" });
    expect(draft.errors).toEqual({}); expect(draft.state).toEqual({ n: 0, b: false, s: "", e: "" });
    expect(parseStateDraft(schema, { n: "" }).errors.n).toBeDefined();
    expect(parseStateDraft(schema, { n: "0.30000000000000004" }).errors.n).toContain("step");
  });
  it("waits for valid selectors and does not erase hidden draft values", () => {
    const schema = fixtures.air_conditioner.schema as IRStateSchema;
    const values = { power: "on", mode: "cool", target_temperature: "26" };
    expect(parseStateDraft(schema, {}).pending.has("target_temperature")).toBe(true);
    const fan = parseStateDraft(schema, { ...values, mode: "fan" });
    expect(fan.state).toEqual({ power: "on", mode: "fan" }); expect(fan.discarded).toEqual({ target_temperature: "26" });
    expect(parseStateDraft(schema, values).state.target_temperature).toBe(26);
  });
  it("protects all registered formats and resolves daily labels and units", () => {
    for (const signal of [{ format: "raw_samples", count: 1 }, { format: "raw", timings_us: [100] }, { format: "code", protocol: "PANASONIC_40" }, { format: "preset_command", pico_command: "LIGHT_ON_CH3" }]) expect(registeredState(signal as IRState)).toBe(true);
    expect(registeredState({ format: "raw_samples", count: 0, captures: [], timings_us: [] } as unknown as IRState)).toBe(false);
    const schema: IRStateSchema = { mode: { type: "enum", values: ["cool", "fan"], label: "運転" }, temperature: { type: "number", presence: "absent", cases: [{ when: { all: [{ field: "mode", in: ["cool"] }] }, presence: "required", label: "設定", unit: "℃" }] } };
    expect(stateSummary(schema, { mode: "cool", temperature: 0 })).toBe("運転=cool · 設定=0 ℃");
    expect(stateSummary(schema, { mode: "fan", temperature: 26 })).toBe("運転=fan");
    expect(stateOperationReason(undefined, { state: {} } as IRState)).toContain("未取得");
  });
  it("uses field IDs for empty base/case labels while preserving empty Schema attributes", () => {
    const schema: IRStateSchema = { mode: { type: "enum", values: ["normal", "special"], label: "" }, level: { type: "number", label: "base label", cases: [{ when: { all: [{ field: "mode", in: ["special"] }] }, presence: "required", label: "" }] } };
    expect(stateSummary(schema, { mode: "special", level: 0 })).toBe("mode=special · level=0");
    expect(stateSummary(schema, { mode: "normal", level: 0 })).toBe("mode=normal · base label=0");
    expect(schema.mode.label).toBe(""); expect(schema.level.cases![0].label).toBe("");
  });

});
