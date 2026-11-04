import { Link } from "react-router-dom";

import { DeviceSection } from "./components/DeviceSection";
import { IRCommandSection } from "./components/IRCommandSection";
import { IRStateSection } from "./components/IRStateSection";
import { RoomSection } from "./components/RoomSection";
import styles from "./WebController.module.css";
import type { WebControllerPageModel } from "./hooks/useWebControllerModel";

function settingsHref(roomId: string, deviceId: string) {
  const query = new URLSearchParams();
  if (roomId) query.set("room", roomId);
  if (deviceId) query.set("device", deviceId);
  const search = query.toString();
  return search ? `/settings?${search}` : "/settings";
}

type WebControllerViewProps = {
  model: WebControllerPageModel;
};

export function WebControllerView({ model }: WebControllerViewProps) {
  const selectedRoomId = model.roomDevice.selection.selectedRoomId;
  const selectedDeviceId = model.roomDevice.selection.selectedDeviceId;
  const currentSettingsHref = settingsHref(selectedRoomId, selectedDeviceId);
  return (
    <main className={styles.page}>
      <div className={styles.container}>
        <header className={styles.header}>
          <p className={styles.label}>Web Controller</p>
          <h1 className={styles.title}>Home Controller</h1>
          <p className={styles.description}>
            RoomとDeviceを選択し、日常操作と設定を行います。
          </p>
        </header>

        <section className={styles.pageGroup} aria-labelledby="room-device-group-title">
          <h2 id="room-device-group-title" className={styles.deviceTitle}>Room / Device選択</h2>
          <RoomSection {...model.roomDevice.roomSectionProps} showManagement={false} settingsHref={currentSettingsHref} />

          <DeviceSection {...model.roomDevice.deviceSectionProps} showManagement={false} settingsHref={currentSettingsHref} />
          <p><a className={styles.button} href={currentSettingsHref}>Room / Device / IR設定</a></p>

        </section>

        <section className={styles.pageGroup} aria-labelledby="daily-operations-group-title">
          <h2 id="daily-operations-group-title" className={styles.deviceTitle}>日常操作</h2>
          <p className={styles.maintenanceDescription}>選択中Deviceに応じてIR CommandまたはIR Stateの操作を表示します。</p>

          <IRStateSection {...model.ir.irStateProps} mode="operations" managementHref={currentSettingsHref} />

          <IRCommandSection {...model.ir.irCommandProps} mode="operations" managementHref={currentSettingsHref} />

        </section>

        <section className={styles.maintenanceArea} aria-labelledby="maintenance-title">
          <div>
            <h2 id="maintenance-title" className={styles.deviceTitle}>
              保守管理
            </h2>
            <p className={styles.maintenanceDescription}>
              Data Health、Migration、operation履歴、Trashは専用画面で確認します。
            </p>
            <div className={styles.pageLinks}>
              <Link className={styles.button} to="/maintenance">Maintenance</Link>
              <Link className={styles.button} to="/trash">Trash</Link>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}

export default WebControllerView;
