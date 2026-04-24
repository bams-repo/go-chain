import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import {
  GetBalance,
  GetWalletAddress,
  GetBlockchainInfo,
  GetPeerCount,
  GetSyncStatus,
  GetUpdateStatus,
  GetNodeConfig,
  OpenDataDir,
  InstallService,
  UninstallService,
  IsServiceInstalled,
  TestPort,
  GetAddressLabel,
} from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime/runtime";

function NetworkIcon({ peers }: { peers: number }) {
  const bars = peers >= 8 ? 4 : peers >= 4 ? 3 : peers >= 1 ? 2 : peers > 0 ? 1 : 0;
  const gold = "var(--color-btc-gold)";
  const dim = "var(--color-btc-text-dim)";
  return (
    <svg
      className="h-5 w-5"
      viewBox="0 0 24 24"
      fill="none"
      strokeWidth={2.5}
      strokeLinecap="round"
    >
      <line x1="6" y1="20" x2="6" y2="17" stroke={bars >= 1 ? gold : dim} />
      <line x1="10" y1="20" x2="10" y2="14" stroke={bars >= 2 ? gold : dim} />
      <line x1="14" y1="20" x2="14" y2="10" stroke={bars >= 3 ? gold : dim} />
      <line x1="18" y1="20" x2="18" y2="6" stroke={bars >= 4 ? gold : dim} />
    </svg>
  );
}

function SyncIcon({ progress }: { progress: number }) {
  const synced = progress >= 0.999;
  if (synced) {
    return (
      <svg
        className="h-5 w-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="var(--color-btc-green)"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M22 11.08V12a10 10 0 11-5.93-9.14" />
        <polyline points="22 4 12 14.01 9 11.01" />
      </svg>
    );
  }
  return (
    <svg
      className="h-5 w-5 animate-spin"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-btc-gold)"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M21 12a9 9 0 11-6.219-8.56" />
    </svg>
  );
}

function detectOS(): "linux" | "mac" | "windows" | "unknown" {
  const ua = navigator.userAgent.toLowerCase();
  if (ua.includes("linux")) return "linux";
  if (ua.includes("mac")) return "mac";
  if (ua.includes("win")) return "windows";
  return "unknown";
}

