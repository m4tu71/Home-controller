import type { FormEvent } from "react";

import type { Device, Room } from "../../../api/controller";

import { DeviceCard } from "./DeviceCard";
import { DeviceForm } from "./DeviceForm";
import styles from "../WebController.module.css";

type DeviceSectionProps = {
  showManagement?: boolean;
  settingsHref?: string;
  selectedRoom?: Room;
  roomsLoaded: boolean;
  hasRooms: boolean;
  roomLoading: boolean;
  roomError: string;
  devices: Device[];
  devicesLoaded: boolean;
  selectedDeviceId: string;
  deviceLoading: boolean;
  deviceMessage: string;
  deviceError: string;
  deviceCreateId: string;
  deviceCreateName: string;
  deviceCreateType: string;
  deviceCreatePrimaryAdapter: string;
  deviceCreateCapabilities: string;
  selectedDevice?: Device;
  deviceEditName: string;
  deviceEditType: string;
  deviceEditPrimaryAdapter: string;
  deviceEditCapabilities: string;
  onRefreshDevices: () => void;
  onSelectDevice: (deviceId: string) => void;
  onCreateDevice: (event: FormEvent<HTMLFormElement>) => void;
  onUpdateDevice: (event: FormEvent<HTMLFormElement>) => void;
  onDeleteDevice: () => void;
  onDeviceCreateIdChange: (value: string) => void;
  onDeviceCreateNameChange: (value: string) => void;
  onDeviceCreateTypeChange: (value: string) => void;
  onDeviceCreatePrimaryAdapterChange: (value: string) => void;
  onDeviceCreateCapabilitiesChange: (value: string) => void;
  onDeviceEditNameChange: (value: string) => void;
  onDeviceEditTypeChange: (value: string) => void;
  onDeviceEditPrimaryAdapterChange: (value: string) => void;
  onDeviceEditCapabilitiesChange: (value: string) => void;
};

export function DeviceSection({
  showManagement = true,
  settingsHref = "/settings",
  selectedRoom,
  roomsLoaded,
  hasRooms,
  roomLoading,
  roomError,
  devices,
  devicesLoaded,
  selectedDeviceId,
  deviceLoading,
  deviceMessage,
  deviceError,
  deviceCreateId,
  deviceCreateName,
  deviceCreateType,
  deviceCreatePrimaryAdapter,
  deviceCreateCapabilities,
  selectedDevice,
  deviceEditName,
  deviceEditType,
  deviceEditPrimaryAdapter,
  deviceEditCapabilities,
  onRefreshDevices,
  onSelectDevice,
  onCreateDevice,
  onUpdateDevice,
  onDeleteDevice,
  onDeviceCreateIdChange,
  onDeviceCreateNameChange,
  onDeviceCreateTypeChange,
  onDeviceCreatePrimaryAdapterChange,
  onDeviceCreateCapabilitiesChange,
  onDeviceEditNameChange,
  onDeviceEditTypeChange,
  onDeviceEditPrimaryAdapterChange,
  onDeviceEditCapabilitiesChange,
}: DeviceSectionProps) {
  const showEmptyState = Boolean(selectedRoom) && devicesLoaded && !deviceLoading && !deviceError && devices.length === 0;
  const showRoomSelectionPrompt = !selectedRoom && roomsLoaded && hasRooms && !roomError;
  const deviceListBusy = Boolean(selectedRoom) && (deviceLoading || !devicesLoaded);

  return (
    <section className={styles.deviceBlock} aria-busy={deviceListBusy || (!selectedRoom && roomLoading)}>
      <div className={styles.deviceHeader}>
        <h3 className={styles.deviceTitle}>Devices</h3>

        <button
          type="button"
          className={styles.statusButton}
          onClick={onRefreshDevices}
          disabled={deviceLoading || !selectedRoom}
        >
          一覧更新
        </button>
      </div>

      {!selectedRoom && !roomsLoaded && (
        <p className={styles.info} role="status">Room一覧を読み込んでいます...</p>
      )}
      {!selectedRoom && showRoomSelectionPrompt ? (
        <p className={styles.roomEmptyState} role="status">
          Device一覧を表示するRoomを選択してください。
        </p>
      ) : null}
      {selectedRoom && deviceListBusy && !deviceMessage && (
        <p className={styles.info} role="status">Device一覧を読み込んでいます...</p>
      )}
      {selectedRoom && devicesLoaded && !deviceLoading && !deviceError && devices.length > 0 ? (
        <div
          className={styles.deviceList}
          role="group"
          aria-label={`${selectedRoom.name} のDevice一覧`}
        >
          {devices.map((device) => (
            <DeviceCard
              key={device.id}
              device={device}
              isSelected={device.id === selectedDeviceId}
              onSelectDevice={onSelectDevice}
            />
          ))}
        </div>
      ) : showEmptyState ? (
        <p className={styles.roomEmptyState} role="status">
          このRoomにはDeviceがありません。
          {showManagement ? "この画面から作成できます。" : <> <a href={settingsHref}>Device設定</a>から登録できます。</>}
        </p>
      ) : null}

      {selectedRoom && !deviceError && (!showEmptyState || deviceMessage.startsWith("Device metadataをTrashへ移動しました:")) && deviceMessage && <p className={styles.info} role="status">{deviceMessage}</p>}
      {deviceError && <p className={styles.error} role="alert">{deviceError}</p>}

      {showManagement && <hr className={styles.divider} />}

      {showManagement && <DeviceForm
        selectedRoom={selectedRoom}
        deviceLoading={deviceLoading || !devicesLoaded}
        deviceCreateId={deviceCreateId}
        deviceCreateName={deviceCreateName}
        deviceCreateType={deviceCreateType}
        deviceCreatePrimaryAdapter={deviceCreatePrimaryAdapter}
        deviceCreateCapabilities={deviceCreateCapabilities}
        selectedDevice={selectedDevice}
        deviceEditName={deviceEditName}
        deviceEditType={deviceEditType}
        deviceEditPrimaryAdapter={deviceEditPrimaryAdapter}
        deviceEditCapabilities={deviceEditCapabilities}
        onCreateDevice={onCreateDevice}
        onUpdateDevice={onUpdateDevice}
        onDeviceCreateIdChange={onDeviceCreateIdChange}
        onDeviceCreateNameChange={onDeviceCreateNameChange}
        onDeviceCreateTypeChange={onDeviceCreateTypeChange}
        onDeviceCreatePrimaryAdapterChange={onDeviceCreatePrimaryAdapterChange}
        onDeviceCreateCapabilitiesChange={onDeviceCreateCapabilitiesChange}
        onDeviceEditNameChange={onDeviceEditNameChange}
        onDeviceEditTypeChange={onDeviceEditTypeChange}
        onDeviceEditPrimaryAdapterChange={onDeviceEditPrimaryAdapterChange}
        onDeviceEditCapabilitiesChange={onDeviceEditCapabilitiesChange}
      />}

      {showManagement && <div className={styles.roomDeletePanel}>
        <div>
          <p className={styles.warningText}>
            削除対象:{" "}
            {selectedDevice
              ? `${selectedDevice.name} (${selectedDevice.id})`
              : "-"}
          </p>
          <p className={styles.helperText}>
            Device削除はdevice.jsonのみを削除します。adapters/などの配下dataは再帰削除しません。
          </p>
        </div>

        <button
          type="button"
          className={styles.dangerButton}
          onClick={onDeleteDevice}
          disabled={deviceLoading || !selectedRoom || !selectedDevice}
        >
          Device削除
        </button>
      </div>}
    </section>
  );
}
