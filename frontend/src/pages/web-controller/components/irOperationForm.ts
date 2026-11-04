import type { IRCommandFormat, IRCommandWriteRequest } from "../../../api/controller";

export const MAX_RAW_TIMINGS = 1024;
export const MAX_RAW_TIMING_US = 100000;
export const MAX_RAW_TOTAL_US = 1000000;
export const PANASONIC_40 = "PANASONIC_40";
export const PRESET_COMMANDS = [
  "LIGHT_ON_CH3",
  "LIGHT_FULL_CH3",
  "LIGHT_OFF_CH3",
  "LIGHT_NIGHT_CH3",
  "LIGHT_BRIGHT_UP_CH3",
  "LIGHT_BRIGHT_DOWN_CH3",
] as const;

export type RawTimingsResult = {
  timings: number[];
  count: number;
  total: number;
  error: string;
};

export function parseRawTimings(value: string): RawTimingsResult {
  const normalized = value.trim();
  if (!normalized) {
    return { timings: [], count: 0, total: 0, error: "RAW timingsを入力してください" };
  }
  const tokens = normalized.split(/[\s,]+/);
  const timings: number[] = [];
  let total = 0;
  for (const token of tokens) {
    if (!/^\d+$/.test(token)) {
      return { timings, count: tokens.length, total, error: "RAW timingは正の整数で入力してください" };
    }
    const timing = Number(token);
    if (!Number.isSafeInteger(timing) || timing <= 0) {
      return { timings, count: tokens.length, total, error: "RAW timingは正の整数で入力してください" };
    }
    timings.push(timing);
    total += timing;
  }
  if (timings.length > MAX_RAW_TIMINGS) {
    return { timings, count: timings.length, total, error: `timing件数は${MAX_RAW_TIMINGS}件以下にしてください` };
  }
  if (timings.some((timing) => timing > MAX_RAW_TIMING_US)) {
    return { timings, count: timings.length, total, error: `1 timingは${MAX_RAW_TIMING_US}us以下にしてください` };
  }
  if (total > MAX_RAW_TOTAL_US) {
    return { timings, count: timings.length, total, error: `合計時間は${MAX_RAW_TOTAL_US}us以下にしてください` };
  }
  return { timings, count: timings.length, total, error: "" };
}

export function operationFieldsForRequest(
  format: IRCommandFormat,
  rawTimings: string,
  protocol: string,
  code: string,
  preset: string
): { fields: IRCommandWriteRequest; error: string } {
  if (format === "raw_samples") return { fields: {}, error: "" };
  if (format === "preset_command") {
    return PRESET_COMMANDS.some((command) => command === preset)
      ? { fields: { pico_command: preset }, error: "" }
      : { fields: {}, error: "許可済みのプリセットIR Commandを選択してください" };
  }
  if (format === "raw") {
    const parsed = parseRawTimings(rawTimings);
    return parsed.error
      ? { fields: {}, error: parsed.error }
      : { fields: { timings_us: parsed.timings, count: parsed.count }, error: "" };
  }
  const normalizedProtocol = protocol.trim();
  const normalizedCode = code.trim();
  if (normalizedProtocol !== PANASONIC_40) return { fields: {}, error: "protocolはPANASONIC_40を選択してください" };
  if (!/^[0-9A-F]{10}$/.test(normalizedCode)) return { fields: {}, error: "codeは10桁の大文字hexで入力してください" };
  return { fields: { protocol: normalizedProtocol, code: normalizedCode }, error: "" };
}

export function timingsToFormValue(timings?: number[]): string {
  return timings?.join(",") ?? "";
}
