import {
  BrowserRouter,
  Navigate,
  Route,
  Routes,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";

import Header from "./components/header/Header";
import Footer from "./components/footer/Footer";

import DesignSystem from "./pages/design-system/DesignSystem";
import WebController from "./pages/web-controller/WebController";
import RoomDeviceSettingsPage from "./pages/web-controller/RoomDeviceSettingsPage";
import MaintenancePage from "./pages/web-controller/MaintenancePage";
import TrashPage from "./pages/web-controller/TrashPage";

function RoomControllerRoute() {
  const { roomId } = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const routeRoomId = roomId ?? searchParams.get("room") ?? undefined;
  const routeDeviceId = searchParams.get("device") ?? undefined;

  return (
    <WebController
      routeRoomId={routeRoomId}
      routeDeviceId={routeDeviceId}
      onRoomSelect={(nextRoomId) =>
        navigate(`/rooms/${encodeURIComponent(nextRoomId)}`)
      }
    />
  );
}

function App() {
  return (
    <BrowserRouter>
      <Header />

      <Routes>
        <Route path="/" element={<RoomControllerRoute />} />
        <Route path="/rooms/:roomId" element={<RoomControllerRoute />} />
        <Route path="/settings" element={<RoomDeviceSettingsPage />} />
        <Route path="/maintenance" element={<MaintenancePage />} />
        <Route path="/trash" element={<TrashPage />} />
        <Route path="/raspy-time" element={<Navigate to="/" replace />} />
        <Route path="/web-controller" element={<Navigate to="/" replace />} />
        <Route path="/design-system" element={<DesignSystem />} />
      </Routes>

      <Footer />
    </BrowserRouter>
  );
}

export default App;
