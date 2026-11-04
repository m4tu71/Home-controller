import type { FormEvent } from "react";

import type { Device, IRCommandFormat, IRExecutionMode, IRExecutionOptionsResponse, IRState, IRStateSchema, Room } from "../../../api/controller";

import { stateOperationReason } from "./irStateDraft";
import { IRStateCard } from "./IRStateCard";
import { IRStateForm } from "./IRStateForm";
import styles from "../WebController.module.css";

type IRStateSectionProps = {
  mode?: "operations" | "management";
  managementHref?: string;
  selectedRoom?: Room;
  selectedDevice?: Device;
  irRemoteType: "command" | "stateful";
  schema?: IRStateSchema;
  states: IRState[];
  selectedStateId: string;
  message: string;
  error: string;
  loading: boolean;
  listLoaded: boolean;
  createId: string;
  createName: string;
  createValues: Record<string, string>;
  createFormat: IRCommandFormat;
  createRawTimings: string;
  createProtocol: string;
  createCode: string;
  createPreset: string;
  editName: string;
  editValues: Record<string, string>;
  editFormat: IRCommandFormat;
  editRawTimings: string;
  editProtocol: string;
  editCode: string;
  editPreset: string;
  fieldErrors: Record<string, string>;
  captureSamples: string;
  captureMaxAttempts: string;
  sendRepeat: string;
  captureFieldErrors: Record<string, string>;
  sendRepeatError: string;
  executionMode: IRExecutionMode | "";
  executionOptions: IRExecutionOptionsResponse | null;
  executionOptionsError: string;
  carrierKHz: number;
  onRefresh: () => void;
  onSelectState: (state: IRState) => void;
  onCreate: (event: FormEvent<HTMLFormElement>) => void;
  onUpdate: (event: FormEvent<HTMLFormElement>) => void;
  onDelete: () => void;
  onCapture: () => void;
  onSend: () => void;
  onCreateIdChange: (value: string) => void;
  onCreateNameChange: (value: string) => void;
  onCreateValueChange: (key: string, value: string | undefined) => void;
  onCreateFormatChange: (value: IRCommandFormat) => void;
  onCreateRawTimingsChange: (value: string) => void;
  onCreateProtocolChange: (value: string) => void;
  onCreateCodeChange: (value: string) => void;
  onCreatePresetChange: (value: string) => void;
  onEditNameChange: (value: string) => void;
  onEditValueChange: (key: string, value: string | undefined) => void;
  onEditFormatChange: (value: IRCommandFormat) => void;
  onEditRawTimingsChange: (value: string) => void;
  onEditProtocolChange: (value: string) => void;
  onEditCodeChange: (value: string) => void;
  onEditPresetChange: (value: string) => void;
  onCaptureSamplesChange: (value: string) => void;
  onCaptureMaxAttemptsChange: (value: string) => void;
  onSendRepeatChange: (value: string) => void;
  onExecutionModeChange: (value: IRExecutionMode | "") => void;
};

