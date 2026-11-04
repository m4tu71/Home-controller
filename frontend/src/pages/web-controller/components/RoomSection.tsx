import type { FormEvent } from "react";

import type { Room } from "../../../api/controller";

import { InfoPopover } from "./InfoPopover";
import infoStyles from "./InfoPopover.module.css";
import styles from "../WebController.module.css";

type RoomSectionProps = {
  showManagement?: boolean;
  settingsHref?: string;
  rooms: Room[];
  roomsLoaded: boolean;
  selectedRoom?: Room;
  deviceCount: number | null;
  selectedRoomId: string;
  roomLoading: boolean;
  roomMessage: string;
  roomError: string;
  roomCreateId: string;
  roomCreateName: string;
  roomCreateSortOrder: string;
  roomEditName: string;
  roomEditSortOrder: string;
  onRefreshRooms: () => void;
  onSelectRoom: (room: Room) => void;
  onCreateRoom: (event: FormEvent<HTMLFormElement>) => void;
  onUpdateRoom: (event: FormEvent<HTMLFormElement>) => void;
  onDeleteRoom: () => void;
  onRoomCreateIdChange: (value: string) => void;
  onRoomCreateNameChange: (value: string) => void;
  onRoomCreateSortOrderChange: (value: string) => void;
  onRoomEditNameChange: (value: string) => void;
  onRoomEditSortOrderChange: (value: string) => void;
};

