import { useCallback, useEffect, useRef, useState } from "react";
import { BrowserRouter } from "react-router-dom";
import { CoinInfoContext } from "./hooks/useCoinInfo";
import { TooltipProvider } from "@/components/ui/tooltip";
import { CoinInfo as GetCoinInfo, GetSyncStatus, ToggleMining } from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";
import { CoinInfo } from "./lib/types";
import AppRoutes from "./routes";
import MainLayout from "./components/layout/MainLayout";
import { StatusBar } from "./components/StatusBar";
import { SyncOverlay } from "./components/SyncOverlay";
import { DebugWindow } from "./components/DebugWindow";

function App() {
  const [coinInfo, setCoinInfo] = useState<CoinInfo | null>(null);

  const [syncing, setSyncing] = useState(true);
  const [syncDismissed, setSyncDismissed] = useState(false);
  const wasSynced = useRef(false);
  const [showDebug, setShowDebug] = useState(false);

  useEffect(() => {
    GetCoinInfo().then((info) => setCoinInfo(info as unknown as CoinInfo));
  }, []);

  useEffect(() => {
    const poll = () => {
      GetSyncStatus()
        .then((s) => {
          const state = s.syncState as string;
          const isSyncing = state !== "SYNCED";
          setSyncing(isSyncing);

          if (!isSyncing) {
            wasSynced.current = true;
          } else if (wasSynced.current) {
            wasSynced.current = false;
            setSyncDismissed(false);
          }
        })
        .catch(() => {});
    };
    poll();
    const id = setInterval(poll, 1500);
    return () => clearInterval(id);
  }, []);

  const handleHide = useCallback(() => setSyncDismissed(true), []);

  useEffect(() => {
    const off = EventsOn("menu:debug-window", () => setShowDebug(true));
    return off;
  }, []);

  useEffect(() => {
    const off = EventsOn("menu:toggle-mining", () => {
      ToggleMining().catch(() => {});
    });
    return off;
  }, []);

  if (!coinInfo) {
    return (
      <div className="flex h-full items-center justify-center" style={{ background: "var(--color-btc-deep)" }}>
        <div className="flex flex-col items-center gap-4">
          <div
            className="h-10 w-10 animate-spin rounded-full border-2 border-transparent"
            style={{ borderTopColor: "var(--color-btc-gold)" }}
          />
          <span style={{ color: "var(--color-btc-text-muted)" }} className="text-sm">
            Starting node...
          </span>
        </div>
      </div>
    );
  }

  const showSyncOverlay = syncing && !syncDismissed;

  return (
    <BrowserRouter>
      <CoinInfoContext.Provider value={coinInfo}>
        <TooltipProvider>
          <div className="relative flex h-full min-h-0 flex-col">
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
              <MainLayout>
                <AppRoutes />
              </MainLayout>
            </div>
            <StatusBar />
            {showSyncOverlay && <SyncOverlay onHide={handleHide} />}
            {showDebug && <DebugWindow onClose={() => setShowDebug(false)} />}
          </div>
        </TooltipProvider>
      </CoinInfoContext.Provider>
    </BrowserRouter>
  );
}

export default App;
