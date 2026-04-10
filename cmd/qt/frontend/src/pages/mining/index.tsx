import { useEffect, useState, useCallback } from "react";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import {
  GetMiningConfig,
  SetMiningConfig,
  SetMining,
  GetStratumStatus,
  StartStratum,
  StopStratum,
} from "../../../wailsjs/go/main/App";

function formatHashrate(h: number): string {
  if (h >= 1e12) return (h / 1e12).toFixed(2) + " TH/s";
  if (h >= 1e9) return (h / 1e9).toFixed(2) + " GH/s";
  if (h >= 1e6) return (h / 1e6).toFixed(2) + " MH/s";
  if (h >= 1e3) return (h / 1e3).toFixed(2) + " KH/s";
  return h.toFixed(1) + " H/s";
}

function formatDuration(secs: number): string {
  if (secs < 60) return `${Math.floor(secs)}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  return `${h}h ${m}m`;
}

const cardStyle: React.CSSProperties = {
  background: "var(--color-btc-card)",
  border: "1px solid var(--color-btc-border)",
};

const labelStyle: React.CSSProperties = {
  color: "var(--color-btc-text-dim)",
};

const headingStyle: React.CSSProperties = {
  color: "var(--color-btc-text-dim)",
};

export function Mining() {
  const coinInfo = useCoinInfo();

  const [config, setConfig] = useState<Record<string, any>>({});
  const [stratum, setStratum] = useState<Record<string, any>>({});
  const [stratumPort, setStratumPort] = useState(3333);
  const [pendingThreads, setPendingThreads] = useState<number | null>(null);
  const [pendingPower, setPendingPower] = useState<number | null>(null);

  const poll = useCallback(() => {
    GetMiningConfig().then(setConfig).catch(() => {});
    GetStratumStatus().then(setStratum).catch(() => {});
  }, []);

  useEffect(() => {
    poll();
    const id = setInterval(poll, 2000);
    return () => clearInterval(id);
  }, [poll]);

  const mining = !!config.mining;
  const hashrate = (config.hashrate as number) || 0;
  const hashrateReady = !!config.hashrateReady;
  const threads = pendingThreads ?? ((config.threads as number) || 1);
  const maxThreads = (config.maxThreads as number) || 1;
  const powerLimit = pendingPower ?? ((config.powerLimit as number) || 100);

  const stratumRunning = !!stratum.running;
  const workerList = (stratum.workerList as any[]) || [];

  function handleToggleMining() {
    SetMining(!mining)
      .then(poll)
      .catch(() => {});
  }

  function handleThreadsChange(val: number) {
    setPendingThreads(val);
    SetMiningConfig(val, powerLimit).then(poll).catch(() => {});
  }

  function handlePowerChange(val: number) {
    setPendingPower(val);
    SetMiningConfig(threads, val).then(poll).catch(() => {});
  }

  function handleToggleStratum() {
    if (stratumRunning) {
      StopStratum().then(poll).catch(() => {});
    } else {
      StartStratum(stratumPort).then(poll).catch(() => {});
    }
  }

  return (
    <div className="flex h-full flex-col gap-3 overflow-y-auto">
      {/* Internal Miner Card */}
      <div className="btc-glow rounded-xl p-4" style={cardStyle}>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-xs font-medium uppercase tracking-wider" style={headingStyle}>
            Internal Miner
          </h3>
          <button
            onClick={handleToggleMining}
            className="rounded px-3 py-1 text-[11px] font-semibold uppercase tracking-wide transition-colors"
            style={{
              background: mining ? "rgba(63, 185, 80, 0.15)" : "rgba(248, 81, 73, 0.15)",
              color: mining ? "var(--color-btc-green)" : "var(--color-btc-red)",
              border: `1px solid ${mining ? "rgba(63, 185, 80, 0.3)" : "rgba(248, 81, 73, 0.3)"}`,
            }}
          >
            {mining ? "Stop Mining" : "Start Mining"}
          </button>
        </div>

        {/* Hashrate display */}
        <div
          className="rounded-lg px-4 py-3 mb-3"
          style={{
            background: "var(--color-btc-deep)",
            border: "1px solid var(--color-btc-border)",
          }}
        >
          <div className="flex items-center justify-between">
            <span className="text-xs" style={{ color: "var(--color-btc-text-muted)" }}>Hashrate</span>
            <span
              className="font-mono text-lg font-bold"
              style={{ color: mining && hashrateReady ? "var(--color-btc-gold)" : "var(--color-btc-text-dim)" }}
            >
              {mining ? (hashrateReady ? formatHashrate(hashrate) : "Warming up...") : "Idle"}
            </span>
          </div>
        </div>

        {/* Thread + Power controls */}
        <div className="grid grid-cols-2 gap-4">
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-[11px] font-medium" style={labelStyle}>Threads</label>
              <span className="font-mono text-xs font-semibold" style={{ color: "var(--color-btc-text)" }}>
                {threads} / {maxThreads}
              </span>
            </div>
            <input
              type="range"
              min={1}
              max={maxThreads}
              value={threads}
              onChange={(e) => handleThreadsChange(parseInt(e.target.value))}
              className="w-full accent-[var(--color-btc-gold)]"
              style={{ height: "6px" }}
            />
          </div>
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-[11px] font-medium" style={labelStyle}>Power Limit</label>
              <span className="font-mono text-xs font-semibold" style={{ color: "var(--color-btc-text)" }}>
                {powerLimit}%
              </span>
            </div>
            <input
              type="range"
              min={1}
              max={100}
              value={powerLimit}
              onChange={(e) => handlePowerChange(parseInt(e.target.value))}
              className="w-full accent-[var(--color-btc-gold)]"
              style={{ height: "6px" }}
            />
          </div>
        </div>
      </div>

      {/* Stratum Server Card */}
      <div className="btc-glow rounded-xl p-4" style={cardStyle}>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-xs font-medium uppercase tracking-wider" style={headingStyle}>
            Stratum Server
          </h3>
          <div className="flex items-center gap-2">
            {!stratumRunning && (
              <div className="flex items-center gap-1">
                <span className="text-[10px]" style={{ color: "var(--color-btc-text-muted)" }}>Port:</span>
                <input
                  type="number"
                  min={1024}
                  max={65535}
                  value={stratumPort}
                  onChange={(e) => setStratumPort(parseInt(e.target.value) || 3333)}
                  className="w-16 rounded px-1.5 py-0.5 text-[11px] font-mono"
                  style={{
                    background: "var(--color-btc-deep)",
                    color: "var(--color-btc-text)",
                    border: "1px solid var(--color-btc-border)",
                  }}
                />
              </div>
            )}
            <button
              onClick={handleToggleStratum}
              className="rounded px-3 py-1 text-[11px] font-semibold uppercase tracking-wide transition-colors"
              style={{
                background: stratumRunning ? "rgba(63, 185, 80, 0.15)" : "rgba(88, 166, 255, 0.12)",
                color: stratumRunning ? "var(--color-btc-green)" : "var(--color-btc-blue)",
                border: `1px solid ${stratumRunning ? "rgba(63, 185, 80, 0.3)" : "rgba(88, 166, 255, 0.25)"}`,
              }}
            >
              {stratumRunning ? "Stop" : "Start"}
            </button>
          </div>
        </div>

        {stratumRunning && (
          <>
            {/* Stratum stats bar */}
            <div
              className="grid grid-cols-4 gap-3 rounded-lg px-3 py-2.5 mb-3"
              style={{
                background: "var(--color-btc-deep)",
                border: "1px solid var(--color-btc-border)",
              }}
            >
              <div className="text-center">
                <p className="text-[10px] uppercase" style={labelStyle}>Listening</p>
                <p className="font-mono text-xs font-semibold" style={{ color: "var(--color-btc-text)" }}>
                  {stratum.listenAddr || "—"}
                </p>
              </div>
              <div className="text-center">
                <p className="text-[10px] uppercase" style={labelStyle}>Workers</p>
                <p className="font-mono text-xs font-semibold" style={{ color: "var(--color-btc-text)" }}>
                  {stratum.workers ?? 0}
                </p>
              </div>
              <div className="text-center">
                <p className="text-[10px] uppercase" style={labelStyle}>Shares</p>
                <p className="font-mono text-xs font-semibold" style={{ color: "var(--color-btc-green)" }}>
                  {stratum.sharesValid ?? 0}
                </p>
              </div>
              <div className="text-center">
                <p className="text-[10px] uppercase" style={labelStyle}>Blocks</p>
                <p className="font-mono text-xs font-semibold" style={{ color: "var(--color-btc-gold)" }}>
                  {stratum.blocksFound ?? 0}
                </p>
              </div>
            </div>

            {/* Connection info */}
            <div
              className="rounded-lg px-3 py-2 mb-3"
              style={{
                background: "rgba(247, 147, 26, 0.06)",
                border: "1px solid rgba(247, 147, 26, 0.15)",
              }}
            >
              <p className="text-[11px]" style={{ color: "var(--color-btc-gold-light)" }}>
                Connect miners to <code className="font-mono" style={{ color: "var(--color-btc-gold)" }}>
                  stratum+tcp://YOUR_IP:{stratumPort}
                </code> with any username and password.
              </p>
            </div>

            {/* Worker table */}
            {workerList.length > 0 && (
              <div className="overflow-x-auto">
                <table className="w-full text-[11px]">
                  <thead>
                    <tr style={{ color: "var(--color-btc-text-muted)" }}>
                      <th className="text-left py-1 px-2 font-medium">Worker</th>
                      <th className="text-left py-1 px-2 font-medium">Address</th>
                      <th className="text-right py-1 px-2 font-medium">Hashrate</th>
                      <th className="text-right py-1 px-2 font-medium">Diff</th>
                      <th className="text-right py-1 px-2 font-medium">Shares</th>
                      <th className="text-right py-1 px-2 font-medium">Stale</th>
                      <th className="text-right py-1 px-2 font-medium">Connected</th>
                    </tr>
                  </thead>
                  <tbody>
                    {workerList.map((w: any, i: number) => (
                      <tr
                        key={i}
                        className="border-t"
                        style={{ borderColor: "var(--color-btc-border)" }}
                      >
                        <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-btc-text)" }}>
                          {w.name || "—"}
                        </td>
                        <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-btc-text-muted)" }}>
                          {w.addr}
                        </td>
                        <td className="py-1.5 px-2 text-right font-mono" style={{ color: "var(--color-btc-gold)" }}>
                          {w.hashrate > 0 ? formatHashrate(w.hashrate) : "—"}
                        </td>
                        <td className="py-1.5 px-2 text-right font-mono" style={{ color: "var(--color-btc-text)" }}>
                          {typeof w.difficulty === "number" ? w.difficulty.toFixed(4) : "—"}
                        </td>
                        <td className="py-1.5 px-2 text-right font-mono" style={{ color: "var(--color-btc-green)" }}>
                          {w.sharesValid ?? 0}
                        </td>
                        <td className="py-1.5 px-2 text-right font-mono" style={{ color: "var(--color-btc-red)" }}>
                          {w.sharesStale ?? 0}
                        </td>
                        <td className="py-1.5 px-2 text-right font-mono" style={{ color: "var(--color-btc-text-muted)" }}>
                          {w.connectedAt ? formatDuration((Date.now() / 1000) - w.connectedAt) : "—"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {workerList.length === 0 && (
              <p className="text-center text-xs py-3" style={{ color: "var(--color-btc-text-dim)" }}>
                No workers connected. Point a miner at the address above.
              </p>
            )}
          </>
        )}

        {!stratumRunning && (
          <p className="text-xs" style={{ color: "var(--color-btc-text-dim)" }}>
            Start the stratum server to accept connections from external mining hardware and software.
            Supports Stratum V1 with automatic variable difficulty (vardiff).
          </p>
        )}
      </div>

      {/* Mining Info Card */}
      <div className="btc-glow rounded-xl p-4 mt-auto" style={cardStyle}>
        <h3 className="text-xs font-medium uppercase tracking-wider mb-2" style={headingStyle}>
          About Mining
        </h3>
        <p className="text-xs leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
          The <strong>Internal Miner</strong> uses your CPU to mine {coinInfo.ticker} directly.
          Adjust the thread count and power limit to balance mining performance with system responsiveness.
          The <strong>Stratum Server</strong> lets you connect external mining software (cgminer, bfgminer, etc.)
          to mine through your wallet. Vardiff automatically adjusts difficulty per worker for optimal share rates.
        </p>
      </div>
    </div>
  );
}
