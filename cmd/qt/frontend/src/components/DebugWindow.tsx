import { useCallback, useEffect, useRef, useState } from "react";
import {
  GetDebugInfo,
  GetPeerList,
  ExecuteRPC,
  ListRPCMethods,
  GetNetworkTotals,
  RescanBlockchain,
} from "../../wailsjs/go/main/App";
import type { PeerEntry } from "@/lib/types";
import { formatPeerPing, normalizePeerList, peerRowKey } from "@/lib/peer-normalize";

interface DebugInfo {
  clientVersion: string;
  userAgent: string;
  datadir: string;
  startupTime: string;
  network: string;
  connections: number;
  inbound: number;
  outbound: number;
  blocks: number;
  bestHash: string;
  lastBlockTime: string;
  mempoolTx: number;
  mempoolBytes: number;
}

interface NetworkTotals {
  totalBytesSent: number;
  totalBytesRecv: number;
  peers: number;
}

interface ConsoleEntry {
  type: "input" | "output" | "error";
  text: string;
}

type Tab = "information" | "console" | "network" | "peers" | "repair";

const tabStyle = (active: boolean): React.CSSProperties => ({
  padding: "6px 16px",
  fontSize: "12px",
  fontWeight: 500,
  cursor: "pointer",
  background: active ? "var(--color-btc-surface)" : "transparent",
  color: active ? "var(--color-btc-gold)" : "var(--color-btc-text-muted)",
  border: active ? "1px solid var(--color-btc-border)" : "1px solid transparent",
  borderBottom: active ? "1px solid var(--color-btc-surface)" : "1px solid var(--color-btc-border)",
  borderRadius: "4px 4px 0 0",
  marginBottom: "-1px",
});

const sectionTitle: React.CSSProperties = {
  fontSize: "12px",
  fontWeight: 700,
  color: "var(--color-btc-text)",
  padding: "8px 0 4px",
};

const rowStyle: React.CSSProperties = {
  display: "flex",
  fontSize: "12px",
  padding: "3px 0",
  color: "var(--color-btc-text-muted)",
};

const labelStyle: React.CSSProperties = {
  width: "200px",
  flexShrink: 0,
  color: "var(--color-btc-text-dim)",
};

function formatBytes(bytes: number): string {
  const n = Number.isFinite(bytes) ? bytes : 0;
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return (n / 1024).toFixed(2) + " KB";
  return (n / (1024 * 1024)).toFixed(2) + " MB";
}

function formatTimestamp(unix: number): string {
  if (!unix) return "—";
  return new Date(unix * 1000).toLocaleString();
}

// ─── Information Tab ───

function InformationTab({ info }: { info: DebugInfo | null }) {
  if (!info)
    return (
      <div style={{ color: "var(--color-btc-text-muted)", fontSize: 12, padding: 16 }}>
        Loading...
      </div>
    );
  return (
    <div style={{ padding: "8px 16px", overflow: "auto", height: "100%" }}>
      <div style={sectionTitle}>General</div>
      <div style={rowStyle}>
        <span style={labelStyle}>Client version</span>
        <span>{info.clientVersion}</span>
      </div>
      <div style={rowStyle}>
        <span style={{ ...labelStyle, paddingLeft: 16 }}>User Agent</span>
        <span>{info.userAgent}</span>
      </div>
      <div style={rowStyle}>
        <span style={labelStyle}>Datadir</span>
        <span style={{ fontFamily: "monospace", fontSize: 11 }}>{info.datadir}</span>
      </div>
      <div style={rowStyle}>
        <span style={labelStyle}>Startup time</span>
        <span>{info.startupTime}</span>
      </div>

      <div style={sectionTitle}>Network</div>
      <div style={rowStyle}>
        <span style={labelStyle}>Name</span>
        <span>{info.network}</span>
      </div>
      <div style={rowStyle}>
        <span style={labelStyle}>Number of connections</span>
        <span>
          {info.connections} (In: {info.inbound} / Out: {info.outbound})
        </span>
      </div>

      <div style={sectionTitle}>Block chain</div>
      <div style={rowStyle}>
        <span style={labelStyle}>Current number of blocks</span>
        <span>{info.blocks.toLocaleString()}</span>
      </div>
      <div style={rowStyle}>
        <span style={labelStyle}>Last block time</span>
        <span>{info.lastBlockTime || "—"}</span>
      </div>

      <div style={sectionTitle}>Memory Pool</div>
      <div style={rowStyle}>
        <span style={labelStyle}>Current number of transactions</span>
        <span>{info.mempoolTx}</span>
      </div>
      <div style={rowStyle}>
        <span style={labelStyle}>Memory usage</span>
        <span>{formatBytes(info.mempoolBytes)}</span>
      </div>
    </div>
  );
}

