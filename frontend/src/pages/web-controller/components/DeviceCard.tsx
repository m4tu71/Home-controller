import type { Device } from "../../../api/controller";

import { InfoPopover } from "./InfoPopover";
import infoStyles from "./InfoPopover.module.css";
import styles from "../WebController.module.css";

type DeviceCardProps = {
  device: Device;
  isSelected: boolean;
  onSelectDevice: (deviceId: string) => void;
};

export function DeviceCard({
  device,
  isSelected,
  onSelectDevice,
}: DeviceCardProps) {
  const capabilities =
    device.capabilities && device.capabilities.length > 0
      ? device.capabilities.join(", ")
      : "-";

  return (
    <article
      className={
        isSelected
          ? `${styles.deviceCard} ${styles.deviceCardSelected}`
          : styles.deviceCard
      }
    >
      <div className={styles.deviceCardHeader}>
        <div>
          <h4 className={styles.deviceCardTitle}>{device.name}</h4>
          <p className={styles.helperText}>{device.id}</p>
        </div>

        <div className={styles.deviceCardHeaderActions}>
          <span className={styles.badge}>
            {isSelected ? "selected" : "device"}
          </span>
          <InfoPopover
            subject={`${device.name} (${device.id})`}
            title={`${device.name} のDevice情報`}
          >
            <dl className={infoStyles.details}>
              <div><dt>Device ID</dt><dd>{device.id}</dd></div>
              <div><dt>種別</dt><dd>{device.device_type}</dd></div>
              <div><dt>Primary adapter</dt><dd>{device.primary_adapter}</dd></div>
              <div><dt>Capabilities</dt><dd>{capabilities}</dd></div>
            </dl>
            <p className={styles.helperText}>{device.primary_adapter === "ir" ? "IR Deviceを選択すると、下の「日常操作」が選択内容に合わせて切り替わります。" : "Deviceを選択すると、このDeviceに対応する日常操作が表示されます。"}</p>
          </InfoPopover>
        </div>
      </div>

      <dl className={styles.deviceMetaGrid}>
        <div>
          <dt>type</dt>
          <dd>{device.device_type}</dd>
        </div>
        <div>
          <dt>adapter</dt>
          <dd>{device.primary_adapter}</dd>
        </div>
        <div>
          <dt>capabilities</dt>
          <dd>{capabilities}</dd>
        </div>
      </dl>

      <div className={styles.deviceCardActions}>
        <button
          type="button"
          className={styles.button}
          onClick={() => onSelectDevice(device.id)}
          aria-pressed={isSelected}
          aria-label={`${device.name} (${device.id}) を選択`}
        >
          選択
        </button>
      </div>
    </article>
  );
}
