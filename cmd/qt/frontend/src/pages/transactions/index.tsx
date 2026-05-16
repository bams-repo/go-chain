import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import { ListTransactions, GetAddressBook } from "../../../wailsjs/go/main/App";
import type { WalletTransaction } from "@/lib/types";
import { ExplorerLink, shortHash } from "@/pages/explorer/shared";

type FilterTab = "all" | "immature" | "received" | "sent";

function categoryLabel(cat: string): string {
  if (cat === "generate") return "Mined";
  if (cat === "immature") return "Immature";
  if (cat === "send") return "Sent";
  return "Received";
}

function categoryColor(cat: string): string {
  if (cat === "generate") return "var(--color-btc-green)";
  if (cat === "immature") return "var(--color-btc-gold)";
  if (cat === "send") return "var(--color-btc-red)";
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

function RefreshIcon({ size = 14, spinning }: { size?: number; spinning?: boolean }) {
  return (
    <svg
      className={spinning ? "animate-spin" : undefined}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M23 4v6h-6" />
      <path d="M20.49 15a9 9 0 11-2.12-9.36L23 10" />
    </svg>
  );
}

function ListIllustration() {
  return (
    <svg className="mb-3 h-12 w-12 opacity-40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.25} strokeLinecap="round" strokeLinejoin="round">
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
      <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" />
    </svg>
  );
}

function MaturityBadge({ status, progress, confirmations, target }: {
  status: string; progress: number; confirmations: number; target: number;
}) {
  if (status === "mempool") {
    return (
      <span
        className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
        style={{ background: "rgba(247, 147, 26, 0.12)", color: "var(--color-btc-gold)", border: "1px solid rgba(247, 147, 26, 0.25)" }}
      >
        <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full" style={{ background: "var(--color-btc-gold)" }} />
        Mempool
      </span>
    );
  }

  if (status === "unverified") {
    const pct = Math.min(progress * 100, 100);
    return (
      <div className="flex items-center gap-2">
        <div className="h-1.5 flex-1 overflow-hidden rounded-full" style={{ background: "var(--color-btc-deep)", minWidth: 50 }}>
          <div
            className="h-full rounded-full transition-all duration-500"
            style={{ width: `${pct}%`, background: "linear-gradient(90deg, var(--color-btc-gold) 0%, var(--color-btc-gold-light) 100%)" }}
          />
        </div>
        <span className="text-[10px] font-mono tabular-nums" style={{ color: "var(--color-btc-text-muted)", minWidth: "5ch", textAlign: "right" }}>
          {confirmations}/{target}
        </span>
      </div>
    );
  }

  return (
    <span
      className="inline-block rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
      style={{ background: "rgba(63, 185, 80, 0.12)", color: "var(--color-btc-green)", border: "1px solid rgba(63, 185, 80, 0.25)" }}
    >
      Verified
    </span>
  );
}

function emptyMessage(filter: FilterTab, search: string, total: number): { title: string; detail: string } {
  if (search.trim()) {
    return { title: "No matches", detail: "Try another txid, address, amount, or address label." };
  }
  if (total === 0) {
    return {
      title: "No activity yet",
      detail: "When you send, receive, or mine, entries appear here with confirmation and maturity status.",
    };
  }
  switch (filter) {
    case "sent":
      return { title: "No outgoing payments", detail: "Nothing labeled as send in this wallet for the current filter." };
    case "received":
      return { title: "No incoming payments", detail: "Received transfers and mature coinbase appear under this tab." };
    case "immature":
      return { title: "No immature coinbase", detail: "Newly mined rewards show here until they reach coin maturity." };
    default:
      return { title: "Nothing to show", detail: "Adjust filters or clear the search box." };
  }
}

export function Transactions() {
  const coinInfo = useCoinInfo();
  const [txs, setTxs] = useState<WalletTransaction[]>([]);
  const [filter, setFilter] = useState<FilterTab>("all");
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [search, setSearch] = useState("");
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [copiedTxid, setCopiedTxid] = useState<string | null>(null);

  const load = useCallback(async (mode: "initial" | "poll" | "manual") => {
    if (mode === "manual") setRefreshing(true);
    try {
      const [raw, book] = await Promise.all([
        ListTransactions().catch(() => [] as unknown[]),
        GetAddressBook().catch(() => null),
      ]);
      const parsed: WalletTransaction[] = (raw || []).map((r) => ({
        txid: String((r as { txid?: string }).txid || ""),
        vout: Number((r as { vout?: number }).vout || 0),
        address: String((r as { address?: string }).address || ""),
        category: (r as { category?: string }).category as WalletTransaction["category"],
        amount: Number((r as { amount?: number }).amount || 0),
        confirmations: Number((r as { confirmations?: number }).confirmations || 0),
        blockheight: Number((r as { blockheight?: number }).blockheight || 0),
        isCoinbase: !!(r as { isCoinbase?: boolean }).isCoinbase,
        maturityProgress: Number((r as { maturityProgress?: number }).maturityProgress || 0),
        maturityTarget: Number((r as { maturityTarget?: number }).maturityTarget || 0),
        maturityStatus: ((r as { maturityStatus?: string }).maturityStatus as WalletTransaction["maturityStatus"]) || "verified",
      }));
      setTxs(parsed);
      if (book && typeof book === "object") setLabels(book as Record<string, string>);
    } catch {
      /* ignore */
    } finally {
      if (mode === "initial") setLoading(false);
      if (mode === "manual") setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void load("initial");
    const id = setInterval(() => void load("poll"), 5000);
    return () => clearInterval(id);
  }, [load]);

  const copyTxid = (txid: string) => {
    navigator.clipboard.writeText(txid).then(() => {
      setCopiedTxid(txid);
      setTimeout(() => setCopiedTxid(null), 2000);
    });
  };

  const byCategory = useMemo(() => {
    if (filter === "all") return txs;
    if (filter === "immature") return txs.filter((t) => t.category === "immature");
    if (filter === "sent") return txs.filter((t) => t.category === "send");
    return txs.filter((t) => t.category !== "immature" && t.category !== "send");
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
      if (String(tx.blockheight).includes(q)) return true;
      return false;
    });
  }, [byCategory, search, labels]);

  const immatureCount = useMemo(() => txs.filter((t) => t.category === "immature").length, [txs]);
  const sentCount = useMemo(() => txs.filter((t) => t.category === "send").length, [txs]);
  const receivedCount = useMemo(() => txs.filter((t) => t.category !== "immature" && t.category !== "send").length, [txs]);

  const tabs: { key: FilterTab; label: string; count: number }[] = [
    { key: "all", label: "All", count: txs.length },
    { key: "sent", label: "Sent", count: sentCount },
    { key: "received", label: "Received", count: receivedCount },
    { key: "immature", label: "Immature", count: immatureCount },
  ];

  const dec = coinInfo.decimals > 4 ? 4 : coinInfo.decimals;
  const empty = emptyMessage(filter, search, txs.length);

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        <div
          className="flex shrink-0 rounded-lg p-0.5"
          role="tablist"
          aria-label="Transaction filters"
          style={{ background: "var(--color-btc-deep)", border: "1px solid var(--color-btc-border)" }}
        >
          {tabs.map((tab) => {
            const selected = filter === tab.key;
            return (
              <button
                key={tab.key}
                type="button"
                role="tab"
                id={`tx-filter-${tab.key}`}
                aria-selected={selected}
                onClick={() => setFilter(tab.key)}
                className="rounded-md px-2.5 py-1.5 text-xs font-semibold transition-colors sm:px-3"
                style={{
                  background: selected ? "rgba(247, 147, 26, 0.18)" : "transparent",
                  color: selected ? "var(--color-btc-gold)" : "var(--color-btc-text-muted)",
                  border: selected ? "1px solid rgba(247, 147, 26, 0.35)" : "1px solid transparent",
                  boxShadow: selected ? "0 0 0 1px rgba(247, 147, 26, 0.08)" : undefined,
                }}
              >
                {tab.label}
                <span className="ml-1.5 font-mono text-[10px] tabular-nums" style={{ opacity: 0.75 }}>
                  {tab.count}
                </span>
              </button>
            );
          })}
        </div>

        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2 sm:justify-end">
          <div
            className="flex min-w-0 flex-1 items-center gap-1.5 rounded-lg px-2.5 py-1.5 sm:max-w-sm sm:flex-initial"
            style={{ background: "var(--color-btc-deep)", border: "1px solid var(--color-btc-border)" }}
          >
            <SearchIcon size={13} />
            <input
              type="search"
              placeholder="Txid, address, amount, label, block…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              spellCheck={false}
              aria-label="Search transactions"
              className="min-w-0 flex-1 bg-transparent text-[11px] text-[var(--color-btc-text)] outline-none placeholder:text-[var(--color-btc-text-dim)]"
            />
            {search ? (
              <button
                type="button"
                onClick={() => setSearch("")}
                className="shrink-0 text-[var(--color-btc-text-dim)] hover:text-[var(--color-btc-text)]"
                style={{ fontSize: "14px", lineHeight: 1 }}
                aria-label="Clear search"
              >
                &times;
              </button>
            ) : null}
          </div>

          <button
            type="button"
            onClick={() => void load("manual")}
            disabled={refreshing}
            className="flex shrink-0 items-center justify-center rounded-lg p-2 transition-colors disabled:opacity-50"
            style={{
              background: "var(--color-btc-deep)",
              border: "1px solid var(--color-btc-border)",
              color: "var(--color-btc-text-muted)",
            }}
            title="Refresh list"
            aria-label="Refresh transaction list"
          >
            <RefreshIcon size={15} spinning={refreshing} />
          </button>
        </div>
      </div>

      {search.trim() ? (
        <p className="text-[11px]" style={{ color: "var(--color-btc-text-muted)" }}>
          {filtered.length} result{filtered.length !== 1 ? "s" : ""} for &ldquo;{search}&rdquo;
        </p>
      ) : (
        <p className="text-[11px]" style={{ color: "var(--color-btc-text-muted)" }}>
          {filtered.length} entr{filtered.length !== 1 ? "ies" : "y"} shown
          {filter !== "all" ? ` (${tabs.find((t) => t.key === filter)?.label ?? filter})` : ""}
        </p>
      )}

      <div
        className="btc-glow flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl"
        style={{ background: "var(--color-btc-card)", border: "1px solid var(--color-btc-border)" }}
      >
        {loading ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-12 text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
            <RefreshIcon size={22} spinning />
            <span>Loading transactions…</span>
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex flex-1 flex-col items-center justify-center px-6 py-10 text-center">
            <ListIllustration />
            <p className="text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
              {empty.title}
            </p>
            <p className="mt-1 max-w-md text-[12px] leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
              {empty.detail}
            </p>
            {!search.trim() && txs.length === 0 ? (
              <div className="mt-5 flex flex-wrap items-center justify-center gap-2">
                <Link
                  to="/send"
                  className="rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors"
                  style={{ background: "rgba(247, 147, 26, 0.15)", color: "var(--color-btc-gold)", border: "1px solid rgba(247, 147, 26, 0.35)" }}
                >
                  Send
                </Link>
                <Link
                  to="/receive"
                  className="rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors"
                  style={{ background: "var(--color-btc-deep)", color: "var(--color-btc-text)", border: "1px solid var(--color-btc-border)" }}
                >
                  Receive
                </Link>
              </div>
            ) : null}
          </div>
        ) : (
          <div className="min-h-0 flex-1 overflow-x-auto overflow-y-auto">
            <div className="min-w-[880px]">
              <div
                className="sticky top-0 z-10 grid gap-3 px-4 py-2.5 text-[10px] font-semibold uppercase tracking-wider"
                style={{
                  gridTemplateColumns: "minmax(200px,1fr) 100px 72px 88px 88px 120px",
                  color: "var(--color-btc-text-dim)",
                  background: "var(--color-btc-surface)",
                  borderBottom: "1px solid var(--color-btc-border)",
                }}
              >
                <span>Transaction</span>
                <span className="text-right">Amount</span>
                <span className="text-right">Block</span>
                <span className="text-right">Confirms</span>
                <span>Type</span>
                <span>Maturity</span>
              </div>

              {filtered.map((tx) => {
                const label = labels[tx.address] || "";
                return (
                  <div
                    key={`${tx.txid}-${tx.vout}`}
                    className="group grid items-center gap-3 px-4 py-3 transition-colors hover:brightness-110"
                    style={{
                      gridTemplateColumns: "minmax(200px,1fr) 100px 72px 88px 88px 120px",
                      borderBottom: "1px solid var(--color-btc-border)",
                    }}
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <div className="h-2 w-2 shrink-0 rounded-full" style={{ background: categoryColor(tx.category) }} />
                        <span className="min-w-0 truncate" title={tx.txid}>
                          <ExplorerLink to={`/explorer/tx/${encodeURIComponent(tx.txid)}`}>
                            {shortHash(tx.txid, 10, 8)}
                          </ExplorerLink>
                        </span>
                        <button
                          type="button"
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
                          {tx.address || "—"}
                        </span>
                        {label ? (
                          <span className="shrink-0 rounded px-1 py-px text-[9px] font-semibold" style={{ background: "rgba(247, 147, 26, 0.08)", color: "var(--color-btc-gold-light)", border: "1px solid rgba(247, 147, 26, 0.15)" }}>
                            {label}
                          </span>
                        ) : null}
                      </div>
                    </div>

                    <div className="text-right">
                      <span className="font-mono text-xs font-bold tabular-nums" style={{ color: tx.amount < 0 ? "var(--color-btc-red)" : "var(--color-btc-text)" }}>
                        {tx.amount < 0 ? "" : "+"}
                        {tx.amount.toFixed(dec)}
                      </span>
                      <span className="ml-1 text-[10px]" style={{ color: "var(--color-btc-gold)" }}>{coinInfo.ticker}</span>
                    </div>

                    <div className="text-right font-mono text-xs tabular-nums" style={{ color: "var(--color-btc-text-muted)" }}>
                      {tx.blockheight > 0 ? tx.blockheight.toLocaleString() : "—"}
                    </div>

                    <div
                      className="text-right font-mono text-xs tabular-nums"
                      style={{ color: tx.confirmations >= tx.maturityTarget ? "var(--color-btc-green)" : "var(--color-btc-text-muted)" }}
                    >
                      {tx.confirmations.toLocaleString()}
                    </div>

                    <div>
                      <span
                        className="inline-block rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
                        style={{
                          background: tx.category === "generate" ? "rgba(63, 185, 80, 0.12)" : tx.category === "immature" ? "rgba(247, 147, 26, 0.12)" : tx.category === "send" ? "rgba(248, 81, 73, 0.12)" : "rgba(88, 166, 255, 0.12)",
                          color: categoryColor(tx.category),
                          border: `1px solid ${tx.category === "generate" ? "rgba(63, 185, 80, 0.25)" : tx.category === "immature" ? "rgba(247, 147, 26, 0.25)" : tx.category === "send" ? "rgba(248, 81, 73, 0.25)" : "rgba(88, 166, 255, 0.25)"}`,
                        }}
                      >
                        {categoryLabel(tx.category)}
                      </span>
                    </div>

                    <div>
                      <MaturityBadge
                        status={tx.maturityStatus}
                        progress={tx.maturityProgress}
                        confirmations={tx.confirmations}
                        target={tx.maturityTarget}
                      />
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
