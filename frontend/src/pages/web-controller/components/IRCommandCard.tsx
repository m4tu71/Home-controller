import type { IRCommand } from "../../../api/controller";

import { InfoPopover } from "./InfoPopover";
import infoStyles from "./InfoPopover.module.css";
import styles from "../WebController.module.css";

type IRCommandCardProps = {
  command: IRCommand;
  isSelected: boolean;
  carrierKHz: number;
  disabled: boolean;
  codeExecutionModeSelected?: boolean;
  onSelectCommand: (command: IRCommand) => void;
};

export function IRCommandCard({
  command,
  isSelected,
  carrierKHz,
  disabled,
  codeExecutionModeSelected = false,
  onSelectCommand,
}: IRCommandCardProps) {
  const captures = command.captures ?? [];
  const isLearned =
    captures.length > 0 ||
    (command.timings_us?.length ?? 0) > 0 ||
    (command.count ?? 0) > 0;
  const effectiveCarrier = command.carrier_khz ?? carrierKHz;
  const rawBlocked = (command.format === "raw_samples" || command.format === "raw") && effectiveCarrier !== 38;
  const sendStatus = rawBlocked
    ? `送信不可 · 実効carrier ${effectiveCarrier}kHz（38kHz必須）`
    : command.format === "raw_samples"
      ? isLearned ? "学習済み · 送信可能" : "未学習 · 学習後に送信可能"
      : command.format === "preset_command"
        ? "Pico preset · 送信可能"
        : command.format === "raw"
          ? "RAW data · 送信可能"
          : codeExecutionModeSelected
            ? "code · 実行方式選択済み"
            : "code · 実行方式の選択が必要";

  return (
    <div className={`${styles.irOperationOption} ${styles.irCommandOption}`}>
      <button
        type="button"
        disabled={disabled}
        className={`${styles.irOperationButton} ${isSelected ? styles.irOperationButtonSelected : ""}`}
        onClick={() => onSelectCommand(command)}
        aria-pressed={isSelected}
        aria-label={`${command.name} (${command.id}) · ${command.format} · ${sendStatus}`}
      >
        <span className={styles.irOperationName}>{command.name}</span>
        <span className={styles.irOperationMeta}>{command.id} · {command.format}</span>
        <span className={styles.irOperationAvailability}>{sendStatus}</span>
      </button>
      <InfoPopover subject={`Command ${command.name} (${command.id})`} title={`${command.name} のCommand情報`} placement="embedded">
        <dl className={infoStyles.details}>
          <div><dt>Command ID</dt><dd>{command.id}</dd></div>
          <div><dt>Format</dt><dd>{command.format}</dd></div>
          <div><dt>実効carrier</dt><dd>{effectiveCarrier} kHz</dd></div>
          {command.protocol && <div><dt>Protocol</dt><dd>{command.protocol}</dd></div>}
          {command.pico_command && <div><dt>Pico preset</dt><dd>{command.pico_command}</dd></div>}
          {(command.format === "raw" || command.format === "raw_samples") && <div><dt>Timing数</dt><dd>{command.timings_us?.length ?? command.count ?? 0}</dd></div>}
          {command.format === "raw_samples" && <div><dt>有効な学習</dt><dd>{command.captures?.filter((capture) => capture.valid).length ?? 0}件</dd></div>}
        </dl>
        <p className={styles.helperText}>この情報ボタンでは選択・送信されません。送信は選択後の専用buttonから行います。</p>
      </InfoPopover>
    </div>
  );
}