export function IRStateSection(props: IRStateSectionProps) {
  const { selectedRoom, selectedDevice, irRemoteType, schema, states, selectedStateId, message, error, loading, listLoaded } = props;
  const managementMode = (props.mode ?? "management") === "management";
  if (!selectedDevice || selectedDevice.primary_adapter !== "ir" || irRemoteType !== "stateful") return null;
  const selectedState = states.find((state) => state.id === selectedStateId);
  const stateReason = selectedState ? stateOperationReason(schema, selectedState) : "";
  const showEmptyState = listLoaded && !loading && !error && states.length === 0;
  const disabled = loading || !selectedRoom;
  const isRawSamplesState = selectedState?.format === "raw_samples";
  const isLearned =
    isRawSamplesState &&
    ((selectedState.captures?.length ?? 0) > 0 ||
      (selectedState.timings_us?.length ?? 0) > 0 ||
      (selectedState.count ?? 0) > 0);
  const rawCarrierKHz = selectedState?.carrier_khz ?? props.carrierKHz;
  const rawCarrierBlocked = (selectedState?.format === "raw_samples" || selectedState?.format === "raw") && rawCarrierKHz !== 38;
  const currentExecutionOptions = props.executionOptions?.protocol === selectedState?.protocol ? props.executionOptions : null;
  const selectedCodeMode = props.executionMode ? currentExecutionOptions?.execution_modes[props.executionMode] : undefined;
  const codeModeAvailable = selectedState?.format !== "code" || Boolean(selectedCodeMode?.available);

  return (
      <section className={styles.deviceBlock} aria-busy={loading}>
      <div className={styles.deviceHeader}>
        <h3 className={styles.deviceTitle}>IR State</h3>
        <button type="button" className={styles.statusButton} onClick={props.onRefresh} disabled={disabled}>一覧更新</button>
      </div>
      <div className={styles.roomStatusGrid}>
        <div className={styles.statusLabel}>対象Room</div><div className={styles.statusValue}>{selectedRoom ? `${selectedRoom.name} (${selectedRoom.id})` : "-"}</div>
        <div className={styles.statusLabel}>対象Device</div><div className={styles.statusValue}>{selectedDevice.name} ({selectedDevice.id})</div>
        <div className={styles.statusLabel}>State数</div><div className={styles.statusValue}>{states.length}</div>
      </div>
      {!listLoaded && !message && <p className={styles.info} role="status">IR State一覧を読み込んでいます...</p>}
      {listLoaded && states.length > 0 ? <div className={styles.irOperationButtons} role="group" aria-label="IR State操作">{states.map((state) => <IRStateCard key={state.id} schema={schema} state={state} isSelected={state.id === selectedStateId} carrierKHz={props.carrierKHz} disabled={loading} codeExecutionModeSelected={state.id === selectedStateId && Boolean(selectedCodeMode?.available)} onSelectState={props.onSelectState} />)}</div> : showEmptyState ? <p className={styles.roomEmptyState} role="status">IR Stateはまだ登録されていません。{managementMode ? "下のState作成フォームから登録できます。" : <> <a href={props.managementHref ?? "/settings"}>Settings</a>から登録できます。</>}</p> : null}
      {message && <p className={styles.info} role="status">{message}</p>}
      {error && <p className={styles.error} role="alert">{error}</p>}
      <p className={styles.helperText}>このDevice配下のIR State dataは、旧IR Signal dataとは独立して管理されます。</p>
      {stateReason && <p className={styles.warningText}>{stateReason}</p>}
      {managementMode && <>
      {selectedState && <div className={styles.roomDeletePanel}><div><p className={styles.warningText}>選択中: {selectedState.name}</p><p className={styles.helperText}>formatにかかわらず、不要なStateはTrashへ移動できます。</p></div><button type="button" className={styles.dangerButton} onClick={props.onDelete} disabled={loading}>IR State削除</button></div>}
      <hr className={styles.divider} />
      <IRStateForm schema={schema} selectedState={selectedState} loading={loading} createId={props.createId} createName={props.createName} createValues={props.createValues} createFormat={props.createFormat} createRawTimings={props.createRawTimings} createProtocol={props.createProtocol} createCode={props.createCode} createPreset={props.createPreset} editName={props.editName} editValues={props.editValues} editFormat={props.editFormat} editRawTimings={props.editRawTimings} editProtocol={props.editProtocol} editCode={props.editCode} editPreset={props.editPreset} fieldErrors={props.fieldErrors} onCreate={props.onCreate} onUpdate={props.onUpdate} onCreateIdChange={props.onCreateIdChange} onCreateNameChange={props.onCreateNameChange} onCreateValueChange={props.onCreateValueChange} onCreateFormatChange={props.onCreateFormatChange} onCreateRawTimingsChange={props.onCreateRawTimingsChange} onCreateProtocolChange={props.onCreateProtocolChange} onCreateCodeChange={props.onCreateCodeChange} onCreatePresetChange={props.onCreatePresetChange} onEditNameChange={props.onEditNameChange} onEditValueChange={props.onEditValueChange} onEditFormatChange={props.onEditFormatChange} onEditRawTimingsChange={props.onEditRawTimingsChange} onEditProtocolChange={props.onEditProtocolChange} onEditCodeChange={props.onEditCodeChange} onEditPresetChange={props.onEditPresetChange} />

      {isRawSamplesState && (
        <div className={styles.irStateCapturePanel}>
          <h4 className={styles.formTitle}>{isLearned ? "IR State再学習" : "IR State学習"}</h4>
          <p className={styles.helperText}>学習を開始したら、リモコンを受信機へ向けて対象状態のボタンを押してください。</p>
          <div className={styles.formRow}>
            <div className={styles.formGroup}>
              <label className={styles.label} htmlFor="ir-state-capture-samples">samples</label>
              <input id="ir-state-capture-samples" className={styles.input} type="number" min="1" max="7" step="1" value={props.captureSamples} onChange={(event) => props.onCaptureSamplesChange(event.target.value)} disabled={loading} />
              {props.captureFieldErrors.samples && <p className={styles.schemaFieldError}>{props.captureFieldErrors.samples}</p>}
            </div>
            <div className={styles.formGroup}>
              <label className={styles.label} htmlFor="ir-state-capture-max-attempts">max_attempts</label>
              <input id="ir-state-capture-max-attempts" className={styles.input} type="number" min="1" max="20" step="1" value={props.captureMaxAttempts} onChange={(event) => props.onCaptureMaxAttemptsChange(event.target.value)} disabled={loading} />
              {props.captureFieldErrors.maxAttempts && <p className={styles.schemaFieldError}>{props.captureFieldErrors.maxAttempts}</p>}
            </div>
          </div>
          <div className={styles.roomFormActions}><button type="button" className={styles.primaryButton} onClick={props.onCapture} disabled={disabled || Boolean(stateReason)}>{isLearned ? "再学習を開始" : "学習を開始"}</button></div>
          <p className={styles.helperText}>学習失敗時は既存のcaptures、timings_us、countを維持します。</p>
        </div>
      )}
      </>}

      {!managementMode && selectedState && (
        <div className={styles.irStateSendPanel}>
          <h4 className={styles.formTitle}>IR State送信</h4>
          <p className={styles.helperText}>{isRawSamplesState ? (isLearned ? "学習済みRAW信号を送信します。送信成功は実機の現在状態を示しません。" : "未学習のraw_samples Stateは送信できません。先に学習してください。") : `${selectedState.format}形式を送信します。送信成功は実機の現在状態を示しません。`}</p>
          {selectedState.format === "code" && <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="ir-state-execution-mode">execution mode</label>
            <select id="ir-state-execution-mode" className={styles.select} value={props.executionMode} onChange={(event) => props.onExecutionModeChange(event.target.value as IRExecutionMode | "")} disabled={loading || !currentExecutionOptions}>
              <option value="">人が実行方式を選択してください</option>
              <option value="backend_raw" disabled={!currentExecutionOptions?.execution_modes.backend_raw.available}>backend_raw</option>
              <option value="native_code" disabled={!currentExecutionOptions?.execution_modes.native_code.available}>native_code</option>
            </select>
            {currentExecutionOptions && Object.entries(currentExecutionOptions.execution_modes).map(([mode, option]) => <p key={mode} className={option.available ? styles.helperText : styles.warningText}>{mode}: {option.available ? "利用可能" : `利用不可 — ${option.reason}`}</p>)}
            {props.executionOptionsError && <p className={styles.error} role="alert">{props.executionOptionsError}</p>}
          </div>}
          {rawCarrierBlocked && <p className={styles.warningText}>実効carrierは{rawCarrierKHz}kHzです。RAW送信はBackendと同じ38kHz条件を満たさないため利用できません。</p>}
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="ir-state-send-repeat">リピート回数 (repeat)</label>
            <input id="ir-state-send-repeat" className={styles.input} type="number" min="1" max="5" step="1" value={props.sendRepeat} onChange={(event) => props.onSendRepeatChange(event.target.value)} placeholder="空欄時は標準値を使用" disabled={loading} />
            {props.sendRepeatError && <p className={styles.schemaFieldError}>{props.sendRepeatError}</p>}
            <p className={styles.helperText}>空欄時はrepeatを送信せず、State、IR Adapter設定、1の順で標準値を解決します。</p>
          </div>
          <div className={styles.roomFormActions}><button type="button" className={styles.primaryButton} onClick={props.onSend} disabled={disabled || Boolean(stateReason) || rawCarrierBlocked || !codeModeAvailable || (isRawSamplesState && !isLearned)}>{selectedState.name} を送信</button></div>
        </div>
      )}

    </section>
  );
}
