import type { FormEvent } from "react";

import type { Device, IRCommand, IRCommandFormat, IRExecutionMode, IRExecutionOptionsResponse, Room } from "../../../api/controller";

import { IRCommandCard } from "./IRCommandCard";
import { IRCommandForm } from "./IRCommandForm";
import styles from "../WebController.module.css";

type IRCommandSectionProps = {
  mode?: "operations" | "management";
  managementHref?: string;
  selectedRoom?: Room;
  selectedDevice?: Device;
  irConfigReady: boolean;
  irConfigError: string;
  irRemoteType: "command" | "stateful";
  irCommands: IRCommand[];
  selectedIRCommandId: string;
  irCommandMessage: string;
  irCommandError: string;
  irCommandLoading: boolean;
  irCommandListLoaded: boolean;
  irCommandCreateId: string;
  irCommandCreateName: string;
  irCommandCreateFormat: IRCommandFormat;
  irCommandCreatePreset: string;
  irCommandCreateRawTimings: string;
  irCommandCreateProtocol: string;
  irCommandCreateCode: string;
  irCommandCreateRepeat: string;
  irCommandEditName: string;
  irCommandEditFormat: IRCommandFormat;
  irCommandEditPreset: string;
  irCommandEditRawTimings: string;
  irCommandEditProtocol: string;
  irCommandEditCode: string;
  irCommandEditRepeat: string;
  irCommandCaptureSamples: string;
  irCommandCaptureMaxAttempts: string;
  irCommandSendRepeat: string;
  irCommandExecutionMode: IRExecutionMode | "";
  irCommandExecutionOptions: IRExecutionOptionsResponse | null;
  irCommandExecutionOptionsError: string;
  carrierKHz: number;
  onRefreshIRCommands: () => void;
  onSelectIRCommand: (command: IRCommand) => void;
  onCreateIRCommand: (event: FormEvent<HTMLFormElement>) => void;
  onUpdateIRCommand: (event: FormEvent<HTMLFormElement>) => void;
  onDeleteIRCommand: () => void;
  onCaptureIRCommand: () => void;
  onSendIRCommand: () => void;
  onIRCommandCreateIdChange: (value: string) => void;
  onIRCommandCreateNameChange: (value: string) => void;
  onIRCommandCreateFormatChange: (value: IRCommandFormat) => void;
  onIRCommandCreatePresetChange: (value: string) => void;
  onIRCommandCreateRawTimingsChange: (value: string) => void;
  onIRCommandCreateProtocolChange: (value: string) => void;
  onIRCommandCreateCodeChange: (value: string) => void;
  onIRCommandCreateRepeatChange: (value: string) => void;
  onIRCommandEditNameChange: (value: string) => void;
  onIRCommandEditFormatChange: (value: IRCommandFormat) => void;
  onIRCommandEditPresetChange: (value: string) => void;
  onIRCommandEditRawTimingsChange: (value: string) => void;
  onIRCommandEditProtocolChange: (value: string) => void;
  onIRCommandEditCodeChange: (value: string) => void;
  onIRCommandEditRepeatChange: (value: string) => void;
  onIRCommandCaptureSamplesChange: (value: string) => void;
  onIRCommandCaptureMaxAttemptsChange: (value: string) => void;
  onIRCommandSendRepeatChange: (value: string) => void;
  onIRCommandExecutionModeChange: (value: IRExecutionMode | "") => void;
};