// ─── Console Tab ───

function ConsoleTab() {
  const [history, setHistory] = useState<ConsoleEntry[]>([
    {
      type: "output",
      text:
        "Welcome to the Fairchain RPC console.\n" +
        "Use up and down arrows to navigate history, and Ctrl-L to clear screen.\n" +
        'Type "help" for an overview of available commands.\n' +
        "WARNING: Scammers have been active, telling users to type commands\n" +
        "here, stealing their wallet contents. Do not use this console without\n" +
        "fully understanding the ramifications of a command.",
    },
  ]);
  const [input, setInput] = useState("");
  const [cmdHistory, setCmdHistory] = useState<string[]>([]);
  const [histIdx, setHistIdx] = useState(-1);
  const [methods, setMethods] = useState<string[]>([]);
  const [acMatches, setAcMatches] = useState<string[]>([]);
  const [acIndex, setAcIndex] = useState(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    ListRPCMethods()
      .then((m) => setMethods(m || []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [history]);

  const commandWord = input.split(/\s/)[0];
  const hasArgs = input.includes(" ");

  useEffect(() => {
    if (!commandWord || hasArgs || methods.length === 0) {
      setAcMatches([]);
      setAcIndex(0);
      return;
    }
    const lower = commandWord.toLowerCase();
    const matches = methods.filter((m) => m.toLowerCase().startsWith(lower));
    setAcMatches(matches);
    setAcIndex(0);
  }, [commandWord, hasArgs, methods]);

  const acceptSuggestion = useCallback(
    (method: string) => {
      const rest = input.slice(commandWord.length);
      setInput(method + (rest || " "));
      setAcMatches([]);
      setAcIndex(0);
      inputRef.current?.focus();
    },
    [input, commandWord],
  );

  const execute = useCallback(async () => {
    const trimmed = input.trim();
    if (!trimmed) return;

    setInput("");
    setAcMatches([]);
    setCmdHistory((prev) => [trimmed, ...prev]);
    setHistIdx(-1);
    setHistory((prev) => [...prev, { type: "input", text: "> " + trimmed }]);

    if (trimmed === "help") {
      const m = methods.length > 0 ? methods : (await ListRPCMethods().catch(() => [])) || [];
      setHistory((prev) => [...prev, { type: "output", text: m.join("\n") }]);
      return;
    }
    if (trimmed === "clear") {
      setHistory([]);
      return;
    }

    const parts = trimmed.split(/\s+/);
    const method = parts[0];
    let paramsJSON = "[]";
    if (parts.length > 1) {
      const rawParams = parts.slice(1).map((p) => {
        if (p === "true" || p === "false") return p;
        if (/^-?\d+$/.test(p)) return p;
        if (/^-?\d+\.\d+$/.test(p)) return p;
        if ((p.startsWith("{") && p.endsWith("}")) || (p.startsWith("[") && p.endsWith("]")))
          return p;
        return JSON.stringify(p);
      });
      paramsJSON = "[" + rawParams.join(",") + "]";
    }

    try {
      const resp = await ExecuteRPC(method, paramsJSON);
      if (resp.error) {
        setHistory((prev) => [...prev, { type: "error", text: resp.error as string }]);
      } else {
        setHistory((prev) => [...prev, { type: "output", text: resp.result as string }]);
      }
    } catch (e: any) {
      setHistory((prev) => [...prev, { type: "error", text: e?.message || String(e) }]);
    }
  }, [input, methods]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (acMatches.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setAcIndex((prev) => Math.min(prev + 1, acMatches.length - 1));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setAcIndex((prev) => Math.max(prev - 1, 0));
        return;
      }
      if (e.key === "Tab" || e.key === "Enter") {
        if (e.key === "Tab" || (e.key === "Enter" && acMatches[acIndex] !== commandWord)) {
          e.preventDefault();
          acceptSuggestion(acMatches[acIndex]);
          return;
        }
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setAcMatches([]);
        return;
      }
    }

    if (e.key === "Enter") {
      e.preventDefault();
      execute();
    } else if (e.key === "ArrowUp" && acMatches.length === 0) {
      e.preventDefault();
      if (cmdHistory.length > 0) {
        const next = Math.min(histIdx + 1, cmdHistory.length - 1);
        setHistIdx(next);
        setInput(cmdHistory[next]);
      }
    } else if (e.key === "ArrowDown" && acMatches.length === 0) {
      e.preventDefault();
      if (histIdx > 0) {
        const next = histIdx - 1;
        setHistIdx(next);
        setInput(cmdHistory[next]);
      } else {
        setHistIdx(-1);
        setInput("");
      }
    } else if (e.key === "Tab") {
      e.preventDefault();
    } else if (e.key === "l" && e.ctrlKey) {
      e.preventDefault();
      setHistory([]);
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInput(e.target.value);
    setHistIdx(-1);
  };

  const showDropdown = acMatches.length > 0 && !hasArgs && commandWord.length > 0;

  return (
    <div
      style={{ display: "flex", flexDirection: "column", height: "100%" }}
      onClick={() => inputRef.current?.focus()}
    >
      <div
        ref={scrollRef}
        style={{
          flex: 1,
          overflow: "auto",
          padding: "8px 12px",
          fontFamily: "monospace",
          fontSize: 11,
        }}
      >
        {history.map((entry, i) => (
          <div
            key={i}
            style={{
              color:
                entry.type === "error"
                  ? "var(--color-btc-red)"
                  : entry.type === "input"
                    ? "var(--color-btc-gold)"
                    : "var(--color-btc-text-muted)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-all",
              padding: "1px 0",
            }}
          >
            {entry.text}
          </div>
        ))}
      </div>

      <div ref={wrapperRef} style={{ position: "relative" }}>
        {showDropdown && (
          <div
            style={{
              position: "absolute",
              bottom: "100%",
              left: 0,
              right: 0,
              maxHeight: 180,
              overflowY: "auto",
              background: "var(--color-btc-surface)",
              border: "1px solid var(--color-btc-border)",
              borderBottom: "none",
              zIndex: 10,
            }}
          >
            {acMatches.map((m, i) => (
              <div
                key={m}
                onMouseDown={(e) => {
                  e.preventDefault();
                  acceptSuggestion(m);
                }}
                onMouseEnter={() => setAcIndex(i)}
                style={{
                  padding: "3px 12px",
                  fontSize: 12,
                  fontFamily: "monospace",
                  cursor: "pointer",
                  background: i === acIndex ? "var(--color-btc-gold)" : "transparent",
                  color: i === acIndex ? "var(--color-btc-deep)" : "var(--color-btc-text-muted)",
                }}
              >
                {m}
              </div>
            ))}
          </div>
        )}

        <div
          style={{
            display: "flex",
            alignItems: "center",
            borderTop: "1px solid var(--color-btc-border)",
            padding: "6px 12px",
            background: "var(--color-btc-surface)",
          }}
        >
          <span
            style={{
              color: "var(--color-btc-gold)",
              fontFamily: "monospace",
              fontSize: 12,
              marginRight: 6,
            }}
          >
            &gt;
          </span>
          <input
            ref={inputRef}
            value={input}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            placeholder="Enter RPC command..."
            autoFocus
            style={{
              flex: 1,
              background: "transparent",
              border: "none",
              outline: "none",
              color: "var(--color-btc-text)",
              fontFamily: "monospace",
              fontSize: 12,
            }}
          />
        </div>
      </div>
    </div>
  );
}

// ─── Network Traffic Tab ───

interface TrafficSnapshot {
  time: number;
  sent: number;
  recv: number;
}

function NetworkTrafficTab() {
  const [totals, setTotals] = useState<NetworkTotals | null>(null);
  const [snapshots, setSnapshots] = useState<TrafficSnapshot[]>([]);
  const prevRef = useRef<{ sent: number; recv: number; time: number } | null>(null);

  useEffect(() => {
    const poll = () => {
      GetNetworkTotals()
        .then((t) => {
          const nt = t as unknown as NetworkTotals;
          setTotals(nt);
          const now = Date.now();
          if (prevRef.current) {
            const dt = (now - prevRef.current.time) / 1000;
            if (dt > 0) {
              const sentRate = (nt.totalBytesSent - prevRef.current.sent) / dt;
              const recvRate = (nt.totalBytesRecv - prevRef.current.recv) / dt;
              setSnapshots((prev) => [
                ...prev.slice(-59),
                { time: now, sent: sentRate, recv: recvRate },
              ]);
            }
          }
          prevRef.current = { sent: nt.totalBytesSent, recv: nt.totalBytesRecv, time: now };
        })
        .catch(() => {});
    };
    poll();
    const id = setInterval(poll, 2000);
    return () => clearInterval(id);
  }, []);

  const maxRate = Math.max(1, ...snapshots.map((s) => Math.max(s.sent, s.recv)));
  const chartH = 160;
  const barW = Math.max(4, Math.floor(620 / 60));

  return (
    <div style={{ padding: "12px 16px", overflow: "auto", height: "100%" }}>
      <div style={sectionTitle}>Totals</div>
      {totals && (
        <>
          <div style={rowStyle}>
            <span style={labelStyle}>Total bytes sent</span>
            <span>{formatBytes(totals.totalBytesSent)}</span>
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>Total bytes received</span>
            <span>{formatBytes(totals.totalBytesRecv)}</span>
          </div>
          <div style={rowStyle}>
            <span style={labelStyle}>Connected peers</span>
            <span>{totals.peers}</span>
          </div>
        </>
      )}

      <div style={{ ...sectionTitle, marginTop: 12 }}>Traffic Rate (bytes/sec)</div>
      <div
        style={{
          display: "flex",
          gap: 16,
          fontSize: 11,
          color: "var(--color-btc-text-dim)",
          marginBottom: 6,
        }}
      >
        <span>
          <span
            style={{
              display: "inline-block",
              width: 10,
              height: 10,
              background: "var(--color-btc-green)",
              marginRight: 4,
              borderRadius: 2,
            }}
          />
          Sent
        </span>
        <span>
          <span
            style={{
              display: "inline-block",
              width: 10,
              height: 10,
              background: "var(--color-btc-gold)",
              marginRight: 4,
              borderRadius: 2,
            }}
          />
          Received
        </span>
      </div>
      <div
        style={{
          height: chartH,
          background: "var(--color-btc-surface)",
          border: "1px solid var(--color-btc-border)",
          borderRadius: 4,
          display: "flex",
          alignItems: "flex-end",
          padding: "4px 4px 0",
          gap: 1,
          overflow: "hidden",
        }}
      >
        {snapshots.map((s, i) => (
          <div
            key={i}
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              width: barW,
              flexShrink: 0,
              gap: 1,
            }}
          >
            <div
              style={{
                width: "100%",
                display: "flex",
                gap: 1,
                alignItems: "flex-end",
                height: chartH - 8,
              }}
            >
              <div
                style={{
                  flex: 1,
                  background: "var(--color-btc-green)",
                  opacity: 0.7,
                  height: Math.max(1, (s.sent / maxRate) * (chartH - 8)),
                  borderRadius: "2px 2px 0 0",
                }}
              />
              <div
                style={{
                  flex: 1,
                  background: "var(--color-btc-gold)",
                  opacity: 0.7,
                  height: Math.max(1, (s.recv / maxRate) * (chartH - 8)),
                  borderRadius: "2px 2px 0 0",
                }}
              />
            </div>
          </div>
        ))}
        {snapshots.length === 0 && (
          <div
            style={{
              flex: 1,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              color: "var(--color-btc-text-dim)",
              fontSize: 11,
            }}
          >
            Collecting data...
          </div>
        )}
      </div>
      <div
        style={{
          textAlign: "right",
          fontSize: 10,
          color: "var(--color-btc-text-dim)",
          marginTop: 4,
        }}
      >
        Peak: {formatBytes(Math.round(maxRate))}/s
      </div>
    </div>
  );
}