function PortForwardingDialog({
  port,
  coinName,
  serviceInstalled,
  onToggleService,
  portTestResult,
  portTesting,
  onTestPort,
  onClose,
}: {
  port: string;
  coinName: string;
  serviceInstalled: boolean;
  onToggleService: () => void;
  portTestResult: null | { open: boolean; publicIP: string };
  portTesting: boolean;
  onTestPort: () => void;
  onClose: () => void;
}) {
  const [showAllOS, setShowAllOS] = useState(false);
  const os = detectOS();

  const stepLabelStyle: React.CSSProperties = {
    color: "var(--color-btc-gold)",
    fontSize: "11px",
    fontWeight: 600,
    textTransform: "uppercase",
    letterSpacing: "0.06em",
    marginBottom: "4px",
  };
  const bodyStyle: React.CSSProperties = { color: "var(--color-btc-text-muted)", fontSize: "12px", lineHeight: "1.6" };
  const codeStyle: React.CSSProperties = {
    background: "var(--color-btc-deep)",
    color: "var(--color-btc-gold-light)",
    border: "1px solid var(--color-btc-border)",
    borderRadius: "4px",
    padding: "6px 10px",
    fontSize: "11px",
    fontFamily: "monospace",
    display: "block",
    marginTop: "4px",
    wordBreak: "break-all",
  };

  const showLinux = showAllOS || os === "linux" || os === "unknown";
  const showMac = showAllOS || os === "mac";
  const showWindows = showAllOS || os === "windows";

  return (
    <div
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 1100,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "rgba(0, 0, 0, 0.7)",
        backdropFilter: "blur(4px)",
      }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div
        style={{
          background: "var(--color-btc-surface)",
          border: "1px solid var(--color-btc-border)",
          borderRadius: "12px",
          width: "min(520px, 90vw)",
          maxHeight: "80vh",
          overflow: "hidden",
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* Header */}
        <div
          style={{
            padding: "16px 20px",
            borderBottom: "1px solid var(--color-btc-border)",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
          }}
        >
          <h2 style={{ color: "var(--color-btc-text)", fontSize: "14px", fontWeight: 600, margin: 0 }}>
            How to Support the {coinName} Network
          </h2>
          <button
            onClick={onClose}
            style={{
              background: "none",
              border: "none",
              color: "var(--color-btc-text-muted)",
              cursor: "pointer",
              padding: "4px",
              fontSize: "18px",
              lineHeight: 1,
            }}
          >
            &times;
          </button>
        </div>

        {/* Scrollable body */}
        <div style={{ padding: "16px 20px", overflowY: "auto", flex: 1 }}>
          {/* Step 1 */}
          <div style={{ marginBottom: "16px" }}>
            <p style={stepLabelStyle}>Step 1: Enable Auto-Start</p>
            <p style={bodyStyle}>
              Keep your node running to strengthen the network. Enable auto-start so it launches when you log in.
            </p>
            <div style={{ marginTop: "8px", display: "flex", alignItems: "center", gap: "8px" }}>
              <button
                onClick={onToggleService}
                className="rounded px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wide transition-colors"
                style={{
                  background: serviceInstalled ? "rgba(63, 185, 80, 0.15)" : "rgba(248, 81, 73, 0.15)",
                  color: serviceInstalled ? "var(--color-btc-green)" : "var(--color-btc-red)",
                  border: `1px solid ${serviceInstalled ? "rgba(63, 185, 80, 0.3)" : "rgba(248, 81, 73, 0.3)"}`,
                }}
              >
                {serviceInstalled ? "Enabled" : "Disabled"}
              </button>
              {serviceInstalled && (
                <span style={{ color: "var(--color-btc-green)", fontSize: "11px" }}>Auto-start is active</span>
              )}
            </div>
          </div>

          {/* Step 2 */}
          <div style={{ marginBottom: "16px" }}>
            <p style={stepLabelStyle}>Step 2: Open P2P Port {port}</p>

            {/* Router */}
            <p style={{ ...bodyStyle, fontWeight: 600, color: "var(--color-btc-text)", marginTop: "8px", marginBottom: "4px" }}>
              Router Port Forwarding
            </p>
            <ol style={{ ...bodyStyle, paddingLeft: "18px", margin: "0 0 8px 0" }}>
              <li>Open your router admin page (usually <code style={{ fontSize: "11px", color: "var(--color-btc-gold-light)" }}>192.168.1.1</code> or <code style={{ fontSize: "11px", color: "var(--color-btc-gold-light)" }}>192.168.0.1</code>)</li>
              <li>Find <strong>Port Forwarding</strong>, <strong>NAT</strong>, or <strong>Virtual Server</strong> settings</li>
              <li>Add a rule: External Port <strong>{port}</strong> &rarr; Internal Port <strong>{port}</strong>, Protocol: <strong>TCP</strong>, to your computer&apos;s local IP</li>
              <li>Save and apply</li>
            </ol>

            {/* Firewall */}
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginTop: "10px", marginBottom: "6px" }}>
              <p style={{ ...bodyStyle, fontWeight: 600, color: "var(--color-btc-text)", margin: 0 }}>
                Firewall Rules
              </p>
              <button
                onClick={() => setShowAllOS(!showAllOS)}
                style={{
                  background: "none",
                  border: "none",
                  color: "var(--color-btc-blue)",
                  cursor: "pointer",
                  fontSize: "10px",
                  textDecoration: "underline",
                }}
              >
                {showAllOS ? "Show my OS only" : "Show all platforms"}
              </button>
            </div>

            {showLinux && (
              <div style={{ marginBottom: "8px" }}>
                <p style={{ ...bodyStyle, fontWeight: 500, color: "var(--color-btc-text-muted)", marginBottom: "2px" }}>Linux (UFW):</p>
                <code style={codeStyle}>sudo ufw allow {port}/tcp</code>
                <p style={{ ...bodyStyle, fontWeight: 500, color: "var(--color-btc-text-muted)", marginTop: "6px", marginBottom: "2px" }}>Linux (firewalld):</p>
                <code style={codeStyle}>sudo firewall-cmd --add-port={port}/tcp --permanent && sudo firewall-cmd --reload</code>
              </div>
            )}

            {showMac && (
              <div style={{ marginBottom: "8px" }}>
                <p style={{ ...bodyStyle, fontWeight: 500, color: "var(--color-btc-text-muted)", marginBottom: "2px" }}>macOS:</p>
                <p style={bodyStyle}>
                  System Settings &rarr; Network &rarr; Firewall &rarr; Options &rarr; Allow incoming connections for <strong>{coinName} Wallet</strong>
                </p>
              </div>
            )}

            {showWindows && (
              <div style={{ marginBottom: "8px" }}>
                <p style={{ ...bodyStyle, fontWeight: 500, color: "var(--color-btc-text-muted)", marginBottom: "2px" }}>Windows (PowerShell as Admin):</p>
                <code style={codeStyle}>netsh advfirewall firewall add rule name=&quot;{coinName} P2P&quot; dir=in action=allow protocol=TCP localport={port}</code>
              </div>
            )}
          </div>

          {/* Step 3 */}
          <div>
            <p style={stepLabelStyle}>Step 3: Test Your Port</p>
            <p style={bodyStyle}>
              After configuring your router and firewall, test that port {port} is reachable from the internet.
            </p>
            <div style={{ marginTop: "8px", display: "flex", alignItems: "center", gap: "10px" }}>
              <button
                onClick={onTestPort}
                disabled={portTesting}
                className="rounded px-3 py-1 text-[11px] font-semibold uppercase tracking-wide transition-colors disabled:opacity-50"
                style={{
                  background: "rgba(88, 166, 255, 0.12)",
                  color: "var(--color-btc-blue)",
                  border: "1px solid rgba(88, 166, 255, 0.25)",
                }}
              >
                {portTesting ? "Testing..." : "Test Port"}
              </button>
              {portTestResult != null && (
                <span style={{
                  fontSize: "11px",
                  fontWeight: 600,
                  color: portTestResult.open ? "var(--color-btc-green)" : "var(--color-btc-red)",
                }}>
                  {portTestResult.open
                    ? `Port ${port} is open` + (portTestResult.publicIP ? ` (${portTestResult.publicIP})` : "")
                    : `Port ${port} is not reachable — check your router and firewall settings`}
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Footer */}
        <div
          style={{
            padding: "12px 20px",
            borderTop: "1px solid var(--color-btc-border)",
            display: "flex",
            justifyContent: "flex-end",
          }}
        >
          <button
            onClick={onClose}
            className="rounded px-4 py-1.5 text-xs font-semibold transition-colors"
            style={{
              background: "var(--color-btc-card)",
              color: "var(--color-btc-text)",
              border: "1px solid var(--color-btc-border)",
            }}
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

export function Overview() {
  const coinInfo = useCoinInfo();
  const navigate = useNavigate();
  const [confirmed, setConfirmed] = useState(0);
  const [unconfirmed, setUnconfirmed] = useState(0);
  const [address, setAddress] = useState("");
  const [addressLabel, setAddressLabel] = useState("");
  const [addressCopied, setAddressCopied] = useState(false);
  const [height, setHeight] = useState(0);
  const [bestHash, setBestHash] = useState("");
  const [peers, setPeers] = useState(0);
  const [syncProgress, setSyncProgress] = useState(0);
  const [syncState, setSyncState] = useState("INITIAL");
  const [updateAvailable, setUpdateAvailable] = useState(false);
  const [protocolOutdated, setProtocolOutdated] = useState(false);
  const [networkVersion, setNetworkVersion] = useState("");

  const copyAddress = useCallback(() => {
    if (!address) return;
    navigator.clipboard.writeText(address).then(() => {
      setAddressCopied(true);
      setTimeout(() => setAddressCopied(false), 2000);
    });
  }, [address]);
  const [releasesURL, setReleasesURL] = useState("");

  const [nodeConfig, setNodeConfig] = useState<Record<string, any>>({});
  const [serviceInstalled, setServiceInstalled] = useState(false);
  const [serviceMsg, setServiceMsg] = useState("");
  const [portTestResult, setPortTestResult] = useState<null | { open: boolean; publicIP: string }>(null);
  const [portTesting, setPortTesting] = useState(false);
  const [showHelpDialog, setShowHelpDialog] = useState(false);

  useEffect(() => {
    const poll = () => {
      GetBalance().then((b) => {
        setConfirmed(b.confirmed as number);
        setUnconfirmed(b.unconfirmed as number);
      });
      GetBlockchainInfo().then((info) => {
        setHeight(info.height as number);
        setBestHash(info.bestHash as string);
      });
      GetPeerCount().then(setPeers);
      GetSyncStatus()
        .then((s) => {
          if (typeof s.progress === "number") setSyncProgress(s.progress as number);
          if (typeof s.syncState === "string") setSyncState(s.syncState as string);
        })
        .catch(() => {});
      GetUpdateStatus()
        .then((u) => {
          setUpdateAvailable(!!u.available);
          setProtocolOutdated(!!u.protocolOutdated);
          if (u.networkVersion) setNetworkVersion(u.networkVersion as string);
          if (u.releasesURL) setReleasesURL(u.releasesURL as string);
        })
        .catch(() => {});
      GetNodeConfig().then(setNodeConfig).catch(() => {});
      IsServiceInstalled().then(setServiceInstalled).catch(() => {});
      if (!address) {
        GetWalletAddress()
          .then((a) => {
            if (a) {
              setAddress(a);
              GetAddressLabel(a).then((lbl) => { if (lbl) setAddressLabel(lbl); }).catch(() => {});
            }
          })
          .catch(() => {});
      }
    };
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, [address]);

  function handleServiceToggle() {
    const action = serviceInstalled ? UninstallService() : InstallService();
    action
      .then((msg) => {
        setServiceMsg(msg);
        setServiceInstalled(!serviceInstalled);
        setTimeout(() => setServiceMsg(""), 4000);
      })
      .catch((err) => {
        setServiceMsg("Error: " + err);
        setTimeout(() => setServiceMsg(""), 4000);
      });
  }

  function handleTestPort() {
    setPortTesting(true);
    setPortTestResult(null);
    TestPort()
      .then((r) => {
        setPortTestResult({ open: !!r.open, publicIP: (r.publicIP as string) || "" });
      })
      .catch(() => {
        setPortTestResult({ open: false, publicIP: "" });
      })
      .finally(() => setPortTesting(false));
  }

  function formatUptime(seconds: number): string {
    if (!seconds || seconds < 0) return "—";
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}d ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  const synced = syncState === "SYNCED";

  return (
    <div className="flex h-full flex-col gap-3">
      {/* Protocol incompatible banner — wallet cannot sync with the network */}
      {protocolOutdated && (
        <div
          className="flex items-center gap-3 rounded-xl px-5 py-3"
          style={{
            background: "linear-gradient(135deg, #7f1d1d 0%, #450a0a 100%)",
            border: "1px solid #f87171",
          }}
        >
          <svg
            className="h-6 w-6 flex-shrink-0"
            viewBox="0 0 24 24"
            fill="none"
            stroke="#fef2f2"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="12" cy="12" r="10" />
            <line x1="15" y1="9" x2="9" y2="15" />
            <line x1="9" y1="9" x2="15" y2="15" />
          </svg>
          <div className="flex-1 text-sm" style={{ color: "#fef2f2" }}>
            <span className="font-bold">Wallet incompatible!</span>{" "}
            This wallet is not compatible with the latest network version and cannot sync.
            {" "}Update your wallet here:{" "}
            {releasesURL && (
              <span
                role="link"
                tabIndex={0}
                onClick={() => BrowserOpenURL(releasesURL)}
                onKeyDown={(e) => { if (e.key === "Enter") BrowserOpenURL(releasesURL); }}
                className="underline font-bold cursor-pointer"
                style={{ color: "#fef2f2" }}
              >
                {releasesURL}
              </span>
            )}
          </div>
        </div>
      )}

      {/* Update available banner */}
      {updateAvailable && !protocolOutdated && (
        <div
          className="flex items-center gap-3 rounded-xl px-5 py-3"
          style={{
            background: "linear-gradient(135deg, #dc2626 0%, #991b1b 100%)",
            border: "1px solid #fca5a5",
          }}
        >
          <svg
            className="h-5 w-5 flex-shrink-0"
            viewBox="0 0 24 24"
            fill="none"
            stroke="#fef2f2"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
          <div className="flex-1 text-sm" style={{ color: "#fef2f2" }}>
            <span className="font-semibold">Update available!</span>{" "}
            A newer version{networkVersion ? ` (v${networkVersion})` : ""} has been detected on the
            network.{" "}
            {releasesURL && (
              <span
                role="link"
                tabIndex={0}
                onClick={() => BrowserOpenURL(releasesURL)}
                onKeyDown={(e) => { if (e.key === "Enter") BrowserOpenURL(releasesURL); }}
                className="underline font-medium cursor-pointer"
                style={{ color: "#fef2f2" }}
              >
                Download the latest release
              </span>
            )}
          </div>
        </div>
      )}

      {/* Balance */}
      <div
        className="btc-noise btc-glow-active relative overflow-hidden rounded-xl p-5"
        style={{
          background:
            "linear-gradient(135deg, var(--color-btc-card) 0%, var(--color-btc-surface) 100%)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <div
          className="absolute -right-8 -top-8 h-32 w-32 rounded-full opacity-[0.04]"
          style={{ background: "var(--color-btc-gold)" }}
        />
        <div className="relative z-10 flex flex-col gap-3">
          {/* Spendable balance */}
          <div>
            <h3
              className="mb-1 text-[10px] font-semibold uppercase tracking-wider"
              style={{ color: "var(--color-btc-text-dim)" }}
            >
              Spendable Balance
            </h3>
            <p className="text-3xl font-bold" style={{ color: "var(--color-btc-text)" }}>
              {confirmed.toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)}{" "}
              <span className="text-lg font-medium" style={{ color: "var(--color-btc-gold)" }}>
                {coinInfo.ticker}
              </span>
            </p>
          </div>
          {/* Unconfirmed balance */}
          <div
            className="border-t pt-3"
            style={{ borderColor: "rgba(255,255,255,0.06)" }}
          >
            <h3
              className="mb-1 text-[10px] font-semibold uppercase tracking-wider"
              style={{ color: "var(--color-btc-text-dim)" }}
            >
              Unconfirmed Balance
            </h3>
            <p className="text-xl font-bold" style={{ color: "var(--color-btc-gold-light)" }}>
              {unconfirmed > 0 ? "+" : ""}
              {unconfirmed.toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)}{" "}
              <span className="text-sm font-medium" style={{ color: "var(--color-btc-gold)" }}>
                {coinInfo.ticker}
              </span>
            </p>
          </div>
        </div>
      </div>

      {/* Address */}
      <div
        className="btc-glow rounded-xl p-4"
        style={{
          background: "var(--color-btc-card)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <div className="mb-2 flex items-center justify-between">
          <h3
            className="text-xs font-medium uppercase tracking-wider"
            style={{ color: "var(--color-btc-text-dim)" }}
          >
            Default Address
          </h3>
          <button
            onClick={() => navigate("/receive")}
            className="rounded px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide transition-colors"
            style={{
              background: "rgba(247, 147, 26, 0.12)",
              color: "var(--color-btc-gold)",
              border: "1px solid rgba(247, 147, 26, 0.25)",
            }}
          >
            Manage Addresses
          </button>
        </div>
        {addressLabel && (
          <p className="mb-1.5 text-[11px] font-semibold" style={{ color: "var(--color-btc-gold-light)" }}>{addressLabel}</p>
        )}
        <div className="flex items-center gap-2">
          <code
            className="min-w-0 flex-1 break-all rounded-lg px-3 py-2 text-sm font-mono"
            style={{
              background: "var(--color-btc-deep)",
              color: "var(--color-btc-gold-light)",
              border: "1px solid var(--color-btc-border)",
            }}
          >
            {address || "Loading..."}
          </code>
          {address && (
            <button
              onClick={copyAddress}
              className="shrink-0 rounded-lg p-2 transition-colors"
              style={{
                background: addressCopied ? "rgba(63, 185, 80, 0.15)" : "rgba(247, 147, 26, 0.12)",
                color: addressCopied ? "var(--color-btc-green)" : "var(--color-btc-gold)",
                border: `1px solid ${addressCopied ? "rgba(63, 185, 80, 0.3)" : "rgba(247, 147, 26, 0.25)"}`,
              }}
              title={addressCopied ? "Copied!" : "Copy address"}
            >
              {addressCopied ? (
                <svg width={14} height={14} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12" /></svg>
              ) : (
                <svg width={14} height={14} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" /></svg>
              )}
            </button>
          )}
        </div>
        {addressCopied && (
          <p className="mt-1.5 text-[11px] font-medium" style={{ color: "var(--color-btc-green)" }}>Address copied to clipboard</p>
        )}
      </div>

      {/* Chain Status */}
      <div
        className="btc-glow rounded-xl p-4"
        style={{
          background: "var(--color-btc-card)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <h3
          className="mb-2 text-xs font-medium uppercase tracking-wider"
          style={{ color: "var(--color-btc-text-dim)" }}
        >
          Chain Status
        </h3>
        <dl className="grid grid-cols-2 gap-3 text-sm">
          <div>
            <dt style={{ color: "var(--color-btc-text-muted)" }} className="text-xs">
              Block Height
            </dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {height.toLocaleString()}
            </dd>
          </div>
          <div>
            <dt style={{ color: "var(--color-btc-text-muted)" }} className="text-xs">
              Best Block
            </dt>
            <dd
              className="truncate font-mono font-medium"
              style={{ color: "var(--color-btc-text)" }}
              title={bestHash}
            >
              {bestHash ? bestHash.slice(0, 16) + "\u2026" : "\u2014"}
            </dd>
          </div>
        </dl>
      </div>

      {/* Node Configuration */}
      <div
        className="btc-glow rounded-xl p-4"
        style={{
          background: "var(--color-btc-card)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <h3
          className="mb-2.5 text-xs font-medium uppercase tracking-wider"
          style={{ color: "var(--color-btc-text-dim)" }}
        >
          Node Configuration
        </h3>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-1.5 text-xs">
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>P2P Port</dt>
            <dd className="flex items-center gap-1.5">
              <span className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
                {nodeConfig.listenPort || "—"}
              </span>
              {portTestResult != null && (
                <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" strokeWidth={3} strokeLinecap="round" strokeLinejoin="round" stroke={portTestResult.open ? "var(--color-btc-green)" : "var(--color-btc-red)"}>
                  {portTestResult.open
                    ? <polyline points="20 6 9 17 4 12" />
                    : <><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></>}
                </svg>
              )}
              <button
                onClick={handleTestPort}
                disabled={portTesting}
                className="rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide transition-colors disabled:opacity-50"
                style={{
                  background: "rgba(88, 166, 255, 0.12)",
                  color: "var(--color-btc-blue)",
                  border: "1px solid rgba(88, 166, 255, 0.25)",
                }}
              >
                {portTesting ? "..." : "Test"}
              </button>
            </dd>
          </div>
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Port-Forward Active</dt>
            <dd className="font-medium" style={{ color: nodeConfig.reachable ? "var(--color-btc-green)" : "var(--color-btc-red)" }}>
              {nodeConfig.reachable ? "Yes" : "No"}
            </dd>
          </div>
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Max Connections</dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {(nodeConfig.maxInbound ?? 0) + (nodeConfig.maxOutbound ?? 0)}
            </dd>
          </div>
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Disk Usage</dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {nodeConfig.diskUsageMB != null ? `${nodeConfig.diskUsageMB} MB` : "—"}
            </dd>
          </div>
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Banned Peers</dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {nodeConfig.bannedCount ?? 0}
            </dd>
          </div>
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Uptime</dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {formatUptime(nodeConfig.uptime as number)}
            </dd>
          </div>
          <div className="col-span-2 flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Auto-start</dt>
            <dd className="flex items-center gap-2">
              <button
                onClick={handleServiceToggle}
                className="rounded px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide transition-colors"
                style={{
                  background: serviceInstalled ? "rgba(63, 185, 80, 0.15)" : "rgba(248, 81, 73, 0.15)",
                  color: serviceInstalled ? "var(--color-btc-green)" : "var(--color-btc-red)",
                  border: `1px solid ${serviceInstalled ? "rgba(63, 185, 80, 0.3)" : "rgba(248, 81, 73, 0.3)"}`,
                }}
              >
                {serviceInstalled ? "On" : "Off"}
              </button>
              {serviceMsg && (
                <span className="text-[10px]" style={{ color: "var(--color-btc-gold-light)" }}>
                  {serviceMsg}
                </span>
              )}
            </dd>
          </div>
        </dl>
        {/* Data directory row */}
        <div className="mt-2.5 flex items-center gap-2 text-xs">
          <span style={{ color: "var(--color-btc-text-muted)" }}>Data Dir</span>
          <code
            className="min-w-0 flex-1 truncate rounded px-1.5 py-0.5 font-mono"
            style={{
              background: "var(--color-btc-deep)",
              color: "var(--color-btc-text-muted)",
              border: "1px solid var(--color-btc-border)",
              fontSize: "10px",
            }}
            title={nodeConfig.dataDir as string}
          >
            {nodeConfig.dataDir || "—"}
          </code>
          <button
            onClick={() => OpenDataDir().catch(() => {})}
            className="shrink-0 rounded px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide transition-colors"
            style={{
              background: "rgba(88, 166, 255, 0.12)",
              color: "var(--color-btc-blue)",
              border: "1px solid rgba(88, 166, 255, 0.25)",
            }}
          >
            Open
          </button>
        </div>
        {/* Support the Network CTA */}
        {(!serviceInstalled || !nodeConfig.reachable) && (
          <div
            className="mt-2.5 flex items-center gap-2 rounded-lg px-3 py-2"
            style={{
              background: "rgba(247, 147, 26, 0.08)",
              border: "1px solid rgba(247, 147, 26, 0.2)",
            }}
          >
            <svg className="h-4 w-4 shrink-0" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-gold)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10" />
              <line x1="12" y1="16" x2="12" y2="12" />
              <line x1="12" y1="8" x2="12.01" y2="8" />
            </svg>
            <span className="flex-1 text-[11px]" style={{ color: "var(--color-btc-gold-light)" }}>
              Support the network by enabling auto-start and opening your P2P port.
            </span>
            <button
              onClick={() => setShowHelpDialog(true)}
              className="shrink-0 rounded px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide transition-colors"
              style={{
                background: "rgba(247, 147, 26, 0.15)",
                color: "var(--color-btc-gold)",
                border: "1px solid rgba(247, 147, 26, 0.3)",
              }}
            >
              How?
            </button>
          </div>
        )}
      </div>

      {/* Port Forwarding Help Dialog */}
      {showHelpDialog && (
        <PortForwardingDialog
          port={nodeConfig.listenPort as string || "19333"}
          coinName={coinInfo.name}
          serviceInstalled={serviceInstalled}
          onToggleService={handleServiceToggle}
          portTestResult={portTestResult}
          portTesting={portTesting}
          onTestPort={handleTestPort}
          onClose={() => setShowHelpDialog(false)}
        />
      )}

      {/* Network & Sync footer */}
      <div
        className="btc-glow mt-auto flex items-center justify-end gap-5 rounded-xl px-5 py-3"
        style={{
          background: "var(--color-btc-card)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <div className="flex items-center gap-2">
          <NetworkIcon peers={peers} />
          <div className="text-xs">
            <p className="font-medium" style={{ color: "var(--color-btc-text)" }}>
              {peers} peer{peers !== 1 ? "s" : ""}
            </p>
            <p style={{ color: "var(--color-btc-text-dim)" }}>
              {peers >= 8
                ? "Excellent"
                : peers >= 4
                  ? "Good"
                  : peers >= 1
                    ? "Low"
                    : "No connections"}
            </p>
          </div>
        </div>
        <div className="h-6 w-px" style={{ background: "var(--color-btc-border)" }} />
        <div className="flex items-center gap-2">
          <SyncIcon progress={syncProgress} />
          <div className="text-xs">
            <p className="font-medium" style={{ color: "var(--color-btc-text)" }}>
              {synced ? "Synced" : `Syncing ${(syncProgress * 100).toFixed(1)}%`}
            </p>
            <p style={{ color: "var(--color-btc-text-dim)" }}>
              {synced
                ? "Up to date"
                : syncState === "HEADER_SYNC"
                  ? "Downloading headers\u2026"
                  : syncState === "BLOCK_SYNC"
                    ? "Downloading blocks\u2026"
                    : "Connecting\u2026"}
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
