import { TooltipProvider } from "@/components/ui/tooltip";
import MainLayout from "@/components/layout/MainLayout";
import { StatusBar } from "@/components/StatusBar";
import { SyncOverlay } from "@/components/SyncOverlay";
import { DebugWindow } from "@/components/DebugWindow";
import { useWalletChrome } from "@/hooks/useWalletChrome";

export default function WalletShell() {
  const { showSyncOverlay, onHideSyncOverlay, showDebug, onCloseDebug } = useWalletChrome();

  return (
    <TooltipProvider>
      <div className="relative flex h-full min-h-0 flex-col">
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <MainLayout />
        </div>
        <StatusBar />
        {showSyncOverlay && <SyncOverlay onHide={onHideSyncOverlay} />}
        {showDebug && <DebugWindow onClose={onCloseDebug} />}
      </div>
    </TooltipProvider>
  );
}
