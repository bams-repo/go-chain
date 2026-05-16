import { Route, Routes } from "react-router-dom";
import ProtectedRoute from "./ProtectedRoute";
import WalletShell from "./WalletShell";
import { Overview } from "@/pages/overview";
import { Send } from "@/pages/send";
import { Receive } from "@/pages/receive";
import { Social } from "@/pages/social";
import { Coming } from "@/pages/Coming";
import { NodeNetwork } from "@/pages/node-network";
import { NodeMap } from "@/pages/node-map";
import { Mining } from "@/pages/mining";
import { Transactions } from "@/pages/transactions";
import { ExplorerHome } from "@/pages/explorer/Home";
import { ExplorerBlockPage } from "@/pages/explorer/BlockPage";
import { ExplorerTxPage } from "@/pages/explorer/TxPage";
import { ExplorerAddressPage } from "@/pages/explorer/AddressPage";

export default function AppRoutes() {
  return (
    <Routes>
      <Route element={<ProtectedRoute />}>
        <Route element={<WalletShell />}>
          <Route path="/" element={<Overview />} />
          <Route path="/send" element={<Send />} />
          <Route path="/receive" element={<Receive />} />
          <Route path="/transactions" element={<Transactions />} />
          <Route path="/social" element={<Social />} />
          <Route path="/node-map" element={<NodeMap />} />
          <Route path="/node-network" element={<NodeNetwork />} />
          <Route path="/mining" element={<Mining />} />
          <Route path="/explorer" element={<ExplorerHome />} />
          <Route path="/explorer/block/:id" element={<ExplorerBlockPage />} />
          <Route path="/explorer/tx/:txid" element={<ExplorerTxPage />} />
          <Route path="/explorer/address/:addr" element={<ExplorerAddressPage />} />
          <Route path="*" element={<Coming />} />
        </Route>
      </Route>
    </Routes>
  );
}
