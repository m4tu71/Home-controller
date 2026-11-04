import { useWebControllerModel } from "./hooks/useWebControllerModel";
import WebControllerView from "./WebControllerView";

type WebControllerProps = {
  routeRoomId?: string;
  routeDeviceId?: string;
  onRoomSelect?: (roomId: string) => void;
};

export function WebController({ routeRoomId, routeDeviceId, onRoomSelect }: WebControllerProps) {
  const model = useWebControllerModel(routeRoomId, onRoomSelect, routeDeviceId);
  return <WebControllerView model={model} />;
}

export default WebController;
