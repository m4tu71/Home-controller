import type { FormEvent } from "react";

import type { Device, Room } from "../../../api/controller";

import styles from "../WebController.module.css";

type DeviceFormProps = {
  selectedRoom?: Room;
  deviceLoading: boolean;
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
  onCreateDevice: (event: FormEvent<HTMLFormElement>) => void;
  onUpdateDevice: (event: FormEvent<HTMLFormElement>) => void;
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

export function DeviceForm({
  selectedRoom,
  deviceLoading,
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
  onCreateDevice,
  onUpdateDevice,
  onDeviceCreateIdChange,
  onDeviceCreateNameChange,
  onDeviceCreateTypeChange,
  onDeviceCreatePrimaryAdapterChange,
  onDeviceCreateCapabilitiesChange,
  onDeviceEditNameChange,
  onDeviceEditTypeChange,
  onDeviceEditPrimaryAdapterChange,
  onDeviceEditCapabilitiesChange,
}: DeviceFormProps) {
  const createDisabled = deviceLoading || !selectedRoom;
  const editDisabled = deviceLoading || !selectedRoom || !selectedDevice;

  return (
    <div className={styles.deviceForm}>
      <form className={styles.deviceSubForm} onSubmit={onCreateDevice}>
        <h4 className={styles.formTitle}>Device作成</h4>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-create-room">
              Room
            </label>
            <div className={styles.readOnlyValue} id="device-create-room">
              {selectedRoom ? selectedRoom.id : "-"}
            </div>
            <p className={styles.helperText}>
              Deviceは選択中Roomに作成されます。
            </p>
          </div>

          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-create-id">
              Device ID
            </label>
            <input
              id="device-create-id"
              className={styles.input}
              value={deviceCreateId}
              onChange={(event) => onDeviceCreateIdChange(event.target.value)}
              placeholder="bedroom_ac"
              disabled={createDisabled}
            />
            <p className={styles.helperText}>
              小文字のsnake_caseで入力します。
            </p>
          </div>
        </div>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-create-name">
              表示名
            </label>
            <input
              id="device-create-name"
              className={styles.input}
              value={deviceCreateName}
              onChange={(event) => onDeviceCreateNameChange(event.target.value)}
              placeholder="寝室エアコン"
              disabled={createDisabled}
            />
          </div>

          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-create-type">
              Device種別
            </label>
            <select
              id="device-create-type"
              className={styles.select}
              value={deviceCreateType}
              onChange={(event) => onDeviceCreateTypeChange(event.target.value)}
              disabled={createDisabled}
            >
              <option value="light">light</option>
              <option value="air_conditioner">air_conditioner</option>
              <option value="tv">tv</option>
              <option value="fan">fan</option>
              <option value="switch">switch</option>
              <option value="pc">pc</option>
              <option value="unknown">unknown</option>
            </select>
          </div>
        </div>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-create-adapter">
              主操作方式
            </label>
            <select
              id="device-create-adapter"
              className={styles.select}
              value={deviceCreatePrimaryAdapter}
              onChange={(event) =>
                onDeviceCreatePrimaryAdapterChange(event.target.value)
              }
              disabled={createDisabled}
            >
              <option value="ir">ir</option>
            </select>
          </div>

          <div className={styles.formGroup}>
            <label
              className={styles.label}
              htmlFor="device-create-capabilities"
            >
              Capabilities
            </label>
            <input
              id="device-create-capabilities"
              className={styles.input}
              value={deviceCreateCapabilities}
              onChange={(event) =>
                onDeviceCreateCapabilitiesChange(event.target.value)
              }
              placeholder="power, mode, fan"
              disabled={createDisabled}
            />
            <p className={styles.helperText}>カンマ区切りで入力します。</p>
          </div>
        </div>

        <div className={styles.roomFormActions}>
          <button
            type="submit"
            className={styles.primaryButton}
            disabled={createDisabled}
          >
            Device作成
          </button>
        </div>
      </form>

      <form className={styles.deviceSubForm} onSubmit={onUpdateDevice}>
        <h4 className={styles.formTitle}>Device編集</h4>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label}>Device ID</label>
            <div className={styles.readOnlyValue}>
              {selectedDevice ? selectedDevice.id : "-"}
            </div>
            <p className={styles.helperText}>Device IDは変更できません。</p>
          </div>

          <div className={styles.formGroup}>
            <label className={styles.label}>Room ID</label>
            <div className={styles.readOnlyValue}>
              {selectedDevice ? selectedDevice.room_id : "-"}
            </div>
            <p className={styles.helperText}>Room IDは変更できません。</p>
          </div>
        </div>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-edit-name">
              表示名
            </label>
            <input
              id="device-edit-name"
              className={styles.input}
              value={deviceEditName}
              onChange={(event) => onDeviceEditNameChange(event.target.value)}
              disabled={editDisabled}
            />
          </div>

          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-edit-type">
              Device種別
            </label>
            <select
              id="device-edit-type"
              className={styles.select}
              value={deviceEditType}
              onChange={(event) => onDeviceEditTypeChange(event.target.value)}
              disabled={editDisabled}
            >
              <option value="light">light</option>
              <option value="air_conditioner">air_conditioner</option>
              <option value="tv">tv</option>
              <option value="fan">fan</option>
              <option value="switch">switch</option>
              <option value="pc">pc</option>
              <option value="unknown">unknown</option>
            </select>
          </div>
        </div>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-edit-adapter">
              主操作方式
            </label>
            <select
              id="device-edit-adapter"
              className={styles.select}
              value={deviceEditPrimaryAdapter}
              onChange={(event) =>
                onDeviceEditPrimaryAdapterChange(event.target.value)
              }
              disabled={editDisabled}
            >
              <option value="ir">ir</option>
            </select>
          </div>

          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="device-edit-capabilities">
              Capabilities
            </label>
            <input
              id="device-edit-capabilities"
              className={styles.input}
              value={deviceEditCapabilities}
              onChange={(event) =>
                onDeviceEditCapabilitiesChange(event.target.value)
              }
              placeholder="power, mode, fan"
              disabled={editDisabled}
            />
            <p className={styles.helperText}>カンマ区切りで入力します。</p>
          </div>
        </div>

        <div className={styles.roomFormActions}>
          <button
            type="submit"
            className={styles.primaryButton}
            disabled={editDisabled}
          >
            Device更新
          </button>
        </div>
      </form>
    </div>
  );
}
