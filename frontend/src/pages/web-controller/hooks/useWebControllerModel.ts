import { useIRController } from "./useIRController";
import { useRoomDeviceController } from "./useRoomDeviceController";

export function useWebControllerModel(
  routeRoomId?: string,
  onRoomSelect?: (roomId: string) => void,
  routeDeviceId?: string,
) {
  const roomDevice = useRoomDeviceController(routeRoomId, onRoomSelect, routeDeviceId);
  const ir = useIRController(roomDevice.selection);

  return { roomDevice, ir };
}

export type WebControllerPageModel = ReturnType<typeof useWebControllerModel>;
