import { useEffect, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import type { CoinInfo } from "@/lib/types";
import { Navbar } from "./Navbar";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { GetMainnetLaunchInfo } from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime/runtime";

function NetworkPill({ network }: { network: CoinInfo["network"] }) {
  const label = network === "mainnet" ? "Mainnet" : network === "testnet" ? "Testnet" : "Regtest";
  const isMainnet = network === "mainnet";
  return (
    <span
      className="shrink-0 rounded-md border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em]"
      style={
        isMainnet
          ? {
              borderColor: "var(--color-btc-border)",
              color: "var(--color-btc-text-muted)",
              background: "var(--color-btc-deep)",
            }
          : network === "regtest"
            ? {
                borderColor: "rgba(180, 80, 80, 0.4)",
                color: "rgb(248, 190, 190)",
                background: "rgba(90, 40, 40, 0.25)",
              }
            : {
                borderColor: "rgba(100, 140, 200, 0.4)",
                color: "rgb(190, 210, 248)",
                background: "rgba(45, 65, 110, 0.3)",
              }
      }
      title={`Chain network: ${label}`}
    >
      {label}
    </span>
  );
}

function MainnetRestartModal({ coinName }: { coinName: string }) {
  return (
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center"
      style={{ background: "rgba(0, 0, 0, 0.75)", backdropFilter: "blur(4px)" }}
    >
      <div
        className="mx-4 max-w-md rounded-xl border p-6 shadow-2xl"
        style={{
          background: "var(--color-btc-card)",
          borderColor: "var(--color-btc-gold)",
        }}
      >
        <div className="mb-4 flex items-center gap-3">
          <div
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full"
            style={{ background: "rgba(247, 147, 26, 0.15)" }}
          >
            <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-gold)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10" />
              <line x1="12" y1="8" x2="12" y2="12" />
              <line x1="12" y1="16" x2="12.01" y2="16" />
            </svg>
          </div>
          <h2
            className="text-lg font-bold tracking-tight"
            style={{ color: "var(--color-btc-gold)" }}
          >
            Mainnet Is Live!
          </h2>
        </div>
        <p className="mb-3 text-sm leading-relaxed" style={{ color: "var(--color-btc-text)" }}>
          The {coinName} mainnet has activated while this wallet was running on testnet.
        </p>
        <p className="mb-5 text-sm leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
          Please close and restart your wallet to switch to mainnet. Your testnet data will remain safe.
        </p>
        <div
          className="rounded-lg border px-4 py-3 text-center text-xs font-medium"
          style={{
            borderColor: "var(--color-btc-border)",
            color: "var(--color-btc-text-muted)",
            background: "var(--color-btc-deep)",
          }}
        >
          Close this wallet and reopen it to join mainnet
        </div>
      </div>
    </div>
  );
}