export function IRCommandSection({
  mode = "management",
  managementHref = "/settings",
  selectedRoom,
  selectedDevice,
  irConfigReady,
  irConfigError,
  irRemoteType,
  irCommands,
  selectedIRCommandId,
  irCommandMessage,
  irCommandError,
  irCommandLoading,
  irCommandListLoaded,
  irCommandCreateId,
  irCommandCreateName,
  irCommandCreateFormat,
  irCommandCreatePreset,
  irCommandCreateRawTimings,
  irCommandCreateProtocol,
  irCommandCreateCode,
  irCommandCreateRepeat,
  irCommandEditName,
  irCommandEditFormat,
  irCommandEditPreset,
  irCommandEditRawTimings,
  irCommandEditProtocol,
  irCommandEditCode,
  irCommandEditRepeat,
  irCommandCaptureSamples,
  irCommandCaptureMaxAttempts,
  irCommandSendRepeat,
  irCommandExecutionMode,
  irCommandExecutionOptions,
  irCommandExecutionOptionsError,
  carrierKHz,
  onRefreshIRCommands,
  onSelectIRCommand,
  onCreateIRCommand,
  onUpdateIRCommand,
  onDeleteIRCommand,
  onCaptureIRCommand,
  onSendIRCommand,
  onIRCommandCreateIdChange,
  onIRCommandCreateNameChange,
  onIRCommandCreateFormatChange,
  onIRCommandCreatePresetChange,
  onIRCommandCreateRawTimingsChange,
  onIRCommandCreateProtocolChange,
  onIRCommandCreateCodeChange,
  onIRCommandCreateRepeatChange,
  onIRCommandEditNameChange,
  onIRCommandEditFormatChange,
  onIRCommandEditPresetChange,
  onIRCommandEditRawTimingsChange,
  onIRCommandEditProtocolChange,
  onIRCommandEditCodeChange,
  onIRCommandEditRepeatChange,
  onIRCommandCaptureSamplesChange,
  onIRCommandCaptureMaxAttemptsChange,
  onIRCommandSendRepeatChange,
  onIRCommandExecutionModeChange,
}: IRCommandSectionProps) {
  const managementMode = mode === "management";
  if (!selectedDevice || selectedDevice.primary_adapter !== "ir") {
    return null;
  }

  if (!irConfigReady) {
    return (
      <section className={styles.deviceBlock} aria-busy={!irConfigError}>
        <h3 className={styles.deviceTitle}>IR Command</h3>
        {irConfigError ? (
          <p className={styles.error} role="alert">
            IR Adapter設定を確認できないためCommand / Stateを判定できません: {irConfigError}。Device設定から再取得してください。
          </p>
        ) : (
          <p className={styles.info} role="status">IR Adapter設定を確認しています...</p>
        )}
      </section>
    );
  }

  if (irRemoteType !== "command") {
    return null;
  }

  const selectedCommand = irCommands.find(
    (command) => command.id === selectedIRCommandId
  );
  const showEmptyState = irCommandListLoaded && !irCommandLoading && !irCommandError && irCommands.length === 0;
  const formDisabled = irCommandLoading || !selectedRoom || !selectedDevice;
  const isRawSamplesLearned =
    selectedCommand?.format === "raw_samples" &&
    ((selectedCommand.captures?.length ?? 0) > 0 ||
      (selectedCommand.timings_us?.length ?? 0) > 0 ||
      (selectedCommand.count ?? 0) > 0);
  const rawCarrierKHz = selectedCommand?.carrier_khz ?? carrierKHz;
  const rawCarrierBlocked = (selectedCommand?.format === "raw_samples" || selectedCommand?.format === "raw") && rawCarrierKHz !== 38;
  const currentExecutionOptions = irCommandExecutionOptions?.protocol === selectedCommand?.protocol ? irCommandExecutionOptions : null;
  const selectedCodeMode = irCommandExecutionMode ? currentExecutionOptions?.execution_modes[irCommandExecutionMode] : undefined;
  const codeModeAvailable = selectedCommand?.format !== "code" || Boolean(selectedCodeMode?.available);

  return (
      <section className={styles.deviceBlock} aria-busy={irCommandLoading}>
      <div className={styles.deviceHeader}>
        <h3 className={styles.deviceTitle}>IR Command</h3>
        <button
          type="button"
          className={styles.statusButton}
          onClick={onRefreshIRCommands}
          disabled={formDisabled}
        >
          一覧更新
        </button>
      </div>

      <div className={styles.roomStatusGrid}>
        <div className={styles.statusLabel}>対象Room</div>
        <div className={styles.statusValue}>
          {selectedRoom ? `${selectedRoom.name} (${selectedRoom.id})` : "-"}
        </div>
        <div className={styles.statusLabel}>対象Device</div>
        <div className={styles.statusValue}>
          {selectedDevice.name} ({selectedDevice.id})
        </div>
        <div className={styles.statusLabel}>Command数</div>
        <div className={styles.statusValue}>{irCommands.length}</div>
      </div>

      {!irCommandListLoaded && !irCommandMessage && <p className={styles.info} role="status">IR Command一覧を読み込んでいます...</p>}
      {irCommandListLoaded && irCommands.length > 0 ? (
        <div className={`${styles.irOperationButtons} ${styles.irCommandButtons}`} role="group" aria-label="IR Command操作">
          {irCommands.map((command) => (
            <IRCommandCard
              key={command.id}
              command={command}
              isSelected={command.id === selectedIRCommandId}
              carrierKHz={carrierKHz}
              disabled={irCommandLoading}
              codeExecutionModeSelected={command.id === selectedIRCommandId && Boolean(selectedCodeMode?.available)}
              onSelectCommand={onSelectIRCommand}
            />
          ))}
        </div>
      ) : showEmptyState ? (
        <p className={styles.roomEmptyState} role="status">
          IR Commandはまだ登録されていません。
          {managementMode ? "下のCommand作成フォームから登録できます。" : <> <a href={managementHref}>Settings</a>から登録できます。</>}
        </p>
      ) : null}

      {irCommandMessage && !showEmptyState && <p className={styles.info} role="status">{irCommandMessage}</p>}
      {irCommandError && <p className={styles.error} role="alert">{irCommandError}</p>}

      <p className={styles.helperText}>
        このDevice配下のIR Command dataは、旧IR Signal dataとは独立して管理されます。
      </p>

      {managementMode && <>
      <hr className={styles.divider} />

      <IRCommandForm
        selectedDevice={Boolean(selectedDevice)}
        selectedCommand={selectedCommand}
        loading={irCommandLoading}
        createId={irCommandCreateId}
        createName={irCommandCreateName}
        createFormat={irCommandCreateFormat}
        createPreset={irCommandCreatePreset}
        createRawTimings={irCommandCreateRawTimings}
        createProtocol={irCommandCreateProtocol}
        createCode={irCommandCreateCode}
        createRepeat={irCommandCreateRepeat}
        editName={irCommandEditName}
        editFormat={irCommandEditFormat}
        editPreset={irCommandEditPreset}
        editRawTimings={irCommandEditRawTimings}
        editProtocol={irCommandEditProtocol}
        editCode={irCommandEditCode}
        editRepeat={irCommandEditRepeat}
        onCreate={onCreateIRCommand}
        onUpdate={onUpdateIRCommand}
        onCreateIdChange={onIRCommandCreateIdChange}
        onCreateNameChange={onIRCommandCreateNameChange}
        onCreateFormatChange={onIRCommandCreateFormatChange}
        onCreatePresetChange={onIRCommandCreatePresetChange}
        onCreateRawTimingsChange={onIRCommandCreateRawTimingsChange}
        onCreateProtocolChange={onIRCommandCreateProtocolChange}
        onCreateCodeChange={onIRCommandCreateCodeChange}
        onCreateRepeatChange={onIRCommandCreateRepeatChange}
        onEditNameChange={onIRCommandEditNameChange}
        onEditFormatChange={onIRCommandEditFormatChange}
        onEditPresetChange={onIRCommandEditPresetChange}
        onEditRawTimingsChange={onIRCommandEditRawTimingsChange}
        onEditProtocolChange={onIRCommandEditProtocolChange}
        onEditCodeChange={onIRCommandEditCodeChange}
        onEditRepeatChange={onIRCommandEditRepeatChange}
      />

      {selectedCommand?.format === "raw_samples" && (
        <div className={styles.irCommandCapturePanel}>
          <h4 className={styles.formTitle}>
            {(selectedCommand.captures?.length ?? 0) > 0 ||
            (selectedCommand.timings_us?.length ?? 0) > 0
              ? "IR Command再学習"
              : "IR Command学習"}
          </h4>
          <p className={styles.helperText}>
            学習を開始したら、リモコンを受信機へ向けて対象ボタンを押してください。
          </p>
          <div className={styles.formRow}>
            <div className={styles.formGroup}>
              <label className={styles.label} htmlFor="ir-command-capture-samples">
                samples
              </label>
              <input
                id="ir-command-capture-samples"
                className={styles.input}
                type="number"
                min="1"
                max="7"
                step="1"
                value={irCommandCaptureSamples}
                onChange={(event) =>
                  onIRCommandCaptureSamplesChange(event.target.value)
                }
                disabled={irCommandLoading}
              />
            </div>
            <div className={styles.formGroup}>
              <label className={styles.label} htmlFor="ir-command-capture-max-attempts">
                max_attempts
              </label>
              <input
                id="ir-command-capture-max-attempts"
                className={styles.input}
                type="number"
                min="1"
                max="20"
                step="1"
                value={irCommandCaptureMaxAttempts}
                onChange={(event) =>
                  onIRCommandCaptureMaxAttemptsChange(event.target.value)
                }
                disabled={irCommandLoading}
              />
            </div>
          </div>
          <div className={styles.roomFormActions}>
            <button
              type="button"
              className={styles.primaryButton}
              onClick={onCaptureIRCommand}
              disabled={irCommandLoading}
            >
              {(selectedCommand.captures?.length ?? 0) > 0 ||
              (selectedCommand.timings_us?.length ?? 0) > 0
                ? "再学習を開始"
                : "学習を開始"}
            </button>
          </div>
          <p className={styles.helperText}>
            学習に失敗しても既存の学習結果は維持されます。
          </p>
        </div>
      )}
      </>}

      {!managementMode && selectedCommand && (
        <div className={styles.irCommandSendPanel}>
          <h4 className={styles.formTitle}>IR Command送信</h4>
          <p className={styles.helperText}>送信結果はdeliveryの報告であり、家電の現在状態は取得しません。</p>
          <p className={styles.helperText}>
            {selectedCommand.format === "raw_samples"
              ? isRawSamplesLearned
                ? "学習済みRAW信号を送信します。"
                : "未学習のraw_samples Commandは送信できません。先に学習してください。"
              : selectedCommand.format === "preset_command" ? "Pico firmwareの許可済みプリセットコマンドを送信します。" : selectedCommand.format === "raw" ? "保存済みRAW timingsを送信します。" : "選択したexecution modeでcodeを送信します。"}
          </p>
          {selectedCommand.format === "code" && <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="ir-command-execution-mode">execution mode</label>
            <select id="ir-command-execution-mode" className={styles.select} value={irCommandExecutionMode} onChange={(event) => onIRCommandExecutionModeChange(event.target.value as IRExecutionMode | "")} disabled={irCommandLoading || !currentExecutionOptions}>
              <option value="">人が実行方式を選択してください</option>
              <option value="backend_raw" disabled={!currentExecutionOptions?.execution_modes.backend_raw.available}>backend_raw</option>
              <option value="native_code" disabled={!currentExecutionOptions?.execution_modes.native_code.available}>native_code</option>
            </select>
            {currentExecutionOptions && Object.entries(currentExecutionOptions.execution_modes).map(([mode, option]) => <p key={mode} className={option.available ? styles.helperText : styles.warningText}>{mode}: {option.available ? "利用可能" : `利用不可 — ${option.reason}`}</p>)}
            {irCommandExecutionOptionsError && <p className={styles.error} role="alert">{irCommandExecutionOptionsError}</p>}
          </div>}
          {rawCarrierBlocked && <p className={styles.warningText}>実効carrierは{rawCarrierKHz}kHzです。RAW送信はBackendと同じ38kHz条件を満たさないため利用できません。</p>}
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="ir-command-send-repeat">
              リピート回数 (repeat)
            </label>
            <input
              id="ir-command-send-repeat"
              className={styles.input}
              type="number"
              min="1"
              max="5"
              step="1"
              value={irCommandSendRepeat}
              onChange={(event) => onIRCommandSendRepeatChange(event.target.value)}
              placeholder="空欄時は標準値を使用"
              disabled={irCommandLoading}
            />
            <p className={styles.helperText}>
              空欄時はrepeatを送信せず、Command、IR Adapter設定、1の順で標準値を解決します。
            </p>
          </div>
          <div className={styles.roomFormActions}>
            <button
              type="button"
              className={styles.primaryButton}
              onClick={onSendIRCommand}
              disabled={
                irCommandLoading ||
                rawCarrierBlocked ||
                !codeModeAvailable ||
                (selectedCommand.format === "raw_samples" && !isRawSamplesLearned)
              }
            >
              {selectedCommand.name} を送信
            </button>
          </div>
        </div>
      )}

      {managementMode && <div className={styles.roomDeletePanel}>
        <div>
          <p className={styles.warningText}>
            削除対象: {selectedCommand ? `${selectedCommand.name} (${selectedCommand.id})` : "-"}
          </p>
          <p className={styles.helperText}>削除前に確認ダイアログを表示します。</p>
        </div>
        <button
          type="button"
          className={styles.dangerButton}
          onClick={onDeleteIRCommand}
          disabled={irCommandLoading || !selectedCommand}
        >
          IR Command削除
        </button>
      </div>}
    </section>
  );
}
