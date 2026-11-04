import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  createDevice,
  createRoom,
  deleteDevice,
  deleteRoom,
  fetchDevices,
  fetchRooms,
  updateDevice,
  updateRoom,
  type Device,
  type Room,
} from "../../../api/controller";

export function useRoomDeviceController(
  routeRoomId?: string,
  onRoomSelect?: (roomId: string) => void,
  routeDeviceId?: string,
) {
  const [rooms, setRooms] = useState<Room[]>([]);

  const [roomsLoaded, setRoomsLoaded] = useState(false);

  const [storedSelectedRoomId, setSelectedRoomId] = useState("");

  const selectedRoomId = routeRoomId
    ? rooms.some((room) => room.id === routeRoomId)
      ? routeRoomId
      : ""
    : storedSelectedRoomId;

  const [roomMessage, setRoomMessage] = useState("");

  const [roomError, setRoomError] = useState("");

  const [roomLoading, setRoomLoading] = useState(true);

  const [roomCreateId, setRoomCreateId] = useState("");

  const [roomCreateName, setRoomCreateName] = useState("");

  const [roomCreateSortOrder, setRoomCreateSortOrder] = useState("");

  const [roomEditName, setRoomEditName] = useState("");

  const [roomEditSortOrder, setRoomEditSortOrder] = useState("");

  const [devices, setDevices] = useState<Device[]>([]);

  const [devicesRoomId, setDevicesRoomId] = useState("");

  const deviceLoadRequestId = useRef(0);

  const [devicesLoaded, setDevicesLoaded] = useState(false);

  const [selectedDeviceId, setSelectedDeviceId] = useState("");

  const [deviceMessage, setDeviceMessage] = useState("");

  const [deviceError, setDeviceError] = useState("");

  const [deviceLoading, setDeviceLoading] = useState(false);

  const [deviceCreateId, setDeviceCreateId] = useState("");

  const [deviceCreateName, setDeviceCreateName] = useState("");

  const [deviceCreateType, setDeviceCreateType] = useState("light");

  const [deviceCreatePrimaryAdapter, setDeviceCreatePrimaryAdapter] =
    useState("ir");

  const [deviceCreateCapabilities, setDeviceCreateCapabilities] = useState("");

  const [deviceEditName, setDeviceEditName] = useState("");

  const [deviceEditType, setDeviceEditType] = useState("light");

  const [deviceEditPrimaryAdapter, setDeviceEditPrimaryAdapter] =
    useState("ir");

  const [deviceEditCapabilities, setDeviceEditCapabilities] = useState("");

  const syncRoomEditForm = (room?: Room) => {
    if (!room) {
      setRoomEditName("");
      setRoomEditSortOrder("");
      return;
    }

    setRoomEditName(room.name);
    setRoomEditSortOrder(
      room.sort_order === undefined ? "" : String(room.sort_order)
    );
  };

  const syncDeviceEditForm = (device?: Device) => {
    if (!device) {
      setDeviceEditName("");
      setDeviceEditType("light");
      setDeviceEditPrimaryAdapter("ir");
      setDeviceEditCapabilities("");
      return;
    }

    setDeviceEditName(device.name);
    setDeviceEditType(device.device_type);
    setDeviceEditPrimaryAdapter(device.primary_adapter);
    setDeviceEditCapabilities(device.capabilities?.join(", ") ?? "");
  };

  const handleSelectRoom = (room: Room) => {
    setSelectedRoomId(room.id);
    deviceLoadRequestId.current += 1;
    setDevices([]);
    setDevicesRoomId("");
    setSelectedDeviceId("");
    setDevicesLoaded(false);
    syncDeviceEditForm();
    setDeviceError("");
    setDeviceMessage("Device一覧を取得中...");
    setDeviceLoading(true);
    syncRoomEditForm(room);
    setRoomMessage(`選択中Room: ${room.name}`);
    setRoomError("");
    if (routeRoomId !== room.id) {
      onRoomSelect?.(room.id);
    }
  };

  const loadRooms = async (preferredRoomId = "") => {
    try {
      setRoomLoading(true);
      setRoomError("");
      setRoomMessage("Room一覧を取得中...");

      const nextRooms = await fetchRooms();
      setRooms(nextRooms);
      setRoomsLoaded(true);

      if (nextRooms.length === 0) {
        setSelectedRoomId("");
        deviceLoadRequestId.current += 1;
        setDevices([]);
        setDevicesRoomId("");
        setSelectedDeviceId("");
        setDevicesLoaded(false);
        syncDeviceEditForm();
        syncRoomEditForm();
        setRoomMessage("Roomはまだ登録されていません");
        return;
      }

      const routeRoom = routeRoomId
        ? nextRooms.find((room) => room.id === routeRoomId)
        : undefined;
      const preferredRoomStillExists = nextRooms.some(
        (room) => room.id === preferredRoomId
      );
      if (routeRoomId && !routeRoom && !preferredRoomStillExists) {
        setSelectedRoomId("");
        deviceLoadRequestId.current += 1;
        setDevices([]);
        setDevicesRoomId("");
        setSelectedDeviceId("");
        setDevicesLoaded(false);
        syncDeviceEditForm();
        syncRoomEditForm();
        setRoomError(`Roomが見つかりません: ${routeRoomId}`);
        setRoomMessage("");
        return;
      }

      const selectedRoomStillExists = nextRooms.some(
        (room) => room.id === selectedRoomId
      );
      const nextSelectedRoomId = preferredRoomStillExists
        ? preferredRoomId
        : routeRoom
          ? routeRoom.id
          : selectedRoomStillExists
            ? selectedRoomId
            : nextRooms[0].id;

      setSelectedRoomId(nextSelectedRoomId);
      syncRoomEditForm(
        nextRooms.find((room) => room.id === nextSelectedRoomId)
      );
      setRoomMessage(`Roomを${nextRooms.length}件取得しました`);
      if (preferredRoomId && preferredRoomId !== routeRoomId) {
        onRoomSelect?.(nextSelectedRoomId);
      }
    } catch (error) {
      setRoomsLoaded(true);
      setRoomError(
        error instanceof Error ? error.message : "Room一覧の取得に失敗しました"
      );
      setRoomMessage("");
    } finally {
      setRoomLoading(false);
    }
  };

  const handleCreateRoom = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const roomId = roomCreateId.trim();
    const roomName = roomCreateName.trim();

    if (!roomId || !roomName) {
      setRoomError("Room IDと表示名を入力してください");
      setRoomMessage("");
      return;
    }

    const room: Room = {
      id: roomId,
      name: roomName,
    };

    if (roomCreateSortOrder.trim() !== "") {
      const sortOrder = Number(roomCreateSortOrder);

      if (!Number.isFinite(sortOrder)) {
        setRoomError("表示優先度には数値を入力してください");
        setRoomMessage("");
        return;
      }

      room.sort_order = sortOrder;
    }

    try {
      setRoomLoading(true);
      setRoomError("");
      setRoomMessage(`Room作成中: ${roomName}`);

      const createdRoom = await createRoom(room);

      setRoomCreateId("");
      setRoomCreateName("");
      setRoomCreateSortOrder("");
      await loadRooms(createdRoom.id);
      setRoomMessage(`Roomを作成しました: ${createdRoom.name}`);
    } catch (error) {
      setRoomError(
        error instanceof Error ? error.message : "Room作成に失敗しました"
      );
      setRoomMessage("");
    } finally {
      setRoomLoading(false);
    }
  };

  const handleUpdateRoom = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const selectedRoom = rooms.find((room) => room.id === selectedRoomId);
    if (!selectedRoom) {
      setRoomError("編集するRoomを選択してください");
      setRoomMessage("");
      return;
    }

    const roomName = roomEditName.trim();
    if (!roomName) {
      setRoomError("Roomの表示名を入力してください");
      setRoomMessage("");
      return;
    }

    const room: Room = {
      id: selectedRoom.id,
      name: roomName,
    };

    if (roomEditSortOrder.trim() !== "") {
      const sortOrder = Number(roomEditSortOrder);

      if (!Number.isFinite(sortOrder)) {
        setRoomError("表示優先度には数値を入力してください");
        setRoomMessage("");
        return;
      }

      room.sort_order = sortOrder;
    }

    try {
      setRoomLoading(true);
      setRoomError("");
      setRoomMessage(`Room更新中: ${selectedRoom.name}`);

      const updatedRoom = await updateRoom(selectedRoom.id, room);

      await loadRooms(updatedRoom.id);
      setRoomMessage(`Roomを更新しました: ${updatedRoom.name}`);
    } catch (error) {
      setRoomError(
        error instanceof Error ? error.message : "Room更新に失敗しました"
      );
      setRoomMessage("");
    } finally {
      setRoomLoading(false);
    }
  };

  const handleDeleteRoom = async () => {
    const selectedRoom = rooms.find((room) => room.id === selectedRoomId);
    if (!selectedRoom) {
      setRoomError("削除するRoomを選択してください");
      setRoomMessage("");
      return;
    }

    const confirmed = window.confirm(
      `Room「${selectedRoom.name}」を削除します。よろしいですか？`
    );

    if (!confirmed) {
      setRoomMessage("Room削除をキャンセルしました");
      setRoomError("");
      return;
    }

    try {
      setRoomLoading(true);
      setRoomError("");
      setRoomMessage(`Room削除中: ${selectedRoom.name}`);

      const result = await deleteRoom(selectedRoom.id);

      const nextRoomId = rooms.find((room) => room.id !== selectedRoom.id)?.id ?? "";
      await loadRooms(nextRoomId);
      setRoomMessage(`Room metadataをTrashへ移動しました: ${selectedRoom.name} (Trash ${result.trash_entry_id} / operation ${result.operation_id} / ${result.status})`);
    } catch (error) {
      setRoomError(
        error instanceof Error ? error.message : "Room削除に失敗しました"
      );
      setRoomMessage("");
    } finally {
      setRoomLoading(false);
    }
  };

  const handleSelectDevice = (deviceId: string) => {
    setSelectedDeviceId(deviceId);
    syncDeviceEditForm(devices.find((device) => device.id === deviceId));
    setDeviceMessage(`選択中Device: ${deviceId}`);
    setDeviceError("");
  };

  const handleCreateDevice = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!selectedRoomId) {
      setDeviceError("Deviceを作成するRoomを選択してください");
      setDeviceMessage("");
      return;
    }

    const deviceId = deviceCreateId.trim();
    const deviceName = deviceCreateName.trim();

    if (!deviceId || !deviceName) {
      setDeviceError("Device IDと表示名を入力してください");
      setDeviceMessage("");
      return;
    }

    const capabilities = deviceCreateCapabilities
      .split(",")
      .map((capability) => capability.trim())
      .filter((capability) => capability.length > 0);

    const device: Device = {
      id: deviceId,
      name: deviceName,
      room_id: selectedRoomId,
      device_type: deviceCreateType,
      primary_adapter: deviceCreatePrimaryAdapter,
    };

    if (capabilities.length > 0) {
      device.capabilities = capabilities;
    }

    try {
      setDeviceLoading(true);
      setDevicesLoaded(false);
      setDeviceError("");
      setDeviceMessage(`Device作成中: ${deviceName}`);

      const createdDevice = await createDevice(selectedRoomId, device);

      setDeviceCreateId("");
      setDeviceCreateName("");
      setDeviceCreateType("light");
      setDeviceCreatePrimaryAdapter("ir");
      setDeviceCreateCapabilities("");

      await loadDevices(selectedRoomId, createdDevice.id);
      setDeviceMessage(`Deviceを作成しました: ${createdDevice.name}`);
    } catch (error) {
      setDeviceError(
        error instanceof Error ? error.message : "Device作成に失敗しました"
      );
      setDeviceMessage("");
    } finally {
      setDeviceLoading(false);
    }
  };

  const handleUpdateDevice = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!selectedRoomId) {
      setDeviceError("Deviceを編集するRoomを選択してください");
      setDeviceMessage("");
      return;
    }

    const selectedDevice = devices.find(
      (device) => device.id === selectedDeviceId
    );
    if (!selectedDevice) {
      setDeviceError("編集するDeviceを選択してください");
      setDeviceMessage("");
      return;
    }

    const deviceName = deviceEditName.trim();
    if (!deviceName) {
      setDeviceError("Deviceの表示名を入力してください");
      setDeviceMessage("");
      return;
    }

    const capabilities = deviceEditCapabilities
      .split(",")
      .map((capability) => capability.trim())
      .filter((capability) => capability.length > 0);

    const device: Device = {
      id: selectedDevice.id,
      name: deviceName,
      room_id: selectedDevice.room_id,
      device_type: deviceEditType,
      primary_adapter: deviceEditPrimaryAdapter,
    };

    if (capabilities.length > 0) {
      device.capabilities = capabilities;
    }

    try {
      setDeviceLoading(true);
      setDeviceError("");
      setDeviceMessage(`Device更新中: ${selectedDevice.name}`);

      const updatedDevice = await updateDevice(
        selectedRoomId,
        selectedDevice.id,
        device
      );

      syncDeviceEditForm(updatedDevice);
      await loadDevices(selectedRoomId, updatedDevice.id);
      setDeviceMessage(`Deviceを更新しました: ${updatedDevice.name}`);
    } catch (error) {
      setDeviceError(
        error instanceof Error ? error.message : "Device更新に失敗しました"
      );
      setDeviceMessage("");
    } finally {
      setDeviceLoading(false);
    }
  };

  const handleDeleteDevice = async () => {
    if (!selectedRoomId) {
      setDeviceError("Deviceを削除するRoomを選択してください");
      setDeviceMessage("");
      return;
    }

    const selectedDevice = devices.find(
      (device) => device.id === selectedDeviceId
    );
    if (!selectedDevice) {
      setDeviceError("削除するDeviceを選択してください");
      setDeviceMessage("");
      return;
    }

    const confirmed = window.confirm(
      `Device「${selectedDevice.name}」を削除します。device.jsonのみを削除し、adapters/などの配下dataは削除しません。よろしいですか？`
    );

    if (!confirmed) {
      setDeviceMessage("Device削除をキャンセルしました");
      setDeviceError("");
      return;
    }

    try {
      setDeviceLoading(true);
      setDeviceError("");
      setDeviceMessage(`Device削除中: ${selectedDevice.name}`);

      const result = await deleteDevice(selectedRoomId, selectedDevice.id);

      await loadDevices(selectedRoomId);
      setDeviceMessage(`Device metadataをTrashへ移動しました: ${selectedDevice.name} (Trash ${result.trash_entry_id} / operation ${result.operation_id} / ${result.status})`);
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Device削除に失敗しました";
      setDeviceError(
        `${message}。一覧を更新して、対象DeviceとRoomを確認してください。`
      );
      setDeviceMessage("");
    } finally {
      setDeviceLoading(false);
    }
  };

  const loadDevices = async (roomId: string, preferredDeviceId = "") => {
    const requestId = ++deviceLoadRequestId.current;

    if (!roomId) {
      setDevices([]);
      setDevicesRoomId("");
      setSelectedDeviceId("");
      setDevicesLoaded(false);
      setDeviceLoading(false);
      setDeviceMessage("");
      setDeviceError("");
      return;
    }

    try {
      setDeviceLoading(true);
      setDeviceError("");
      setDeviceMessage("Device一覧を取得中...");

      const nextDevices = await fetchDevices(roomId);
      if (requestId !== deviceLoadRequestId.current) {
        return;
      }

      setDevices(nextDevices);
      setDevicesRoomId(roomId);
      setDevicesLoaded(true);

      if (nextDevices.length === 0) {
        setSelectedDeviceId("");
        syncDeviceEditForm();
        setDeviceMessage("");
        return;
      }

      const preferredDeviceStillExists = nextDevices.some(
        (device) => device.id === preferredDeviceId
      );
      const nextSelectedDeviceId = preferredDeviceStillExists
        ? preferredDeviceId
        : nextDevices[0].id;

      setSelectedDeviceId(nextSelectedDeviceId);
      syncDeviceEditForm(
        nextDevices.find((device) => device.id === nextSelectedDeviceId)
      );
      setDeviceMessage(`Deviceを${nextDevices.length}件取得しました`);
    } catch (error) {
      if (requestId !== deviceLoadRequestId.current) {
        return;
      }

      setDevices([]);
      setDevicesRoomId(roomId);
      setDevicesLoaded(true);
      setSelectedDeviceId("");
      syncDeviceEditForm();
      setDeviceError(
        error instanceof Error
          ? error.message
          : "Device一覧の取得に失敗しました"
      );
      setDeviceMessage("");
    } finally {
      if (requestId === deviceLoadRequestId.current) {
        setDeviceLoading(false);
      }
    }
  };

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadRooms(routeRoomId);
    }, 0);

    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadDevices(selectedRoomId, routeDeviceId);
    }, 0);

    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRoomId, routeDeviceId]);

  const selectedRoom = rooms.find((room) => room.id === selectedRoomId);

  const devicesBelongToSelectedRoom =
    Boolean(selectedRoomId) && devicesRoomId === selectedRoomId;
  const visibleDevices = devicesBelongToSelectedRoom ? devices : [];
  const visibleSelectedDeviceId = devicesBelongToSelectedRoom
    ? selectedDeviceId
    : "";
  const visibleDeviceLoading = Boolean(selectedRoomId) &&
    (!devicesBelongToSelectedRoom || deviceLoading);
  const visibleDeviceError = devicesBelongToSelectedRoom ? deviceError : "";
  const visibleDeviceMessage = devicesBelongToSelectedRoom
    ? deviceMessage
    : selectedRoomId
      ? "Device一覧を取得中..."
      : "";
  const visibleDevicesLoaded = devicesBelongToSelectedRoom && devicesLoaded;
  const selectedDevice = visibleDevices.find(
    (device) => device.id === visibleSelectedDeviceId
  );
  const routeRoomMissing = Boolean(
    routeRoomId && roomsLoaded && !rooms.some((room) => room.id === routeRoomId)
  );
  const visibleRoomError = roomError.startsWith("Roomが見つかりません:")
    ? routeRoomMissing
      ? `Roomが見つかりません: ${routeRoomId}`
      : ""
    : roomError || (routeRoomMissing ? `Roomが見つかりません: ${routeRoomId}` : "");

  const roomSectionProps = {
    rooms,
    roomsLoaded,
    selectedRoom,
    deviceCount: selectedRoom && visibleDevicesLoaded && !visibleDeviceLoading && !visibleDeviceError
      ? visibleDevices.length
      : null,
    selectedRoomId,
    roomLoading,
    roomMessage,
    roomError: visibleRoomError,
    roomCreateId,
    roomCreateName,
    roomCreateSortOrder,
    roomEditName,
    roomEditSortOrder,
    onRefreshRooms: () => loadRooms(),
    onSelectRoom: handleSelectRoom,
    onCreateRoom: handleCreateRoom,
    onUpdateRoom: handleUpdateRoom,
    onDeleteRoom: handleDeleteRoom,
    onRoomCreateIdChange: setRoomCreateId,
    onRoomCreateNameChange: setRoomCreateName,
    onRoomCreateSortOrderChange: setRoomCreateSortOrder,
    onRoomEditNameChange: setRoomEditName,
    onRoomEditSortOrderChange: setRoomEditSortOrder,
  };

  const deviceSectionProps = {
    selectedRoom,
    roomsLoaded,
    hasRooms: rooms.length > 0,
    roomLoading,
    roomError: visibleRoomError,
    devices: visibleDevices,
    devicesLoaded: visibleDevicesLoaded,
    selectedDeviceId: visibleSelectedDeviceId,
    deviceLoading: visibleDeviceLoading,
    deviceMessage: visibleDeviceMessage,
    deviceError: visibleDeviceError,
    deviceCreateId,
    deviceCreateName,
    deviceCreateType,
    deviceCreatePrimaryAdapter,
    deviceCreateCapabilities,
    selectedDevice,
    deviceEditName: devicesBelongToSelectedRoom ? deviceEditName : "",
    deviceEditType: devicesBelongToSelectedRoom ? deviceEditType : "light",
    deviceEditPrimaryAdapter: devicesBelongToSelectedRoom
      ? deviceEditPrimaryAdapter
      : "ir",
    deviceEditCapabilities: devicesBelongToSelectedRoom
      ? deviceEditCapabilities
      : "",
    onRefreshDevices: () => loadDevices(selectedRoomId, visibleSelectedDeviceId),
    onSelectDevice: handleSelectDevice,
    onCreateDevice: handleCreateDevice,
    onUpdateDevice: handleUpdateDevice,
    onDeleteDevice: handleDeleteDevice,
    onDeviceCreateIdChange: setDeviceCreateId,
    onDeviceCreateNameChange: setDeviceCreateName,
    onDeviceCreateTypeChange: setDeviceCreateType,
    onDeviceCreatePrimaryAdapterChange: setDeviceCreatePrimaryAdapter,
    onDeviceCreateCapabilitiesChange: setDeviceCreateCapabilities,
    onDeviceEditNameChange: setDeviceEditName,
    onDeviceEditTypeChange: setDeviceEditType,
    onDeviceEditPrimaryAdapterChange: setDeviceEditPrimaryAdapter,
    onDeviceEditCapabilitiesChange: setDeviceEditCapabilities,
  };

  return {
    selection: { selectedRoomId, selectedDeviceId, selectedRoom, selectedDevice },
    roomSectionProps,
    deviceSectionProps,
  };
}