function MainnetCountdown() {
  const [launchEpoch, setLaunchEpoch] = useState(0);
  const [countdown, setCountdown] = useState({ days: 0, hours: 0, minutes: 0, seconds: 0 });
  const [launched, setLaunched] = useState(false);
  const [showRestart, setShowRestart] = useState(false);
  const [startedOnTestnet, setStartedOnTestnet] = useState(false);
  const coinInfo = useCoinInfo();

  useEffect(() => {
    GetMainnetLaunchInfo()
      .then((info) => {
        if (info.miningStartTime) setLaunchEpoch(info.miningStartTime as number);
        if (info.network && info.network !== "mainnet") setStartedOnTestnet(true);
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    return EventsOn("mainnet:activated", () => {
      setShowRestart(true);
    });
  }, []);

  useEffect(() => {
    if (!launchEpoch) return;
    const tick = () => {
      const now = Math.floor(Date.now() / 1000);
      const diff = launchEpoch - now;
      if (diff <= 0) {
        setLaunched(true);
        setCountdown({ days: 0, hours: 0, minutes: 0, seconds: 0 });
        if (startedOnTestnet) setShowRestart(true);
        return;
      }
      setLaunched(false);
      setCountdown({
        days: Math.floor(diff / 86400),
        hours: Math.floor((diff % 86400) / 3600),
        minutes: Math.floor((diff % 3600) / 60),
        seconds: diff % 60,
      });
    };
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [launchEpoch, startedOnTestnet]);

  if (!launchEpoch) return null;

  if (launched) {
    return (
      <>
        {showRestart && <MainnetRestartModal coinName={coinInfo.name} />}
        <div className="flex items-center gap-1.5">
          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-green)" strokeWidth={2.5} strokeLinecap="round" strokeLinejoin="round">
            <path d="M22 11.08V12a10 10 0 11-5.93-9.14" />
            <polyline points="22 4 12 14.01 9 11.01" />
          </svg>
          <span className="text-[11px] font-semibold" style={{ color: "var(--color-btc-green)" }}>
            Mainnet Live
          </span>
        </div>
      </>
    );
  }

  const units = [
    { value: countdown.days, label: "d" },
    { value: countdown.hours, label: "h" },
    { value: countdown.minutes, label: "m" },
    { value: countdown.seconds, label: "s" },
  ];

  return (
    <div className="flex items-center gap-2.5">
      <span
        className="text-[10px] font-medium uppercase tracking-wider"
        style={{ color: "var(--color-btc-text-dim)" }}
      >
        Mainnet
      </span>
      <div className="flex items-baseline gap-1">
        {units.map((u) => (
          <span key={u.label} className="font-mono text-xs font-bold tabular-nums" style={{ color: "var(--color-btc-gold)" }}>
            {String(u.value).padStart(2, "0")}
            <span className="text-[10px] font-bold" style={{ color: "var(--color-btc-gold-light)" }}>{u.label}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

function viewMeta(pathname: string): { title: string; subtitle: string } {
  const p = pathname.replace(/\/$/, "") || "/";
  if (p === "" || p === "/") {
    return { title: "Overview", subtitle: "Balances, default address, and chain status" };
  }
  if (p === "/social" || p.startsWith("/social/")) {
    return { title: "Social", subtitle: "Wallet IRC — community channel" };
  }
  if (p === "/node-map" || p.startsWith("/node-map/")) {
    return { title: "Node Map", subtitle: "View the node map for the Fairchain network" };
  }
  if (p === "/transactions" || p.startsWith("/transactions/")) {
    return { title: "Transactions", subtitle: "Wallet transaction history & maturity" };
  }
  if (p === "/mining" || p.startsWith("/mining/")) {
    return { title: "Mining", subtitle: "Internal miner & stratum server" };
  }
  if (p === "/receive" || p.startsWith("/receive/")) {
    return { title: "Receive", subtitle: "Generate & manage receiving addresses" };
  }
  if (p === "/send" || p.startsWith("/send/")) {
    return { title: "Send", subtitle: "Send coins to another address" };
  }
  return { title: "Wallet", subtitle: "Fairchain" };
}

export default function MainLayout() {
  const { pathname } = useLocation();
  const { title, subtitle } = viewMeta(pathname);
  const coinInfo = useCoinInfo();

  return (
    <SidebarProvider className="flex h-full min-h-0! w-full flex-col">
      <div className="flex min-h-0 flex-1 flex-row overflow-hidden">
        <Navbar />
        <SidebarInset className="min-h-0 flex-1 overflow-hidden border-0 bg-transparent md:peer-data-[variant=inset]:m-0 md:peer-data-[variant=inset]:shadow-none">
          <div
            className="flex min-h-0 flex-1 flex-col overflow-hidden"
            style={{ background: "var(--color-btc-deep)" }}
          >
            <div
              className="flex shrink-0 items-center gap-2 border-b px-3 py-2.5 pl-2 md:gap-3 md:px-5 md:pl-3"
              style={{
                borderColor: "var(--color-btc-border)",
                background: "var(--color-btc-card)",
              }}
            >
              <SidebarTrigger
                className="shrink-0"
                style={{ color: "var(--color-btc-text-muted)" }}
              />
              <div className="min-w-0 flex-1">
                <h1
                  className="text-[15px] font-semibold leading-tight tracking-tight"
                  style={{ color: "var(--color-btc-text)" }}
                >
                  {title}
                </h1>
                <p
                  className="mt-0.5 text-[11px] leading-snug"
                  style={{ color: "var(--color-btc-text-muted)" }}
                >
                  {subtitle}
                </p>
              </div>
              <MainnetCountdown />
              <NetworkPill network={coinInfo.network} />
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain p-5 md:p-6">
              <Outlet />
            </div>
          </div>
        </SidebarInset>
      </div>
    </SidebarProvider>
  );
}
