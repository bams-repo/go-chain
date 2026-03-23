import { useEffect, useState } from "react";
import { GetMiningStatus, GetBlockchainInfo, GetPeerCount, GetSyncStatus } from "../../wailsjs/go/main/App";

interface MiningStatus {
  mining: boolean;
  hashrate: number;
  hashrateReady: boolean;
}

function formatHashrate(h: number): string {
  if (h >= 1e12) return (h / 1e12).toFixed(2) + " TH/s";
  if (h >= 1e9) return (h / 1e9).toFixed(2) + " GH/s";
  if (h >= 1e6) return (h / 1e6).toFixed(2) + " MH/s";
  if (h >= 1e3) return (h / 1e3).toFixed(2) + " KH/s";
  return h.toFixed(0) + " H/s";
}

function SignalBars({ peers }: { peers: number }) {
  const bars = peers >= 8 ? 4 : peers >= 4 ? 3 : peers >= 1 ? 2 : 0;
  const gold = "var(--color-btc-gold)";
  const dim = "rgba(255,255,255,0.15)";
  return (
    <svg width="14" height="12" viewBox="0 0 24 20" fill="none" strokeWidth={3} strokeLinecap="round">
      <line x1="4" y1="18" x2="4" y2="15" stroke={bars >= 1 ? gold : dim} />
      <line x1="9" y1="18" x2="9" y2="12" stroke={bars >= 2 ? gold : dim} />
      <line x1="14" y1="18" x2="14" y2="8" stroke={bars >= 3 ? gold : dim} />
      <line x1="19" y1="18" x2="19" y2="4" stroke={bars >= 4 ? gold : dim} />
    </svg>
  );
}

export function StatusBar() {
  const [mining, setMining] = useState<MiningStatus>({ mining: false, hashrate: 0, hashrateReady: false });
  const [height, setHeight] = useState(0);
  const [peers, setPeers] = useState(0);
  const [syncState, setSyncState] = useState("INITIAL");
  const [syncProgress, setSyncProgress] = useState(0);

  useEffect(() => {
    const poll = () => {
      GetMiningStatus()
        .then((s) => setMining(s as unknown as MiningStatus))
        .catch(() => {});
      GetBlockchainInfo()
        .then((info) => {
          setHeight(info.height as number);
        })
        .catch(() => {});
      GetPeerCount().then(setPeers).catch(() => {});
      GetSyncStatus()
        .then((s) => {
          setSyncState(s.syncState as string);
          if (typeof s.progress === "number") setSyncProgress(s.progress as number);
        })
        .catch(() => {});
    };
    poll();
    const id = setInterval(poll, 1500);
    return () => clearInterval(id);
  }, []);

  const synced = syncState === "SYNCED";
  let syncLabel: string;
  if (synced) {
    syncLabel = "Synced";
  } else if (syncState === "HEADER_SYNC") {
    syncLabel = "Syncing Headers...";
  } else if (syncState === "BLOCK_SYNC") {
    syncLabel = `Syncing Blocks (${(syncProgress * 100).toFixed(1)}%)`;
  } else {
    syncLabel = peers > 0 ? "Synced" : "Connecting...";
  }
  const isSynced = synced || (syncState === "INITIAL" && peers > 0);

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        padding: "4px 16px",
        borderTop: "1px solid var(--color-btc-border)",
        background: "var(--color-btc-surface)",
        fontSize: 11,
        color: "var(--color-btc-text-dim)",
        flexShrink: 0,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 5 }}>
          <SignalBars peers={peers} />
          <span>
            {peers} connection{peers !== 1 ? "s" : ""}
          </span>
        </div>

        <span style={{ fontFamily: "monospace" }}>
          Block: {height.toLocaleString()}
        </span>

        <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
          {isSynced ? (
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-green)" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <polyline points="20 6 9 17 4 12" />
            </svg>
          ) : (
            <div
              style={{
                width: 8,
                height: 8,
                borderRadius: "50%",
                border: "2px solid var(--color-btc-gold)",
                borderTopColor: "transparent",
                animation: "spin 1s linear infinite",
              }}
            />
          )}
          <span style={{ color: isSynced ? "var(--color-btc-green)" : "var(--color-btc-gold)" }}>
            {syncLabel}
          </span>
        </div>
      </div>

      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        {mining.mining ? (
          <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <div
              style={{
                width: 7,
                height: 7,
                borderRadius: "50%",
                background: "var(--color-btc-gold)",
                boxShadow: "0 0 6px var(--color-btc-gold)",
                animation: "pulse-glow 1.5s ease-in-out infinite",
              }}
            />
            {mining.hashrateReady ? (
              <>
                <span style={{ color: "var(--color-btc-gold)", fontWeight: 600 }}>
                  Mining Active
                </span>
                <span style={{ fontFamily: "monospace", color: "var(--color-btc-text-muted)" }}>
                  {formatHashrate(mining.hashrate)}
                </span>
              </>
            ) : (
              <span style={{ color: "var(--color-btc-gold)" }}>
                Mining Initializing...
              </span>
            )}
          </div>
        ) : (
          <span style={{ color: "var(--color-btc-text-dim)" }}>Mining Inactive</span>
        )}
      </div>
    </div>
  );
}
