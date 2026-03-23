import { Outlet, useLocation } from "react-router-dom";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import type { CoinInfo } from "@/lib/types";
import { Navbar } from "./Navbar";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";

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

function viewMeta(pathname: string): { title: string; subtitle: string } {
  const p = pathname.replace(/\/$/, "") || "/";
  if (p === "" || p === "/") {
    return { title: "Overview", subtitle: "Balances, default address, and chain status" };
  }
  if (p === "/social" || p.startsWith("/social/")) {
    return { title: "Social", subtitle: "Wallet IRC — community channel" };
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
