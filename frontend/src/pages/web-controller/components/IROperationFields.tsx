import type { IRCommandFormat } from "../../../api/controller";
import styles from "../WebController.module.css";
import { MAX_RAW_TIMINGS, MAX_RAW_TIMING_US, MAX_RAW_TOTAL_US, PANASONIC_40, PRESET_COMMANDS, parseRawTimings } from "./irOperationForm";

type Props = {
  prefix: string;
  format: IRCommandFormat;
  originalFormat?: IRCommandFormat;
  rawTimings: string;
  protocol: string;
  code: string;
  preset: string;
  disabled: boolean;
  formatDisabled?: boolean;
  formatGuidance?: string;
  onFormatChange: (value: IRCommandFormat) => void;
  onRawTimingsChange: (value: string) => void;
  onProtocolChange: (value: string) => void;
  onCodeChange: (value: string) => void;
  onPresetChange: (value: string) => void;
};

export function IROperationFields(props: Props) {
  const parsed = props.format === "raw" ? parseRawTimings(props.rawTimings) : undefined;
  return <>
    <div className={styles.formGroup}>
      <label className={styles.label} htmlFor={`${props.prefix}-format`}>format</label>
      <select id={`${props.prefix}-format`} className={styles.select} value={props.format} onChange={(event) => props.onFormatChange(event.target.value as IRCommandFormat)} disabled={props.disabled || props.formatDisabled}>
        <option value="raw_samples">raw_samples</option><option value="raw">raw</option><option value="code">code</option><option value="preset_command">preset_command</option>
      </select>
    </div>
    {props.formatGuidance && <p className={styles.helperText}>{props.formatGuidance}</p>}
    {props.originalFormat && props.originalFormat !== props.format && <p className={styles.warningText}>formatを変更して保存すると、旧format固有のdataは削除されます。</p>}
    {props.format === "raw_samples" && <p className={styles.helperText}>学習dataは直接編集できません。保存後にcapture操作で更新します。</p>}
    {props.format === "raw" && <div className={styles.formGroup}>
      <label className={styles.label} htmlFor={`${props.prefix}-raw`}>RAW timings (us)</label>
      <textarea id={`${props.prefix}-raw`} className={styles.textarea} value={props.rawTimings} onChange={(event) => props.onRawTimingsChange(event.target.value)} disabled={props.disabled} placeholder="3500,1750,450,450" />
      <p className={parsed?.error ? styles.error : styles.helperText}>件数 {parsed?.count ?? 0} / {MAX_RAW_TIMINGS}、合計 {parsed?.total ?? 0} / {MAX_RAW_TOTAL_US}us、単値上限 {MAX_RAW_TIMING_US}us{parsed?.error ? ` — ${parsed.error}` : ""}</p>
    </div>}
    {props.format === "code" && <div className={styles.formRow}>
      <div className={styles.formGroup}><label className={styles.label} htmlFor={`${props.prefix}-protocol`}>protocol</label><select id={`${props.prefix}-protocol`} className={styles.select} value={props.protocol} onChange={(event) => props.onProtocolChange(event.target.value)} disabled={props.disabled}><option value={PANASONIC_40}>{PANASONIC_40}</option></select></div>
      <div className={styles.formGroup}><label className={styles.label} htmlFor={`${props.prefix}-code`}>code</label><input id={`${props.prefix}-code`} className={styles.input} value={props.code} onChange={(event) => props.onCodeChange(event.target.value.trim().toUpperCase())} disabled={props.disabled} placeholder="344A90BC2C" maxLength={10} /></div>
    </div>}
    {props.format === "preset_command" && <div className={styles.formGroup}><label className={styles.label} htmlFor={`${props.prefix}-preset`}>プリセットIR Command</label><select id={`${props.prefix}-preset`} className={styles.select} value={props.preset} onChange={(event) => props.onPresetChange(event.target.value)} disabled={props.disabled}><option value="">選択してください</option>{PRESET_COMMANDS.map((command) => <option key={command} value={command}>{command}</option>)}</select></div>}
  </>;
}
