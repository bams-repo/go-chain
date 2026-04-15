import { useEffect, useMemo, useState } from "react";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import { ListTransactions } from "../../../wailsjs/go/main/App";
import type { WalletTransaction } from "@/lib/types";

type FilterTab = "all" | "immature" | "confirmed";

function categoryLabel(cat: string): string {
  if (cat === "generate") return "Mined";
  if (cat === "immature") return "Immature";
  return "Received";
}

function categoryColor(cat: string): string {
  if (cat === "generate") return "var(--color-btc-green)";
  if (cat === "immature") return "var(--color-btc-gold)";
  return "var(--color-btc-blue)";
}

function MaturityBar({ progress, confirmations, target }: { progress: number; confirmations: number; target: number }) {
  const pct = Math.min(progress * 100, 100);
  const mature = confirmations >= target;

  return (
    <div className="flex items-center gap-2">
      <div
        className="h-1.5 flex-1 overflow-hidden rounded-full"
        style={{ background: "var(--color-btc-deep)", minWidth: 60 }}
      >
        <div
          className="h-full rounded-full transition-all duration-500"
          style={{
            width: `${pct}%`,
            background: mature
              ? "var(--color-btc-green)"
              : `linear-gradient(90deg, var(--color-btc-gold) 0%, var(--color-btc-gold-light) 100%)`,
          }}
        />
      </div>
      <span
        className="text-[10px] font-mono tabular-nums"
        style={{ color: mature ? "var(--color-btc-green)" : "var(--color-btc-text-muted)", minWidth: "4.5ch", textAlign: "right" }}
      >
        {confirmations}/{target}
      </span>
    </div>
  );
}

