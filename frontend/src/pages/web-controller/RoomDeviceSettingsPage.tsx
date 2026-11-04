import { Link, useNavigate, useSearchParams } from "react-router-dom";

import { DeviceSection } from "./components/DeviceSection";
import { IRAdapterConfigSection } from "./components/IRAdapterConfigSection";
import { IRCommandSection } from "./components/IRCommandSection";
import { IRStateSection } from "./components/IRStateSection";
import { IRStateSchemaSection } from "./components/IRStateSchemaSection";
import { RoomSection } from "./components/RoomSection";
import { useRoomDeviceController } from "./hooks/useRoomDeviceController";
import { useIRController } from "./hooks/useIRController";
import styles from "./WebController.module.css";

function settingsHref(roomId: string, deviceId: string) {
  const query = new URLSearchParams();
  if (roomId) query.set("room", roomId);
  if (deviceId) query.set("device", deviceId);
  const search = query.toString();
  return search ? `/settings?${search}` : "/settings";
}

function homeHref(roomId: string, deviceId: string) {
  if (!roomId) return "/";
  const query = new URLSearchParams();
  if (deviceId) query.set("device", deviceId);
  const search = query.toString();
  return `/rooms/${encodeURIComponent(roomId)}${search ? `?${search}` : ""}`;
}

export function RoomDeviceSettingsPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const routeRoomId = searchParams.get("room") || undefined;
  const routeDeviceId = searchParams.get("device") || undefined;
  const model = useRoomDeviceController(
    routeRoomId,
    (roomId) => navigate(settingsHref(roomId, "")),
    routeDeviceId,
  );
  const ir = useIRController(model.selection);
  const { selectedRoomId, selectedDeviceId, selectedRoom, selectedDevice } = model.selection;

  return (
    <main className={styles.page}>
      <div className={styles.container}>
        <header className={styles.header}>
          <p className={styles.label}>Settings</p>
          <h1 className={styles.title}>Room / Device設定</h1>
          <p className={styles.description}>
            Room / Deviceの登録・編集・削除、および選択中IR DeviceのAdapter設定とState Schemaを管理します。
          </p>
          <Link className={styles.button} to={homeHref(selectedRoomId, selectedDeviceId)}>
            選択中のRoom / Deviceへ戻る
          </Link>
        </header>

        <section className={styles.pageGroup} aria-labelledby="room-device-management-title">
          <h2 id="room-device-management-title" className={styles.deviceTitle}>Room / Device管理</h2>
          <RoomSection
            {...model.roomSectionProps}
            showManagement
            settingsHref={settingsHref(selectedRoomId, selectedDeviceId)}
          />
          <DeviceSection
            {...model.deviceSectionProps}
            showManagement
            settingsHref={settingsHref(selectedRoomId, selectedDeviceId)}
          />
          <p className={styles.helperText}>
            選択中: {selectedRoom ? `${selectedRoom.name} (${selectedRoom.id})` : "Roomなし"}
            {selectedDevice ? ` / ${selectedDevice.name} (${selectedDevice.id})` : " / Deviceなし"}
          </p>
        </section>

        <section className={styles.pageGroup} aria-labelledby="ir-settings-title">
          <h2 id="ir-settings-title" className={styles.deviceTitle}>IR Adapter / State Schema設定</h2>
          {selectedDevice?.primary_adapter === "ir" ? (
            <>
              <IRAdapterConfigSection {...ir.irAdapterConfigProps} />
              <IRStateSchemaSection {...ir.irStateSchemaProps} />
            </>
          ) : (
            <p className={styles.roomEmptyState} role="status">
              IR Adapter設定を行うIR Deviceを選択してください。
            </p>
          )}
        </section>

        <section className={styles.pageGroup} aria-labelledby="ir-management-title">
          <h2 id="ir-management-title" className={styles.deviceTitle}>IR Command / State管理</h2>
          {selectedDevice?.primary_adapter === "ir" ? (
            <>
              <IRCommandSection {...ir.irCommandProps} mode="management" managementHref={settingsHref(selectedRoomId, selectedDeviceId)} />
              <IRStateSection {...ir.irStateProps} mode="management" managementHref={settingsHref(selectedRoomId, selectedDeviceId)} />
            </>
          ) : (
            <p className={styles.roomEmptyState} role="status">Command / Stateを管理するIR Deviceを選択してください。</p>
          )}
        </section>
      </div>
    </main>
  );
}

export default RoomDeviceSettingsPage;
