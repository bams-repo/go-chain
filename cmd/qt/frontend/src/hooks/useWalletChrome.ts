import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GetSyncStatus } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { SecurityHighlight } from "@/components/SecurityWindow";

export function useWalletChrome() {
  const [syncing, setSyncing] = useState(true);
  const [syncDismissed, setSyncDismissed] = useState(false);
  const wasSynced = useRef(false);
  const [showDebug, setShowDebug] = useState(false);
  const [showSecurity, setShowSecurity] = useState(false);
  const [securityHighlight, setSecurityHighlight] = useState<SecurityHighlight>(null);

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
    return EventsOn("menu:wallet-security", () => {
      setSecurityHighlight(null);
      setShowSecurity(true);
    });
  }, []);

  useEffect(() => {
    return EventsOn("menu:encrypt-wallet", () => {
      setSecurityHighlight("encrypt");
      setShowSecurity(true);
    });
  }, []);

  useEffect(() => {
    return EventsOn("menu:change-passphrase", () => {
      setSecurityHighlight("passphrase");
      setShowSecurity(true);
    });
  }, []);

  const onCloseDebug = useCallback(() => setShowDebug(false), []);

  const onSecurityOpenChange = useCallback((open: boolean) => {
    setShowSecurity(open);
    if (!open) {
      setSecurityHighlight(null);
    }
  }, []);

  const handleSyncOverlay = useCallback(() => {
    if (syncing) setSyncDismissed(false);
  }, [syncing]);

  return useMemo(
    () => ({
      showSyncOverlay: syncing && !syncDismissed,
      onHideSyncOverlay,
      showDebug,
      onCloseDebug,
      handleSyncOverlay,
      showSecurity,
      onSecurityOpenChange,
      securityHighlight,
    }),
    [
      syncing,
      syncDismissed,
      onHideSyncOverlay,
      showDebug,
      onCloseDebug,
      handleSyncOverlay,
      showSecurity,
      onSecurityOpenChange,
      securityHighlight,
    ],
  );
}
