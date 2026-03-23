import { Route, Routes } from "react-router-dom";
import ProtectedRoute from "./ProtectedRoute";
import WalletShell from "./WalletShell";
import { Overview } from "@/pages/overview";
import { Social } from "@/pages/social";
import { Coming } from "@/pages/Coming";

export default function AppRoutes() {
  return (
    <Routes>
      <Route element={<ProtectedRoute />}>
        <Route element={<WalletShell />}>
          <Route path="/" element={<Overview />} />
          <Route path="/social" element={<Social />} />
          <Route path="*" element={<Coming />} />
        </Route>
      </Route>
    </Routes>
  );
}