export function Transactions() {
  const coinInfo = useCoinInfo();
  const [txs, setTxs] = useState<WalletTransaction[]>([]);
  const [filter, setFilter] = useState<FilterTab>("all");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const poll = () => {
      ListTransactions()
        .then((raw) => {
          const parsed: WalletTransaction[] = (raw || []).map((r) => ({
            txid: String(r.txid || ""),
            vout: Number(r.vout || 0),
            address: String(r.address || ""),
            category: r.category as WalletTransaction["category"],
            amount: Number(r.amount || 0),
            confirmations: Number(r.confirmations || 0),
            blockheight: Number(r.blockheight || 0),
            isCoinbase: !!r.isCoinbase,
            maturityProgress: Number(r.maturityProgress || 0),
            maturityTarget: Number(r.maturityTarget || 0),
          }));
          setTxs(parsed);
          setLoading(false);
        })
        .catch(() => setLoading(false));
    };
    poll();
    const id = setInterval(poll, 5000);
    return () => clearInterval(id);
  }, []);

  const filtered = useMemo(() => {
    if (filter === "all") return txs;
    if (filter === "immature") return txs.filter((t) => t.category === "immature");
    return txs.filter((t) => t.category !== "immature");
  }, [txs, filter]);

  const immatureCount = useMemo(() => txs.filter((t) => t.category === "immature").length, [txs]);
  const confirmedCount = useMemo(() => txs.filter((t) => t.category !== "immature").length, [txs]);

  const tabs: { key: FilterTab; label: string; count: number }[] = [
    { key: "all", label: "All", count: txs.length },
    { key: "immature", label: "Immature", count: immatureCount },
    { key: "confirmed", label: "Confirmed", count: confirmedCount },
  ];

  return (
    <div className="flex h-full flex-col gap-4">
      {/* Filter tabs */}
      <div className="flex items-center gap-1">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setFilter(tab.key)}
            className="rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors"
            style={{
              background: filter === tab.key ? "rgba(247, 147, 26, 0.12)" : "transparent",
              color: filter === tab.key ? "var(--color-btc-gold)" : "var(--color-btc-text-muted)",
              border: filter === tab.key ? "1px solid rgba(247, 147, 26, 0.25)" : "1px solid transparent",
            }}
          >
            {tab.label}
            <span
              className="ml-1.5 font-mono text-[10px]"
              style={{ opacity: 0.7 }}
            >
              {tab.count}
            </span>
          </button>
        ))}
      </div>

      {/* Transaction list */}
      <div
        className="btc-glow flex-1 overflow-hidden rounded-xl"
        style={{
          background: "var(--color-btc-card)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        {loading ? (
          <div
            className="flex h-full items-center justify-center text-sm"
            style={{ color: "var(--color-btc-text-muted)" }}
          >
            Loading transactions...
          </div>
        ) : filtered.length === 0 ? (
          <div
            className="flex h-full items-center justify-center text-sm"
            style={{ color: "var(--color-btc-text-muted)" }}
          >
            No transactions found.
          </div>
        ) : (
          <div className="h-full overflow-y-auto">
            {/* Header */}
            <div
              className="sticky top-0 z-10 grid gap-3 px-4 py-2.5 text-[10px] font-semibold uppercase tracking-wider"
              style={{
                gridTemplateColumns: "1fr 100px 120px 90px 120px",
                color: "var(--color-btc-text-dim)",
                background: "var(--color-btc-surface)",
                borderBottom: "1px solid var(--color-btc-border)",
              }}
            >
              <span>Transaction</span>
              <span className="text-right">Amount</span>
              <span className="text-right">Confirmations</span>
              <span>Status</span>
              <span>Maturity</span>
            </div>

            {/* Rows */}
            {filtered.map((tx) => (
              <div
                key={`${tx.txid}-${tx.vout}`}
                className="grid items-center gap-3 px-4 py-3 transition-colors hover:brightness-110"
                style={{
                  gridTemplateColumns: "1fr 100px 120px 90px 120px",
                  borderBottom: "1px solid var(--color-btc-border)",
                }}
              >
                {/* Tx info */}
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <div
                      className="h-2 w-2 shrink-0 rounded-full"
                      style={{ background: categoryColor(tx.category) }}
                    />
                    <span
                      className="truncate font-mono text-xs"
                      style={{ color: "var(--color-btc-text)" }}
                      title={tx.txid}
                    >
                      {tx.txid.slice(0, 12)}&hellip;{tx.txid.slice(-8)}
                    </span>
                  </div>
                  <div
                    className="mt-0.5 truncate pl-4 text-[10px]"
                    style={{ color: "var(--color-btc-text-muted)" }}
                    title={tx.address}
                  >
                    {tx.address}
                  </div>
                </div>

                {/* Amount */}
                <div className="text-right">
                  <span
                    className="font-mono text-xs font-bold tabular-nums"
                    style={{ color: "var(--color-btc-text)" }}
                  >
                    {tx.amount.toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)}
                  </span>
                  <span
                    className="ml-1 text-[10px]"
                    style={{ color: "var(--color-btc-gold)" }}
                  >
                    {coinInfo.ticker}
                  </span>
                </div>

                {/* Confirmations */}
                <div
                  className="text-right font-mono text-xs tabular-nums"
                  style={{
                    color: tx.confirmations >= tx.maturityTarget
                      ? "var(--color-btc-green)"
                      : "var(--color-btc-text-muted)",
                  }}
                >
                  {tx.confirmations.toLocaleString()}
                </div>

                {/* Status badge */}
                <div>
                  <span
                    className="inline-block rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
                    style={{
                      background:
                        tx.category === "generate"
                          ? "rgba(63, 185, 80, 0.12)"
                          : tx.category === "immature"
                            ? "rgba(247, 147, 26, 0.12)"
                            : "rgba(88, 166, 255, 0.12)",
                      color: categoryColor(tx.category),
                      border: `1px solid ${
                        tx.category === "generate"
                          ? "rgba(63, 185, 80, 0.25)"
                          : tx.category === "immature"
                            ? "rgba(247, 147, 26, 0.25)"
                            : "rgba(88, 166, 255, 0.25)"
                      }`,
                    }}
                  >
                    {categoryLabel(tx.category)}
                  </span>
                </div>

                {/* Maturity progress */}
                <div>
                  {tx.isCoinbase ? (
                    <MaturityBar
                      progress={tx.maturityProgress}
                      confirmations={tx.confirmations}
                      target={tx.maturityTarget}
                    />
                  ) : (
                    <span
                      className="text-[10px]"
                      style={{ color: "var(--color-btc-text-dim)" }}
                    >
                      &mdash;
                    </span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
