import { useEffect, useState } from "react";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import {
  GetBalance,
  GetWalletAddress,
  GetBlockchainInfo,
  GetPeerCount,
  GetSyncProgress,
} from "../../../wailsjs/go/main/App";

function NetworkIcon({ peers }: { peers: number }) {
  const bars = peers >= 8 ? 4 : peers >= 4 ? 3 : peers >= 1 ? 2 : peers > 0 ? 1 : 0;
  const gold = "var(--color-btc-gold)";
  const dim = "var(--color-btc-text-dim)";
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" strokeWidth={2.5} strokeLinecap="round">
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
      <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-green)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
        <path d="M22 11.08V12a10 10 0 11-5.93-9.14" />
        <polyline points="22 4 12 14.01 9 11.01" />
      </svg>
    );
  }
  return (
    <svg className="h-5 w-5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-gold)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="M21 12a9 9 0 11-6.219-8.56" />
    </svg>
  );
}

export function Overview() {
  const coinInfo = useCoinInfo();
  const [confirmed, setConfirmed] = useState(0);
  const [unconfirmed, setUnconfirmed] = useState(0);
  const [address, setAddress] = useState("");
  const [height, setHeight] = useState(0);
  const [bestHash, setBestHash] = useState("");
  const [peers, setPeers] = useState(0);
  const [syncProgress, setSyncProgress] = useState(1);

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
      GetSyncProgress().then(setSyncProgress);
    };
    GetWalletAddress().then(setAddress);
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, []);

  const synced = syncProgress >= 0.999;

  return (
    <div className="flex h-full flex-col gap-4">
      {/* Balance */}
      <div
        className="btc-noise btc-glow-active relative overflow-hidden rounded-xl p-6"
        style={{
          background: "linear-gradient(135deg, var(--color-btc-card) 0%, var(--color-btc-surface) 100%)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <div className="absolute -right-8 -top-8 h-32 w-32 rounded-full opacity-[0.04]" style={{ background: "var(--color-btc-gold)" }} />
        <div className="relative z-10">
          <h3 className="mb-1 text-xs font-medium uppercase tracking-wider" style={{ color: "var(--color-btc-text-dim)" }}>
            Balance
          </h3>
          <p className="text-3xl font-bold" style={{ color: "var(--color-btc-text)" }}>
            {confirmed.toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)}{" "}
            <span className="text-lg font-medium" style={{ color: "var(--color-btc-gold)" }}>
              {coinInfo.ticker}
            </span>
          </p>
          {unconfirmed > 0 && (
            <p className="mt-1.5 text-sm" style={{ color: "var(--color-btc-gold-light)" }}>
              +{unconfirmed.toFixed(4)} {coinInfo.ticker} unconfirmed
            </p>
          )}
        </div>
      </div>

      {/* Address */}
      <div
        className="btc-glow rounded-xl p-5"
        style={{
          background: "var(--color-btc-card)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <h3 className="mb-2 text-xs font-medium uppercase tracking-wider" style={{ color: "var(--color-btc-text-dim)" }}>
          Default Address
        </h3>
        <code
          className="block break-all rounded-lg px-3 py-2.5 text-sm font-mono"
          style={{
            background: "var(--color-btc-deep)",
            color: "var(--color-btc-gold-light)",
            border: "1px solid var(--color-btc-border)",
          }}
        >
          {address || "Loading..."}
        </code>
      </div>

      {/* Chain Status */}
      <div
        className="btc-glow rounded-xl p-5"
        style={{
          background: "var(--color-btc-card)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <h3 className="mb-3 text-xs font-medium uppercase tracking-wider" style={{ color: "var(--color-btc-text-dim)" }}>
          Chain Status
        </h3>
        <dl className="grid grid-cols-2 gap-4 text-sm">
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
            <dd className="truncate font-mono font-medium" style={{ color: "var(--color-btc-text)" }} title={bestHash}>
              {bestHash ? bestHash.slice(0, 16) + "\u2026" : "\u2014"}
            </dd>
          </div>
        </dl>
      </div>

      <div className="flex-1" />

      {/* Network & Sync footer */}
      <div
        className="btc-glow flex items-center justify-end gap-5 rounded-xl px-5 py-3"
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
              {peers >= 8 ? "Excellent" : peers >= 4 ? "Good" : peers >= 1 ? "Low" : "No connections"}
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
            <p style={{ color: "var(--color-btc-text-dim)" }}>{synced ? "Up to date" : "Downloading blocks\u2026"}</p>
          </div>
        </div>
      </div>
    </div>
  );
}