export function RoomSection({
  showManagement = true,
  settingsHref = "/settings",
  rooms,
  roomsLoaded,
  selectedRoom,
  deviceCount,
  selectedRoomId,
  roomLoading,
  roomMessage,
  roomError,
  roomCreateId,
  roomCreateName,
  roomCreateSortOrder,
  roomEditName,
  roomEditSortOrder,
  onRefreshRooms,
  onSelectRoom,
  onCreateRoom,
  onUpdateRoom,
  onDeleteRoom,
  onRoomCreateIdChange,
  onRoomCreateNameChange,
  onRoomCreateSortOrderChange,
  onRoomEditNameChange,
  onRoomEditSortOrderChange,
}: RoomSectionProps) {
  const showEmptyState = roomsLoaded && !roomLoading && !roomError && rooms.length === 0;

  return (
    <section className={styles.roomStatusBlock} aria-busy={roomLoading}>
      <div className={styles.deviceHeader}>
        <h3 className={styles.deviceTitle}>Rooms</h3>
        <div className={styles.sectionHeaderActions}>
          <button
            type="button"
            className={styles.statusButton}
            onClick={onRefreshRooms}
            disabled={roomLoading}
          >
            一覧更新
          </button>
          {selectedRoom && (
            <InfoPopover subject={`Room ${selectedRoom.name}`} title={`${selectedRoom.name} のRoom情報`}>
              <dl className={infoStyles.details}>
                <div><dt>Room ID</dt><dd>{selectedRoom.id}</dd></div>
                <div><dt>表示優先度</dt><dd>{selectedRoom.sort_order ?? "未設定"}</dd></div>
              </dl>
              <p className={styles.helperText}>Roomを選択すると、Device一覧と日常操作の対象が切り替わります。</p>
            </InfoPopover>
          )}
        </div>
      </div>

      {rooms.length > 0 ? (
        <div className={styles.roomTabs} role="group" aria-label="Rooms">
          {rooms.map((room) => {
            const isSelected = room.id === selectedRoomId;

            return (
                <button
                  key={room.id}
                  type="button"
                  className={
                    isSelected
                      ? `${styles.roomTab} ${styles.roomTabActive}`
                      : styles.roomTab
                  }
                  onClick={() => onSelectRoom(room)}
                  aria-pressed={isSelected}
                  disabled={roomLoading}
                >
                  {room.name}
                </button>
            );
          })}
        </div>
      ) : showEmptyState ? (
        <p className={styles.roomEmptyState} role="status">
          Roomはまだ登録されていません。
          {showManagement ? "この画面から作成できます。" : <> <a href={settingsHref}>Room設定</a>から作成できます。</>}
        </p>
      ) : !roomsLoaded && !roomMessage ? (
        <p className={styles.info} role="status">Room一覧を読み込んでいます...</p>
      ) : null}

      <div className={styles.roomStatusGrid}>
        <div className={styles.statusLabel}>選択中</div>
        <div className={styles.statusValue}>
          {selectedRoom ? `${selectedRoom.name} (${selectedRoom.id})` : "-"}
        </div>

        <div className={styles.statusLabel}>Device数</div>
        <div className={styles.statusValue}>{deviceCount ?? "-"}</div>
      </div>

      {showManagement && <form className={styles.roomCreateForm} onSubmit={onCreateRoom}>
        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="room-create-id">
              Room ID
            </label>
            <input
              id="room-create-id"
              className={styles.input}
              value={roomCreateId}
              onChange={(event) => onRoomCreateIdChange(event.target.value)}
              placeholder="living"
              disabled={roomLoading}
            />
            <p className={styles.helperText}>
              小文字のsnake_caseで入力します。
            </p>
          </div>

          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="room-create-name">
              表示名
            </label>
            <input
              id="room-create-name"
              className={styles.input}
              value={roomCreateName}
              onChange={(event) => onRoomCreateNameChange(event.target.value)}
              placeholder="リビング"
              disabled={roomLoading}
            />
          </div>
        </div>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="room-create-sort-order">
              表示優先度
            </label>
            <input
              id="room-create-sort-order"
              className={styles.input}
              inputMode="numeric"
              value={roomCreateSortOrder}
              onChange={(event) =>
                onRoomCreateSortOrderChange(event.target.value)
              }
              placeholder="10"
              disabled={roomLoading}
            />
          </div>

          <div className={styles.roomFormActions}>
            <button
              type="submit"
              className={styles.primaryButton}
              disabled={roomLoading}
            >
              Room作成
            </button>
          </div>
        </div>
      </form>}

      {showManagement && <form className={styles.roomEditForm} onSubmit={onUpdateRoom}>
        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label}>Room ID</label>
            <div className={styles.readOnlyValue}>
              {selectedRoom ? selectedRoom.id : "-"}
            </div>
            <p className={styles.helperText}>Room IDは変更できません。</p>
          </div>

          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="room-edit-name">
              表示名
            </label>
            <input
              id="room-edit-name"
              className={styles.input}
              value={roomEditName}
              onChange={(event) => onRoomEditNameChange(event.target.value)}
              disabled={roomLoading || !selectedRoom}
            />
          </div>
        </div>

        <div className={styles.formRow}>
          <div className={styles.formGroup}>
            <label className={styles.label} htmlFor="room-edit-sort-order">
              表示優先度
            </label>
            <input
              id="room-edit-sort-order"
              className={styles.input}
              inputMode="numeric"
              value={roomEditSortOrder}
              onChange={(event) =>
                onRoomEditSortOrderChange(event.target.value)
              }
              disabled={roomLoading || !selectedRoom}
            />
          </div>

          <div className={styles.roomFormActions}>
            <button
              type="submit"
              className={styles.primaryButton}
              disabled={roomLoading || !selectedRoom}
            >
              Room更新
            </button>
          </div>
        </div>
      </form>}

      {showManagement && <div className={styles.roomDeletePanel}>
        <div>
          <p className={styles.warningText}>
            削除対象:{" "}
            {selectedRoom ? `${selectedRoom.name} (${selectedRoom.id})` : "-"}
          </p>
          <p className={styles.helperText}>
            Room削除はroom.jsonのみを削除します。Deviceが残っているRoomは削除できません。
          </p>
        </div>

        <button
          type="button"
          className={styles.dangerButton}
          onClick={onDeleteRoom}
          disabled={roomLoading || !selectedRoom}
        >
          Room削除
        </button>
      </div>}

      {roomMessage && (!showEmptyState || roomMessage.startsWith("Room metadataをTrashへ移動しました:")) && <p className={styles.info} role="status">{roomMessage}</p>}
      {roomError && <p className={styles.error} role="alert">{roomError}</p>}
    </section>
  );
}
