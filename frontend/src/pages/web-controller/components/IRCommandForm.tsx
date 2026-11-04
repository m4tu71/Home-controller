import type { FormEvent } from "react";

import type { IRCommand, IRCommandFormat } from "../../../api/controller";

import styles from "../WebController.module.css";
import { IROperationFields } from "./IROperationFields";

type IRCommandFormProps = {
  selectedDevice: boolean;
  selectedCommand?: IRCommand;
  loading: boolean;
  createId: string;
  createName: string;
  createFormat: IRCommandFormat;
  createPreset: string;
  createRawTimings: string;
  createProtocol: string;
  createCode: string;
  createRepeat: string;
  editName: string;
  editFormat: IRCommandFormat;
  editPreset: string;
  editRawTimings: string;
  editProtocol: string;
  editCode: string;
  editRepeat: string;
  onCreate: (event: FormEvent<HTMLFormElement>) => void;
  onUpdate: (event: FormEvent<HTMLFormElement>) => void;
  onCreateIdChange: (value: string) => void;
  onCreateNameChange: (value: string) => void;
  onCreateFormatChange: (value: IRCommandFormat) => void;
  onCreatePresetChange: (value: string) => void;
  onCreateRawTimingsChange: (value: string) => void;
  onCreateProtocolChange: (value: string) => void;
  onCreateCodeChange: (value: string) => void;
  onCreateRepeatChange: (value: string) => void;
  onEditNameChange: (value: string) => void;
  onEditFormatChange: (value: IRCommandFormat) => void;
  onEditPresetChange: (value: string) => void;
  onEditRawTimingsChange: (value: string) => void;
  onEditProtocolChange: (value: string) => void;
  onEditCodeChange: (value: string) => void;
  onEditRepeatChange: (value: string) => void;
};

type CommandFieldsProps = {
  prefix: "create" | "edit";
  name: string;
  format: IRCommandFormat;
  preset: string;
  rawTimings: string;
  protocol: string;
  code: string;
  originalFormat?: IRCommandFormat;
  repeat: string;
  disabled: boolean;
  onNameChange: (value: string) => void;
  onFormatChange: (value: IRCommandFormat) => void;
  onPresetChange: (value: string) => void;
  onRawTimingsChange: (value: string) => void;
  onProtocolChange: (value: string) => void;
  onCodeChange: (value: string) => void;
  onRepeatChange: (value: string) => void;
};

function CommandFields({
  prefix,
  name,
  format,
  preset,
  rawTimings,
  protocol,
  code,
  originalFormat,
  repeat,
  disabled,
  onNameChange,
  onFormatChange,
  onPresetChange,
  onRawTimingsChange,
  onProtocolChange,
  onCodeChange,
  onRepeatChange,
}: CommandFieldsProps) {
  return (
    <>
      <div className={styles.formRow}>
        <div className={styles.formGroup}>
          <label className={styles.label} htmlFor={`ir-command-${prefix}-name`}>
            表示名
          </label>
          <input
            id={`ir-command-${prefix}-name`}
            className={styles.input}
            value={name}
            onChange={(event) => onNameChange(event.target.value)}
            disabled={disabled}
          />
        </div>

      </div>
      <IROperationFields prefix={`ir-command-${prefix}`} format={format} originalFormat={originalFormat} rawTimings={rawTimings} protocol={protocol} code={code} preset={preset} disabled={disabled} onFormatChange={onFormatChange} onRawTimingsChange={onRawTimingsChange} onProtocolChange={onProtocolChange} onCodeChange={onCodeChange} onPresetChange={onPresetChange} />

      <div className={styles.formGroup}>
        <label className={styles.label} htmlFor={`ir-command-${prefix}-repeat`}>
          リピート回数 (repeat)
        </label>
        <input
          id={`ir-command-${prefix}-repeat`}
          className={styles.input}
          type="number"
          min="1"
          max="5"
          step="1"
          value={repeat}
          onChange={(event) => onRepeatChange(event.target.value)}
          placeholder="未指定時はIR Adapter設定を使用"
          disabled={disabled}
        />
      </div>
    </>
  );
}

export function IRCommandForm({
  selectedDevice,
  selectedCommand,
  loading,
  createId,
  createName,
  createFormat,
  createPreset,
  createRawTimings,
  createProtocol,
  createCode,
  createRepeat,
  editName,
  editFormat,
  editPreset,
  editRawTimings,
  editProtocol,
  editCode,
  editRepeat,
  onCreate,
  onUpdate,
  onCreateIdChange,
  onCreateNameChange,
  onCreateFormatChange,
  onCreatePresetChange,
  onCreateRawTimingsChange,
  onCreateProtocolChange,
  onCreateCodeChange,
  onCreateRepeatChange,
  onEditNameChange,
  onEditFormatChange,
  onEditPresetChange,
  onEditRawTimingsChange,
  onEditProtocolChange,
  onEditCodeChange,
  onEditRepeatChange,
}: IRCommandFormProps) {
  const createDisabled = loading || !selectedDevice;
  const editDisabled = loading || !selectedDevice || !selectedCommand;

  return (
    <div className={styles.deviceForm}>
      <form className={styles.deviceSubForm} onSubmit={onCreate}>
        <h4 className={styles.formTitle}>IR Command作成</h4>

        <div className={styles.formGroup}>
          <label className={styles.label} htmlFor="ir-command-create-id">
            IR Command ID
          </label>
          <input
            id="ir-command-create-id"
            className={styles.input}
            value={createId}
            onChange={(event) => onCreateIdChange(event.target.value)}
            placeholder="light_on"
            disabled={createDisabled}
          />
          <p className={styles.helperText}>小文字のsnake_caseで入力します。</p>
        </div>

        <CommandFields
          prefix="create"
          name={createName}
          format={createFormat}
          preset={createPreset}
          rawTimings={createRawTimings}
          protocol={createProtocol}
          code={createCode}
          repeat={createRepeat}
          disabled={createDisabled}
          onNameChange={onCreateNameChange}
          onFormatChange={onCreateFormatChange}
          onPresetChange={onCreatePresetChange}
          onRawTimingsChange={onCreateRawTimingsChange}
          onProtocolChange={onCreateProtocolChange}
          onCodeChange={onCreateCodeChange}
          onRepeatChange={onCreateRepeatChange}
        />

        <div className={styles.roomFormActions}>
          <button type="submit" className={styles.primaryButton} disabled={createDisabled}>
            IR Command作成
          </button>
        </div>
      </form>

      <form className={styles.deviceSubForm} onSubmit={onUpdate}>
        <h4 className={styles.formTitle}>IR Command編集</h4>

        <div className={styles.formGroup}>
          <label className={styles.label}>IR Command ID</label>
          <div className={styles.readOnlyValue}>{selectedCommand?.id ?? "-"}</div>
        </div>

        <CommandFields
          prefix="edit"
          name={editName}
          format={editFormat}
          preset={editPreset}
          rawTimings={editRawTimings}
          protocol={editProtocol}
          code={editCode}
          originalFormat={selectedCommand?.format}
          repeat={editRepeat}
          disabled={editDisabled}
          onNameChange={onEditNameChange}
          onFormatChange={onEditFormatChange}
          onPresetChange={onEditPresetChange}
          onRawTimingsChange={onEditRawTimingsChange}
          onProtocolChange={onEditProtocolChange}
          onCodeChange={onEditCodeChange}
          onRepeatChange={onEditRepeatChange}
        />

        <div className={styles.roomFormActions}>
          <button type="submit" className={styles.primaryButton} disabled={editDisabled}>
            IR Command更新
          </button>
        </div>
      </form>
    </div>
  );
}