// ─── Peers Tab ───

function PeersTab({ peers }: { peers: PeerEntry[] }) {
  const [selectedKey, setSelectedKey] = useState<string | null>(null);

  useEffect(() => {
    if (selectedKey && !peers.some((p) => peerRowKey(p) === selectedKey)) {
      setSelectedKey(null);
    }
  }, [peers, selectedKey]);

  const selectedPeer = selectedKey
    ? peers.find((p) => peerRowKey(p) === selectedKey) ?? null
    : null;

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", overflow: "hidden" }}>
      <div style={{ flex: 1, overflow: "auto", borderBottom: "1px solid var(--color-btc-border)" }}>
        <table style={{ width: "100%", fontSize: 11, borderCollapse: "collapse" }}>
          <thead>
            <tr
              style={{
                position: "sticky",
                top: 0,
                background: "var(--color-btc-surface)",
                borderBottom: "1px solid var(--color-btc-border)",
              }}
            >
              {["Address", "Type", "User Agent", "Ping", "Sent", "Received", "Height"].map((h) => (
                <th
                  key={h}
                  style={{
                    textAlign: "left",
                    padding: "6px 8px",
                    color: "var(--color-btc-text-dim)",
                    fontWeight: 600,
                  }}
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {peers.map((p) => (
              <tr
                key={peerRowKey(p)}
                onClick={() => setSelectedKey(peerRowKey(p))}
                style={{
                  cursor: "pointer",
                  background:
                    selectedKey === peerRowKey(p) ? "rgba(247,147,26,0.1)" : "transparent",
                  borderBottom: "1px solid var(--color-btc-border)",
                }}
              >
                <td
                  style={{
                    padding: "4px 8px",
                    color: "var(--color-btc-text-muted)",
                    fontFamily: "monospace",
                  }}
                >
                  {p.addr}
                </td>
                <td
                  style={{
                    padding: "4px 8px",
                    color: p.inbound ? "var(--color-btc-text-dim)" : "var(--color-btc-green)",
                  }}
                >
                  {p.inbound ? "In" : "Out"}
                </td>
                <td style={{ padding: "4px 8px", color: "var(--color-btc-text-muted)" }}>
                  {p.subver}
                </td>
                <td
                  style={{
                    padding: "4px 8px",
                    color: "var(--color-btc-text-muted)",
                    fontFamily: "monospace",
                  }}
                >
                  {formatPeerPing(p.pingTime)}
                </td>
                <td style={{ padding: "4px 8px", color: "var(--color-btc-text-muted)" }}>
                  {formatBytes(p.bytesSent)}
                </td>
                <td style={{ padding: "4px 8px", color: "var(--color-btc-text-muted)" }}>
                  {formatBytes(p.bytesRecv)}
                </td>
                <td
                  style={{
                    padding: "4px 8px",
                    color: "var(--color-btc-text-muted)",
                    fontFamily: "monospace",
                  }}
                >
                  {p.startingHeight}
                </td>
              </tr>
            ))}
            {peers.length === 0 && (
              <tr>
                <td
                  colSpan={7}
                  style={{
                    padding: 16,
                    textAlign: "center",
                    color: "var(--color-btc-text-dim)",
                    fontSize: 12,
                  }}
                >
                  No peers connected
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {selectedPeer && (
        <div style={{ padding: "8px 16px", fontSize: 11, color: "var(--color-btc-text-muted)" }}>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Address</span>
            <span style={{ fontFamily: "monospace" }}>{selectedPeer.addr}</span>
          </div>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Local address</span>
            <span style={{ fontFamily: "monospace" }}>{selectedPeer.addrLocal || "—"}</span>
          </div>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Connected since</span>
            <span>{formatTimestamp(selectedPeer.connTime)}</span>
          </div>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Ping (RTT)</span>
            <span>{formatPeerPing(selectedPeer.pingTime)}</span>
          </div>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Bytes sent</span>
            <span>{formatBytes(selectedPeer.bytesSent)}</span>
          </div>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Bytes received</span>
            <span>{formatBytes(selectedPeer.bytesRecv)}</span>
          </div>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Last send</span>
            <span>{formatTimestamp(selectedPeer.lastSend)}</span>
          </div>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Last receive</span>
            <span>{formatTimestamp(selectedPeer.lastRecv)}</span>
          </div>
          <div style={rowStyle}>
            <span style={{ ...labelStyle, width: 140 }}>Ban score</span>
            <span>{selectedPeer.banScore}</span>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Wallet Repair Tab ───

function WalletRepairTab() {
  const [status, setStatus] = useState<string | null>(null);
  const [running, setRunning] = useState(false);

  const handleRescan = async () => {
    setRunning(true);
    setStatus("Rescanning blockchain...");
    try {
      const msg = await RescanBlockchain();
      setStatus(msg);
    } catch (e: any) {
      setStatus("Error: " + (e?.message || String(e)));
    } finally {
      setRunning(false);
    }
  };

  const btnStyle: React.CSSProperties = {
    padding: "8px 20px",
    fontSize: 12,
    fontWeight: 500,
    border: "1px solid var(--color-btc-border)",
    borderRadius: 4,
    cursor: running ? "not-allowed" : "pointer",
    opacity: running ? 0.5 : 1,
    color: "var(--color-btc-text)",
    background: "var(--color-btc-surface)",
  };

  return (
    <div style={{ padding: "16px", overflow: "auto", height: "100%" }}>
      <div
        style={{
          fontSize: 12,
          color: "var(--color-btc-text-muted)",
          marginBottom: 16,
          lineHeight: 1.6,
        }}
      >
        These options can be used to repair your wallet or rescan the blockchain. Use with caution —
        some operations may take a long time.
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <button style={btnStyle} disabled={running} onClick={handleRescan}>
            Rescan blockchain
          </button>
          <span style={{ fontSize: 11, color: "var(--color-btc-text-dim)" }}>
            Rebuild the UTXO set from stored blocks. Fixes missing or incorrect balances.
          </span>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <button style={{ ...btnStyle, cursor: "not-allowed", opacity: 0.4 }} disabled>
            Recover transactions
          </button>
          <span style={{ fontSize: 11, color: "var(--color-btc-text-dim)" }}>
            Attempt to recover transactions from the blockchain (not yet implemented).
          </span>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <button style={{ ...btnStyle, cursor: "not-allowed", opacity: 0.4 }} disabled>
            Upgrade wallet format
          </button>
          <span style={{ fontSize: 11, color: "var(--color-btc-text-dim)" }}>
            Upgrade the wallet to the latest format (not yet implemented).
          </span>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <button style={{ ...btnStyle, cursor: "not-allowed", opacity: 0.4 }} disabled>
            Rebuild index
          </button>
          <span style={{ fontSize: 11, color: "var(--color-btc-text-dim)" }}>
            Rebuild the block index from disk (not yet implemented).
          </span>
        </div>
      </div>

      {status && (
        <div
          style={{
            marginTop: 20,
            padding: "10px 14px",
            fontSize: 12,
            borderRadius: 4,
            border: "1px solid var(--color-btc-border)",
            background: "var(--color-btc-surface)",
            color: status.startsWith("Error") ? "var(--color-btc-red)" : "var(--color-btc-green)",
          }}
        >
          {status}
        </div>
      )}
    </div>
  );
}

// ─── Debug Window ───

export function DebugWindow({ onClose }: { onClose: () => void }) {
  const [tab, setTab] = useState<Tab>("information");
  const [info, setInfo] = useState<DebugInfo | null>(null);
  const [peers, setPeers] = useState<PeerEntry[]>([]);

  useEffect(() => {
    const poll = () => {
      GetDebugInfo()
        .then((d) => setInfo(d as unknown as DebugInfo))
        .catch(() => {});
      GetPeerList()
        .then((p) => setPeers(normalizePeerList(p)))
        .catch(() => {});
    };
    poll();
    const id = setInterval(poll, 2000);
    return () => clearInterval(id);
  }, []);

  const tabs: { id: Tab; label: string }[] = [
    { id: "information", label: "Information" },
    { id: "console", label: "Console" },
    { id: "network", label: "Network Traffic" },
    { id: "peers", label: "Peers" },
    { id: "repair", label: "Wallet Repair" },
  ];

  return (
    <div
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 9999,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "rgba(0,0,0,0.6)",
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        style={{
          width: 720,
          height: 520,
          background: "var(--color-btc-deep)",
          border: "1px solid var(--color-btc-border)",
          borderRadius: 8,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          boxShadow: "0 8px 32px rgba(0,0,0,0.5)",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "10px 16px",
            borderBottom: "1px solid var(--color-btc-border)",
            background: "var(--color-btc-surface)",
          }}
        >
          <span style={{ fontSize: 13, fontWeight: 600, color: "var(--color-btc-text)" }}>
            Debug window
          </span>
          <button
            onClick={onClose}
            style={{
              background: "none",
              border: "none",
              color: "var(--color-btc-text-muted)",
              cursor: "pointer",
              fontSize: 18,
              lineHeight: 1,
              padding: "0 4px",
            }}
          >
            ×
          </button>
        </div>

        <div
          style={{
            display: "flex",
            gap: 2,
            padding: "8px 16px 0",
            borderBottom: "1px solid var(--color-btc-border)",
          }}
        >
          {tabs.map((t) => (
            <div key={t.id} style={tabStyle(tab === t.id)} onClick={() => setTab(t.id)}>
              {t.label}
            </div>
          ))}
        </div>

        <div style={{ flex: 1, overflow: "hidden" }}>
          {tab === "information" && <InformationTab info={info} />}
          {tab === "console" && <ConsoleTab />}
          {tab === "network" && <NetworkTrafficTab />}
          {tab === "peers" && <PeersTab peers={peers} />}
          {tab === "repair" && <WalletRepairTab />}
        </div>
      </div>
    </div>
  );
}
