import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { TooltipProvider } from "@/components/ui/tooltip";
import MainLayout from "@/components/layout/MainLayout";
import { StatusBar } from "@/components/StatusBar";
import { SyncOverlay } from "@/components/SyncOverlay";
import { DebugWindow } from "@/components/DebugWindow";
import { SecurityWindow } from "@/components/SecurityWindow";
import { useWalletChrome } from "@/hooks/useWalletChrome";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export default function WalletShell() {
  const navigate = useNavigate();
  useEffect(() => {
    return EventsOn("menu:block-explorer", () => {
      navigate("/explorer");
    });
  }, [navigate]);

  const {
    showSyncOverlay,
    onHideSyncOverlay,
    showDebug,
    onCloseDebug,
    handleSyncOverlay,
    showSecurity,
    onSecurityOpenChange,
    securityHighlight,
  } = useWalletChrome();

  return (
    <TooltipProvider>
      <div className="relative flex h-full min-h-0 flex-col">
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <MainLayout />
        </div>
        <StatusBar handleSyncOverlay={handleSyncOverlay} />
        {showSyncOverlay && <SyncOverlay onHide={onHideSyncOverlay} />}
        {showDebug && <DebugWindow onClose={onCloseDebug} />}
        <SecurityWindow open={showSecurity} onOpenChange={onSecurityOpenChange} highlight={securityHighlight} />
      </div>
    </TooltipProvider>
  );
}
