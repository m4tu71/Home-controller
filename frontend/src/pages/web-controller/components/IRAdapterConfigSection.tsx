import type { FormEvent } from "react";

import type { Device, IRConfig, Room } from "../../../api/controller";

import styles from "../WebController.module.css";

type IRAdapterConfigSectionProps = {
  selectedRoom?: Room;
  selectedDevice?: Device;
  irConfigRemoteType: IRConfig["ir_remote_type"];
  irConfigCarrierKHz: string;
  irConfigDefaultRepeat: string;
  irConfigMessage: string;
  irConfigError: string;
  irConfigLoading: boolean;
  configReady: boolean;
  onRefreshIRConfig: () => void;
  onUpdateIRConfig: (event: FormEvent<HTMLFormElement>) => void;
  onIRConfigRemoteTypeChange: (value: IRConfig["ir_remote_type"]) => void;
  onIRConfigCarrierKHzChange: (value: string) => void;
  onIRConfigDefaultRepeatChange: (value: string) => void;
};

export function IRAdapterConfigSection({
  selectedRoom,
  selectedDevice,
  irConfigRemoteType,
  irConfigCarrierKHz,
  irConfigDefaultRepeat,
  irConfigMessage,
  irConfigError,
  irConfigLoading,
  configReady,
  onRefreshIRConfig,
  onUpdateIRConfig,
  onIRConfigRemoteTypeChange,
  onIRConfigCarrierKHzChange,
  onIRConfigDefaultRepeatChange,
}: IRAdapterConfigSectionProps) {
  const isIRDevice = selectedDevice?.primary_adapter === "ir";
  const formDisabled = irConfigLoading || !configReady || !selectedRoom || !selectedDevice;

  if (!selectedDevice) {
    return (
      <section className={styles.deviceBlock}>
        <div className={styles.deviceHeader}>
          <h3 className={styles.deviceTitle}>IR Adapter設定</h3>
        </div>

        <p className={styles.roomEmptyState} role="status">
          IR Adapter設定を表示するDeviceを選択してください。
        </p>
      </section>
    );
  }

  if (!isIRDevice) {
    return null;
  }

  return (
    <section className={styles.deviceBlock} aria-busy={irConfigLoading || (!configReady && !irConfigError)}>
      <div className={styles.deviceHeader}>
          <h3 className={styles.deviceTitle}>IR Adapter設定</h3>

        <button
          type="button"
          className={styles.statusButton}
          onClick={onRefreshIRConfig}
          disabled={irConfigLoading || !selectedRoom || !selectedDevice}
        >
          設定更新
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
      </div>

      {irConfigMessage && <p className={styles.info} role="status">{irConfigMessage}</p>}
      {irConfigError && <p className={styles.error} role="alert">{irConfigError}</p>}
      {!configReady && !irConfigLoading && !irConfigError && <p className={styles.info} role="status">IR Adapter設定を読み込んでいます...</p>}

      <form className={styles.irConfigForm} onSubmit={onUpdateIRConfig}>
        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="ir-config-remote-type">
              リモコン種別 (ir_remote_type)
            </label>
            <select
              id="ir-config-remote-type"
              className={styles.select}
              value={irConfigRemoteType}
              onChange={(event) =>
                onIRConfigRemoteTypeChange(
                  event.target.value as IRConfig["ir_remote_type"]
                )
              }
              disabled={formDisabled}
            >
              <option value="command">単発コマンド型 (command)</option>
              <option value="stateful">状態管理型 (stateful)</option>
            </select>
          </div>

          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="ir-config-carrier-khz">
              搬送波周波数 kHz (carrier_khz)
            </label>
            <input
              id="ir-config-carrier-khz"
              className={styles.input}
              type="number"
              min="1"
              step="1"
              value={irConfigCarrierKHz}
              onChange={(event) =>
                onIRConfigCarrierKHzChange(event.target.value)
              }
              disabled={formDisabled}
            />
          </div>
        </div>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="ir-config-repeat">
              標準リピート回数 (default_repeat)
            </label>
            <input
              id="ir-config-repeat"
              className={styles.input}
              type="number"
              min="0"
              step="1"
              value={irConfigDefaultRepeat}
              onChange={(event) =>
                onIRConfigDefaultRepeatChange(event.target.value)
              }
              disabled={formDisabled}
            />
          </div>

          <div className={styles.roomFormActions}>
            <button
              type="submit"
              className={styles.primaryButton}
              disabled={formDisabled}
            >
              保存
            </button>
          </div>
        </div>
      </form>
    </section>
  );
}
