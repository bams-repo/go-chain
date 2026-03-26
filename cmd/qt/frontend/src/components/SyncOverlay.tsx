import { useEffect, useRef, useState } from "react";
import { useCoinInfo } from "../hooks/useCoinInfo";
import { GetSyncStatus } from "../../wailsjs/go/main/App";

interface SyncStatus {
  syncState: string;
  stageLabel: string;
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

function StageIndicator({ syncState }: { syncState: string }) {
  const stages = [
    { key: "INITIAL", label: "Connect" },
    { key: "HEADER_SYNC", label: "Headers" },
    { key: "BLOCK_SYNC", label: "Blocks" },
    { key: "SYNCED", label: "Synced" },
  ];

  const currentIdx = stages.findIndex((s) => s.key === syncState);

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 0, width: "100%", maxWidth: 400, margin: "0 auto" }}>
      {stages.map((stage, i) => {
        const isActive = i === currentIdx;
        const isDone = i < currentIdx;
        const color = isDone
          ? "var(--color-btc-green)"
          : isActive
            ? "var(--color-btc-gold)"
            : "var(--color-btc-text-dim)";

        return (
          <div key={stage.key} style={{ display: "flex", alignItems: "center", flex: 1 }}>
            <div style={{ display: "flex", flexDirection: "column", alignItems: "center", minWidth: 48 }}>
              <div
                style={{
                  width: 24,
                  height: 24,
                  borderRadius: "50%",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  fontSize: 11,
                  fontWeight: 700,
                  border: `2px solid ${color}`,
                  background: isDone ? color : "transparent",
                  color: isDone ? "var(--color-btc-deep)" : color,
                  transition: "all 0.3s ease",
                }}
              >
                {isDone ? (
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                    <polyline points="20 6 9 17 4 12" />
                  </svg>
                ) : isActive ? (
                  <div
                    style={{
                      width: 8,
                      height: 8,
                      borderRadius: "50%",
                      background: color,
                      animation: "pulse-glow 1.5s ease-in-out infinite",
                    }}
                  />
                ) : (
                  i + 1
                )}
              </div>
              <span
                style={{
                  fontSize: 10,
                  fontWeight: isActive ? 700 : 500,
                  color,
                  marginTop: 4,
                  letterSpacing: "0.02em",
                }}
              >
                {stage.label}
              </span>
            </div>
            {i < stages.length - 1 && (
              <div
                style={{
                  flex: 1,
                  height: 2,
                  background: isDone ? "var(--color-btc-green)" : "var(--color-btc-border)",
                  marginBottom: 18,
                  transition: "background 0.3s ease",
                }}
              />
            )}
          </div>
        );
      })}
    </div>
  );
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

  const progressPct = status ? (status.progress * 100).toFixed(1) : "0.0";

  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        zIndex: 1000,
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

      {/* Sync detail */}
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
          {/* Stage indicator */}
          <div style={{ marginBottom: 28 }}>
            <StageIndicator syncState={status?.syncState ?? "INITIAL"} />
          </div>

          {/* Current stage label */}
          <div
            style={{
              textAlign: "center",
              marginBottom: 20,
              fontSize: 14,
              fontWeight: 600,
              color: "var(--color-btc-gold)",
              minHeight: 20,
            }}
          >
            {status?.stageLabel ?? "Initializing..."}
          </div>

          {/* Progress bar */}
          <div style={{ marginBottom: 24 }}>
            <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 6 }}>
              <span style={{ fontSize: 12, fontWeight: 600, color: "var(--color-btc-text)" }}>
                Overall Progress
              </span>
              <span style={{ fontSize: 12, fontWeight: 700, color: "var(--color-btc-gold)", fontFamily: "monospace" }}>
                {progressPct}%
              </span>
            </div>
            <div
              style={{
                height: 18,
                borderRadius: 4,
                background: "var(--color-btc-surface)",
                border: "1px solid var(--color-btc-border)",
                overflow: "hidden",
                position: "relative",
              }}
            >
              <div
                style={{
                  height: "100%",
                  width: `${Math.min(100, status?.progress ? status.progress * 100 : 0)}%`,
                  background: "linear-gradient(90deg, var(--color-btc-gold), #e8a820)",
                  borderRadius: 3,
                  transition: "width 0.6s ease",
                }}
              />
              {/* 50% marker showing header/block boundary */}
              <div
                style={{
                  position: "absolute",
                  left: "50%",
                  top: 0,
                  bottom: 0,
                  width: 1,
                  background: "rgba(255,255,255,0.15)",
                }}
              />
            </div>
            <div style={{ display: "flex", justifyContent: "space-between", marginTop: 4 }}>
              <span style={{ fontSize: 10, color: "var(--color-btc-text-dim)" }}>Headers</span>
              <span style={{ fontSize: 10, color: "var(--color-btc-text-dim)" }}>Blocks</span>
            </div>
          </div>

          {/* Detail table */}
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <tbody>
              <Row
                label="Sync stage"
                value={
                  status == null
                    ? "Connecting..."
                    : status.syncState === "HEADER_SYNC"
                      ? "Phase 1: Downloading & verifying headers"
                      : status.syncState === "BLOCK_SYNC"
                        ? "Phase 2: Downloading block data"
                        : status.syncState === "SYNCED"
                          ? "Complete"
                          : "Waiting for peers..."
                }
              />
              <Row
                label="Headers"
                value={
                  status
                    ? `${status.headerHeight.toLocaleString()}${status.bestPeerHeight > 0 ? ` / ${status.bestPeerHeight.toLocaleString()}` : ""}`
                    : "..."
                }
              />
              <Row
                label="Blocks"
                value={
                  status
                    ? status.syncState === "HEADER_SYNC"
                      ? "Waiting for headers to complete..."
                      : `${status.blockHeight.toLocaleString()}${status.headerHeight > 0 ? ` / ${status.headerHeight.toLocaleString()}` : ""}`
                    : "..."
                }
              />
              <Row
                label="Connected peers"
                value={status ? `${status.peers}` : "0"}
              />
              <Row
                label="Last block time"
                value={status ? formatBlockTime(status.lastBlockTime) : "Unknown"}
              />
              <Row
                label="Progress increase per hour"
                value={
                  ratePerHour != null ? `${(ratePerHour * 100).toFixed(2)}%` : "calculating..."
                }
              />
              <Row label="Estimated time left" value={eta} />
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
          padding: "8px 16px 8px 0",
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
          padding: "8px 0",
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
