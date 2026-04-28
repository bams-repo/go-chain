import { useState } from "react";

function detectOS(): "linux" | "mac" | "windows" | "unknown" {
  const ua = navigator.userAgent.toLowerCase();
  if (ua.includes("linux")) return "linux";
  if (ua.includes("mac")) return "mac";
  if (ua.includes("win")) return "windows";
  return "unknown";
}

export function PortForwardingDialog({
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

        <div style={{ padding: "16px 20px", overflowY: "auto", flex: 1 }}>
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

          <div style={{ marginBottom: "16px" }}>
            <p style={stepLabelStyle}>Step 2: Open P2P Port {port}</p>

            <p style={{ ...bodyStyle, fontWeight: 600, color: "var(--color-btc-text)", marginTop: "8px", marginBottom: "4px" }}>
              Router Port Forwarding
            </p>
            <ol style={{ ...bodyStyle, paddingLeft: "18px", margin: "0 0 8px 0" }}>
              <li>Open your router admin page (usually <code style={{ fontSize: "11px", color: "var(--color-btc-gold-light)" }}>192.168.1.1</code> or <code style={{ fontSize: "11px", color: "var(--color-btc-gold-light)" }}>192.168.0.1</code>)</li>
              <li>Find <strong>Port Forwarding</strong>, <strong>NAT</strong>, or <strong>Virtual Server</strong> settings</li>
              <li>Add a rule: External Port <strong>{port}</strong> &rarr; Internal Port <strong>{port}</strong>, Protocol: <strong>TCP</strong>, to your computer&apos;s local IP</li>
              <li>Save and apply</li>
            </ol>

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
