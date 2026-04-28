import { useCallback, useEffect, useState } from "react";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import { PortForwardingDialog } from "@/components/PortForwardingDialog";
import {
  GetNodeConfig,
  OpenDataDir,
  InstallService,
  UninstallService,
  IsServiceInstalled,
  TestPort,
} from "../../../wailsjs/go/main/App";

function formatUptime(seconds: number): string {
  if (!seconds || seconds < 0) return "—";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

export function NodeNetwork() {
  const coinInfo = useCoinInfo();
  const [nodeConfig, setNodeConfig] = useState<Record<string, unknown>>({});
  const [serviceInstalled, setServiceInstalled] = useState(false);
  const [serviceMsg, setServiceMsg] = useState("");
  const [portTestResult, setPortTestResult] = useState<null | { open: boolean; publicIP: string }>(null);
  const [portTesting, setPortTesting] = useState(false);
  const [showHelpDialog, setShowHelpDialog] = useState(false);

  useEffect(() => {
    const poll = () => {
      GetNodeConfig().then(setNodeConfig).catch(() => {});
      IsServiceInstalled().then(setServiceInstalled).catch(() => {});
    };
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, []);

  const handleServiceToggle = useCallback(() => {
    const action = serviceInstalled ? UninstallService() : InstallService();
    action
      .then((msg) => {
        setServiceMsg(msg);
        setServiceInstalled(!serviceInstalled);
        setTimeout(() => setServiceMsg(""), 4000);
      })
      .catch((err: Error) => {
        setServiceMsg("Error: " + err);
        setTimeout(() => setServiceMsg(""), 4000);
      });
  }, [serviceInstalled]);

  const handleTestPort = useCallback(() => {
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
  }, []);

  return (
    <div className="flex h-full flex-col gap-4">
      <div
        className="rounded-xl p-4"
        style={{
          background: "var(--color-btc-card)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <h2 className="text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
          Node &amp; network
        </h2>
        <p className="mt-1.5 text-xs leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
          Run a healthy node: listening port, reachability, disk use, and auto-start. Explorer-style maps and
          advanced telemetry stay on the roadmap; this page focuses on how your wallet participates in the P2P
          network today.
        </p>
      </div>

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
          Node configuration
        </h3>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-1.5 text-xs">
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>P2P port</dt>
            <dd className="flex items-center gap-1.5">
              <span className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
                {(nodeConfig.listenPort as string) || "—"}
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
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Port-forward active</dt>
            <dd className="font-medium" style={{ color: nodeConfig.reachable ? "var(--color-btc-green)" : "var(--color-btc-red)" }}>
              {nodeConfig.reachable ? "Yes" : "No"}
            </dd>
          </div>
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Max connections</dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {((nodeConfig.maxInbound as number) ?? 0) + ((nodeConfig.maxOutbound as number) ?? 0)}
            </dd>
          </div>
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Disk usage</dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {nodeConfig.diskUsageMB != null ? `${nodeConfig.diskUsageMB} MB` : "—"}
            </dd>
          </div>
          <div className="flex items-center justify-between">
            <dt style={{ color: "var(--color-btc-text-muted)" }}>Banned peers</dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {(nodeConfig.bannedCount as number) ?? 0}
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
        <div className="mt-2.5 flex items-center gap-2 text-xs">
          <span style={{ color: "var(--color-btc-text-muted)" }}>Data dir</span>
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
            {(nodeConfig.dataDir as string) || "—"}
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

      {showHelpDialog && (
        <PortForwardingDialog
          port={(nodeConfig.listenPort as string) || "19333"}
          coinName={coinInfo.name}
          serviceInstalled={serviceInstalled}
          onToggleService={handleServiceToggle}
          portTestResult={portTestResult}
          portTesting={portTesting}
          onTestPort={handleTestPort}
          onClose={() => setShowHelpDialog(false)}
        />
      )}
    </div>
  );
}
