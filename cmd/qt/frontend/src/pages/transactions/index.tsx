import { useEffect, useMemo, useState } from "react";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import { ListTransactions, GetAddressBook } from "../../../wailsjs/go/main/App";
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

function SearchIcon({ size = 14 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <circle cx="11" cy="11" r="8" />
      <line x1="21" y1="21" x2="16.65" y2="16.65" />
    </svg>
  );
}

function CopyIcon({ size = 12 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
    </svg>
  );
}

function MaturityBar({ progress, confirmations, target }: { progress: number; confirmations: number; target: number }) {
  const pct = Math.min(progress * 100, 100);
  const mature = confirmations >= target;
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 flex-1 overflow-hidden rounded-full" style={{ background: "var(--color-btc-deep)", minWidth: 60 }}>
        <div
          className="h-full rounded-full transition-all duration-500"
          style={{
            width: `${pct}%`,
            background: mature ? "var(--color-btc-green)" : "linear-gradient(90deg, var(--color-btc-gold) 0%, var(--color-btc-gold-light) 100%)",
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
  const [search, setSearch] = useState("");
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [copiedTxid, setCopiedTxid] = useState<string | null>(null);

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
      GetAddressBook().then((book) => { if (book) setLabels(book); }).catch(() => {});
    };
    poll();
    const id = setInterval(poll, 5000);
    return () => clearInterval(id);
  }, []);

  const copyTxid = (txid: string) => {
    navigator.clipboard.writeText(txid).then(() => {
      setCopiedTxid(txid);
      setTimeout(() => setCopiedTxid(null), 2000);
    });
  };

  const byCategory = useMemo(() => {
    if (filter === "all") return txs;
    if (filter === "immature") return txs.filter((t) => t.category === "immature");
    return txs.filter((t) => t.category !== "immature");
  }, [txs, filter]);

  const filtered = useMemo(() => {
    if (!search.trim()) return byCategory;
    const q = search.toLowerCase();
    return byCategory.filter((tx) => {
      if (tx.txid.toLowerCase().includes(q)) return true;
      if (tx.address.toLowerCase().includes(q)) return true;
      const label = labels[tx.address] || "";
      if (label.toLowerCase().includes(q)) return true;
      const amtStr = tx.amount.toString();
      if (amtStr.includes(q)) return true;
      const catLabel = categoryLabel(tx.category).toLowerCase();
      if (catLabel.includes(q)) return true;
      return false;
    });
  }, [byCategory, search, labels]);

  const immatureCount = useMemo(() => txs.filter((t) => t.category === "immature").length, [txs]);
  const confirmedCount = useMemo(() => txs.filter((t) => t.category !== "immature").length, [txs]);

  const tabs: { key: FilterTab; label: string; count: number }[] = [
    { key: "all", label: "All", count: txs.length },
    { key: "immature", label: "Immature", count: immatureCount },
    { key: "confirmed", label: "Confirmed", count: confirmedCount },
  ];

  return (
    <div className="flex h-full flex-col gap-4">
      {/* Toolbar: filter tabs + search */}
      <div className="flex items-center gap-3">
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
              <span className="ml-1.5 font-mono text-[10px]" style={{ opacity: 0.7 }}>{tab.count}</span>
            </button>
          ))}
        </div>

        <div className="ml-auto flex items-center gap-1.5 rounded-lg px-2.5 py-1.5" style={{ background: "var(--color-btc-deep)", border: "1px solid var(--color-btc-border)", minWidth: 200, maxWidth: 320 }}>
          <SearchIcon size={13} />
          <input
            type="text"
            placeholder="Search txid, address, amount, label..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            spellCheck={false}
            className="flex-1 bg-transparent text-[11px] text-[var(--color-btc-text)] outline-none placeholder:text-[var(--color-btc-text-dim)]"
          />
          {search && (
            <button onClick={() => setSearch("")} className="text-[var(--color-btc-text-dim)] hover:text-[var(--color-btc-text)]" style={{ fontSize: "14px", lineHeight: 1 }}>
              &times;
            </button>
          )}
        </div>
      </div>

      {/* Results summary when searching */}
      {search.trim() && (
        <p className="text-[11px]" style={{ color: "var(--color-btc-text-muted)" }}>
          {filtered.length} result{filtered.length !== 1 ? "s" : ""} for &ldquo;{search}&rdquo;
        </p>
      )}

      {/* Transaction list */}
      <div
        className="btc-glow flex-1 overflow-hidden rounded-xl"
        style={{ background: "var(--color-btc-card)", border: "1px solid var(--color-btc-border)" }}
      >
        {loading ? (
          <div className="flex h-full items-center justify-center text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
            Loading transactions...
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
            {search ? "No transactions match your search." : "No transactions found."}
          </div>
        ) : (
          <div className="h-full overflow-y-auto">
            {/* Header */}
            <div
              className="sticky top-0 z-10 grid gap-3 px-4 py-2.5 text-[10px] font-semibold uppercase tracking-wider"
              style={{
                gridTemplateColumns: "1fr 100px 100px 90px 120px",
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
            {filtered.map((tx) => {
              const label = labels[tx.address] || "";
              return (
                <div
                  key={`${tx.txid}-${tx.vout}`}
                  className="group grid items-center gap-3 px-4 py-3 transition-colors hover:brightness-110"
                  style={{
                    gridTemplateColumns: "1fr 100px 100px 90px 120px",
                    borderBottom: "1px solid var(--color-btc-border)",
                  }}
                >
                  {/* Tx info */}
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <div className="h-2 w-2 shrink-0 rounded-full" style={{ background: categoryColor(tx.category) }} />
                      <span className="truncate font-mono text-xs" style={{ color: "var(--color-btc-text)" }} title={tx.txid}>
                        {tx.txid.slice(0, 12)}&hellip;{tx.txid.slice(-8)}
                      </span>
                      <button
                        onClick={() => copyTxid(tx.txid)}
                        className="shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
                        style={{ color: copiedTxid === tx.txid ? "var(--color-btc-green)" : "var(--color-btc-text-dim)" }}
                        title="Copy txid"
                      >
                        <CopyIcon size={11} />
                      </button>
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 pl-4">
                      <span className="truncate text-[10px]" style={{ color: "var(--color-btc-text-muted)" }} title={tx.address}>
                        {tx.address}
                      </span>
                      {label && (
                        <span className="shrink-0 rounded px-1 py-px text-[9px] font-semibold" style={{ background: "rgba(247, 147, 26, 0.08)", color: "var(--color-btc-gold-light)", border: "1px solid rgba(247, 147, 26, 0.15)" }}>
                          {label}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Amount */}
                  <div className="text-right">
                    <span className="font-mono text-xs font-bold tabular-nums" style={{ color: "var(--color-btc-text)" }}>
                      {tx.amount.toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)}
                    </span>
                    <span className="ml-1 text-[10px]" style={{ color: "var(--color-btc-gold)" }}>{coinInfo.ticker}</span>
                  </div>

                  {/* Confirmations */}
                  <div
                    className="text-right font-mono text-xs tabular-nums"
                    style={{ color: tx.confirmations >= tx.maturityTarget ? "var(--color-btc-green)" : "var(--color-btc-text-muted)" }}
                  >
                    {tx.confirmations.toLocaleString()}
                  </div>

                  {/* Status */}
                  <div>
                    <span
                      className="inline-block rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
                      style={{
                        background: tx.category === "generate" ? "rgba(63, 185, 80, 0.12)" : tx.category === "immature" ? "rgba(247, 147, 26, 0.12)" : "rgba(88, 166, 255, 0.12)",
                        color: categoryColor(tx.category),
                        border: `1px solid ${tx.category === "generate" ? "rgba(63, 185, 80, 0.25)" : tx.category === "immature" ? "rgba(247, 147, 26, 0.25)" : "rgba(88, 166, 255, 0.25)"}`,
                      }}
                    >
                      {categoryLabel(tx.category)}
                    </span>
                  </div>

                  {/* Maturity */}
                  <div>
                    {tx.isCoinbase ? (
                      <MaturityBar progress={tx.maturityProgress} confirmations={tx.confirmations} target={tx.maturityTarget} />
                    ) : (
                      <span className="text-[10px]" style={{ color: "var(--color-btc-text-dim)" }}>&mdash;</span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
