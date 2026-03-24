import { useEffect, useRef, useState } from "react";
import { useCoinInfo } from "../hooks/useCoinInfo";
import { GetSyncStatus } from "../../wailsjs/go/main/App";

interface SyncStatus {
  syncState: string;
  headerHeight: number;
  blockHeight: number;
  bestPeerHeight: number;
  peers: number;
  progress: number;
  lastBlockTime: number;
}

function formatBlockTime(unix: number): string {
  if (!unix || unix <= 0) return "Unknown";
  const d = new Date(unix * 1000);
  return d.toLocaleDateString(undefined, {
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatEta(secondsLeft: number): string {
  if (!isFinite(secondsLeft) || secondsLeft <= 0) return "Unknown...";
  if (secondsLeft < 60) return "< 1 minute";
  if (secondsLeft < 3600) {
    const m = Math.ceil(secondsLeft / 60);
    return `${m} minute${m !== 1 ? "s" : ""}`;
  }
  const h = Math.floor(secondsLeft / 3600);
  const m = Math.ceil((secondsLeft % 3600) / 60);
  return `${h}h ${m}m`;
}

export function SyncOverlay({ onHide }: { onHide: () => void }) {
  const coinInfo = useCoinInfo();
  const [status, setStatus] = useState<SyncStatus | null>(null);

  const progressHistory = useRef<{ time: number; progress: number }[]>([]);
  const [ratePerHour, setRatePerHour] = useState<number | null>(null);
  const [eta, setEta] = useState<string>("Unknown...");

  useEffect(() => {
    const poll = () => {
      GetSyncStatus()
        .then((s) => {
          const st = s as unknown as SyncStatus;
          setStatus(st);

          const now = Date.now();
          const hist = progressHistory.current;
          hist.push({ time: now, progress: st.progress });

          // Keep only the last 60 seconds of samples for rate calculation.
          const cutoff = now - 60_000;
          while (hist.length > 1 && hist[0].time < cutoff) {
            hist.shift();
          }

          if (hist.length >= 2) {
            const oldest = hist[0];
            const elapsed = (now - oldest.time) / 1000;
            const delta = st.progress - oldest.progress;
            if (elapsed > 5 && delta > 0) {
              const perSecond = delta / elapsed;
              setRatePerHour(perSecond * 3600);
              const remaining = 1.0 - st.progress;
              setEta(formatEta(remaining / perSecond));
            } else if (delta <= 0) {
              setRatePerHour(null);
              setEta("calculating...");
            }
          } else {
            setRatePerHour(null);
            setEta("calculating...");
          }
        })
        .catch(() => {});
    };
    poll();
    const id = setInterval(poll, 1500);
    return () => clearInterval(id);
  }, []);

  const blocksLeft = status
    ? status.bestPeerHeight > status.blockHeight
      ? status.bestPeerHeight - status.blockHeight
      : 0
    : 0;

  const isHeaderSync = status?.syncState === "HEADER_SYNC";
  const progressPct = status ? (status.progress * 100).toFixed(2) : "0.00";

  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        zIndex: 50,
        background: "var(--color-btc-deep)",
        display: "flex",
        flexDirection: "column",
      }}
    >
      {/* Warning banner */}
      <div
        style={{
          display: "flex",
          alignItems: "flex-start",
          gap: 12,
          padding: "16px 24px",
          background: "var(--color-btc-surface)",
          borderBottom: "1px solid var(--color-btc-border)",
        }}
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--color-btc-gold)"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
          style={{ width: 24, height: 24, flexShrink: 0, marginTop: 2 }}
        >
          <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
        <div style={{ fontSize: 13, lineHeight: 1.5, color: "var(--color-btc-text)" }}>
          <p>
            Recent transactions may not yet be visible, and therefore your wallet's balance might be
            incorrect. This information will be correct once your wallet has finished synchronizing
            with the {coinInfo.name} network, as detailed below.
          </p>
          <p style={{ fontWeight: 600, marginTop: 4 }}>
            Attempting to spend {coinInfo.nameLower} that are affected by not-yet-displayed
            transactions will not be accepted by the network.
          </p>
        </div>
      </div>

      {/* Sync detail table */}
      <div
        style={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          padding: 32,
        }}
      >
        <div style={{ width: "100%", maxWidth: 520 }}>
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <tbody>
              <Row
                label="Number of blocks left"
                value={
                  status == null
                    ? "Connecting..."
                    : isHeaderSync
                      ? `Unknown. Syncing Headers (${status.headerHeight.toLocaleString()})...`
                      : blocksLeft > 0
                        ? blocksLeft.toLocaleString()
                        : "0"
                }
              />
              <Row
                label="Last block time"
                value={status ? formatBlockTime(status.lastBlockTime) : "Unknown"}
              />
              <Row
                label="Progress"
                value={
                  <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                    <span>{progressPct}%</span>
                    <div
                      style={{
                        flex: 1,
                        height: 14,
                        borderRadius: 3,
                        background: "var(--color-btc-surface)",
                        border: "1px solid var(--color-btc-border)",
                        overflow: "hidden",
                      }}
                    >
                      <div
                        style={{
                          height: "100%",
                          width: `${Math.min(100, status?.progress ? status.progress * 100 : 0)}%`,
                          background: "var(--color-btc-gold)",
                          borderRadius: 2,
                          transition: "width 0.6s ease",
                        }}
                      />
                    </div>
                  </div>
                }
              />
              <Row
                label="Progress increase per hour"
                value={
                  ratePerHour != null ? `${(ratePerHour * 100).toFixed(2)}%` : "calculating..."
                }
              />
              <Row label="Estimated time left until synced" value={eta} />
            </tbody>
          </table>
        </div>
      </div>

      {/* Footer: version + hide */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          padding: "12px 24px",
          borderTop: "1px solid var(--color-btc-border)",
          background: "var(--color-btc-surface)",
        }}
      >
        <span style={{ fontSize: 12, color: "var(--color-btc-text-dim)", fontFamily: "monospace" }}>
          {coinInfo.version}
        </span>
        <button
          onClick={onHide}
          style={{
            padding: "6px 20px",
            fontSize: 13,
            fontWeight: 500,
            borderRadius: 4,
            border: "1px solid var(--color-btc-border)",
            background: "var(--color-btc-card)",
            color: "var(--color-btc-text)",
            cursor: "pointer",
          }}
        >
          Hide
        </button>
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <tr>
      <td
        style={{
          padding: "10px 16px 10px 0",
          fontSize: 13,
          fontWeight: 600,
          color: "var(--color-btc-text)",
          whiteSpace: "nowrap",
          verticalAlign: "top",
        }}
      >
        {label}
      </td>
      <td
        style={{
          padding: "10px 0",
          fontSize: 13,
          color: "var(--color-btc-text-muted)",
          width: "60%",
        }}
      >
        {value}
      </td>
    </tr>
  );
}
