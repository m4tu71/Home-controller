import type { IRState, IRStateSchema } from "../../../api/controller";

import { stateSummary, stateOperationReason } from "./irStateDraft";
import { InfoPopover } from "./InfoPopover";
import infoStyles from "./InfoPopover.module.css";
import styles from "../WebController.module.css";

type IRStateCardProps = {
  state: IRState;
  schema?: IRStateSchema;
  isSelected: boolean;
  carrierKHz: number;
  disabled: boolean;
  codeExecutionModeSelected?: boolean;
  onSelectState: (state: IRState) => void;
};

export function IRStateCard({
  state,
  schema,
  isSelected,
  carrierKHz,
  disabled,
  codeExecutionModeSelected = false,
  onSelectState,
}: IRStateCardProps) {
  const isLearned =
    (state.captures?.length ?? 0) > 0 ||
    (state.timings_us?.length ?? 0) > 0 ||
    (state.count ?? 0) > 0;
  const effectiveCarrier = state.carrier_khz ?? carrierKHz;
  const rawBlocked = (state.format === "raw_samples" || state.format === "raw") && effectiveCarrier !== 38;
  const reason = stateOperationReason(schema, state);
  const sendStatus = reason ? `送信不可 · ${reason}` : rawBlocked
    ? `送信不可 · 実効carrier ${effectiveCarrier}kHz（38kHz必須）`
    : state.format === "raw_samples"
      ? isLearned ? "学習済み · 送信可能" : "未学習 · 学習後に送信可能"
      : state.format === "code"
        ? codeExecutionModeSelected ? "code · 実行方式選択済み" : "code · 実行方式の選択が必要"
        : `${state.format} · 送信可能`;
  const schemaValues = stateSummary(schema, state.state);

  return (
    <div className={styles.irOperationOption}>
      <InfoPopover subject={`State ${state.name} (${state.id})`} title={`${state.name} のState情報`} placement="operation">
        <dl className={infoStyles.details}>
          <div><dt>State ID</dt><dd>{state.id}</dd></div>
          <div><dt>Format</dt><dd>{state.format}</dd></div>
          <div><dt>Schema値</dt><dd>{schemaValues}</dd></div>
          <div><dt>保存field ID / 値</dt><dd>{JSON.stringify(state.state)}</dd></div>
          <div><dt>実効carrier</dt><dd>{effectiveCarrier} kHz</dd></div>
          {state.protocol && <div><dt>Protocol</dt><dd>{state.protocol}</dd></div>}
          {state.pico_command && <div><dt>Pico preset</dt><dd>{state.pico_command}</dd></div>}
          {(state.format === "raw" || state.format === "raw_samples") && <div><dt>Timing数</dt><dd>{state.timings_us?.length ?? state.count ?? 0}</dd></div>}
          {state.format === "raw_samples" && <div><dt>有効な学習</dt><dd>{state.captures?.filter((capture) => capture.valid).length ?? 0}件</dd></div>}
        </dl>
        <p className={styles.helperText}>情報ボタンでは選択・送信されません。送信成功は家電の実状態を示しません。</p>
      </InfoPopover>
      <button
        type="button"
        disabled={disabled}
        className={`${styles.irOperationButton} ${isSelected ? styles.irOperationButtonSelected : ""}`}
        onClick={() => onSelectState(state)}
        aria-pressed={isSelected}
        aria-label={`${state.name} (${state.id}) · ${schemaValues} · ${state.format} · ${sendStatus}`}
      >
        <span className={styles.irOperationName}>{state.name}</span>
        <span className={styles.irOperationMeta}>{schemaValues}</span>
        <span className={styles.irOperationAvailability}>{state.id} · {state.format} · {sendStatus}</span>
      </button>
    </div>
  );
}
