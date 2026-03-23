import { useCallback, useEffect, useRef, useState } from "react";
import { GetSyncStatus, ToggleMining } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export function useWalletChrome() {
  const [syncing, setSyncing] = useState(true);
  const [syncDismissed, setSyncDismissed] = useState(false);
  const wasSynced = useRef(false);
  const [showDebug, setShowDebug] = useState(false);

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

  const onHideSyncOverlay = useCallback(() => setSyncDismissed(true), []);

  useEffect(() => {
    return EventsOn("menu:debug-window", () => setShowDebug(true));
  }, []);

  useEffect(() => {
    return EventsOn("menu:toggle-mining", () => {
      ToggleMining().catch(() => {});
    });
  }, []);

  const onCloseDebug = useCallback(() => setShowDebug(false), []);

  return {
    showSyncOverlay: syncing && !syncDismissed,
    onHideSyncOverlay,
    showDebug,
    onCloseDebug,
  };
}
