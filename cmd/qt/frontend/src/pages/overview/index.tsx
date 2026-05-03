import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import { walletRpc } from "@/lib/walletRpc";
import {
  GetBalance,
  GetWalletAddress,
  GetBlockchainInfo,
  GetPeerCount,
  GetSyncStatus,
  GetUpdateStatus,
  GetAddressLabel,
} from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime/runtime";

let updateBannerDismissed = false;

function formatNetworkHashRate(hps: number): string {
  if (!Number.isFinite(hps) || hps < 0) return "—";
  if (hps >= 1e18) return `${(hps / 1e18).toFixed(2)} EH/s`;
  if (hps >= 1e15) return `${(hps / 1e15).toFixed(2)} PH/s`;
  if (hps >= 1e12) return `${(hps / 1e12).toFixed(2)} TH/s`;
  if (hps >= 1e9) return `${(hps / 1e9).toFixed(2)} GH/s`;
  if (hps >= 1e6) return `${(hps / 1e6).toFixed(2)} MH/s`;
  if (hps >= 1e3) return `${(hps / 1e3).toFixed(2)} kH/s`;
  return `${hps.toFixed(1)} H/s`;
}

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
  const [updateDismissed, setUpdateDismissed] = useState(updateBannerDismissed);
  const [protocolOutdated, setProtocolOutdated] = useState(false);
  const [networkVersion, setNetworkVersion] = useState("");
  const [networkHashPS, setNetworkHashPS] = useState<number | null>(null);

  const copyAddress = useCallback(() => {
    if (!address) return;
    navigator.clipboard.writeText(address).then(() => {
      setAddressCopied(true);
      setTimeout(() => setAddressCopied(false), 2000);
    });
  }, [address]);
  const [releasesURL, setReleasesURL] = useState("");

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
      walletRpc<number>("getnetworkhashps", [120, -1])
        .then((v) => {
          if (typeof v === "number") setNetworkHashPS(v);
          else setNetworkHashPS(null);
        })
        .catch(() => setNetworkHashPS(null));
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

  const synced = syncState === "SYNCED";

  return (
    <div className="flex h-full flex-col gap-3">
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

      {updateAvailable && !protocolOutdated && !updateDismissed && (
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
          <button
            type="button"
            onClick={() => { updateBannerDismissed = true; setUpdateDismissed(true); }}
            className="flex-shrink-0 rounded-md p-1 transition-opacity hover:opacity-80"
            style={{ background: "rgba(255,255,255,0.12)" }}
            aria-label="Dismiss update banner"
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="#fef2f2" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>
      )}

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
          <div className="col-span-2">
            <dt style={{ color: "var(--color-btc-text-muted)" }} className="text-xs">
              Est. network hashrate (120 blocks)
            </dt>
            <dd className="font-mono font-medium" style={{ color: "var(--color-btc-text)" }}>
              {networkHashPS == null ? "—" : formatNetworkHashRate(networkHashPS)}
            </dd>
          </div>
        </dl>
      </div>

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
