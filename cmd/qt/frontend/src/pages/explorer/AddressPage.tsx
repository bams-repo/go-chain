import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ExplorerAddressIndex } from "../../../wailsjs/go/main/App";
import { cardClass, cardStyle, CopyChip, ExplorerLink } from "./shared";

const PAGE_SIZE = 50;

export function ExplorerAddressPage() {
  const { addr } = useParams<{ addr: string }>();
  const raw = addr ? decodeURIComponent(addr) : "";
  const [data, setData] = useState<{ address: string; txids: string[]; count: number } | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(0);

  useEffect(() => {
    setPage(0);
  }, [raw]);

  useEffect(() => {
    if (!raw) {
      setErr("Missing address");
      setLoading(false);
      return;
    }
    setLoading(true);
    setErr(null);
    ExplorerAddressIndex(raw)
      .then((res) => {
        const r = res as Record<string, unknown>;
        const txids = Array.isArray(r.txids) ? (r.txids as string[]) : [];
        setData({
          address: String(r.address || raw),
          txids,
          count: Number(r.count ?? txids.length),
        });
      })
      .catch((e: Error) => {
        setData(null);
        setErr(e.message || String(e));
      })
      .finally(() => setLoading(false));
  }, [raw]);

  const pageCount = useMemo(() => Math.max(1, Math.ceil((data?.txids.length ?? 0) / PAGE_SIZE)), [data?.txids.length]);

  useEffect(() => {
    if (page > pageCount - 1) setPage(Math.max(0, pageCount - 1));
  }, [page, pageCount]);

  const slice = useMemo(() => {
    if (!data) return [];
    const start = page * PAGE_SIZE;
    return data.txids.slice(start, start + PAGE_SIZE);
  }, [data, page]);

  if (loading) {
    return (
      <div className="text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
        Scanning chain for this address…
      </div>
    );
  }

  if (err || !data) {
    return (
      <div className="mx-auto max-w-3xl">
        <Link to="/explorer" className="mb-4 inline-block text-sm text-[var(--color-btc-blue)] hover:underline">
          ← Explorer home
        </Link>
        <div
          className="rounded-lg border px-3 py-2 text-sm"
          style={{ borderColor: "var(--color-btc-red)", color: "var(--color-btc-red)", background: "rgba(248,81,73,0.08)" }}
        >
          {err || "No results"}
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-4xl space-y-5">
      <Link to="/explorer" className="inline-block text-sm text-[var(--color-btc-blue)] hover:underline">
        ← Explorer home
      </Link>

      <div className={cardClass()} style={cardStyle()}>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-lg font-semibold" style={{ color: "var(--color-btc-text)" }}>
            Address
          </h1>
          <CopyChip text={data.address} label="Copy address" />
        </div>
        <p className="break-all font-mono text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
          {data.address}
        </p>
        <p className="mt-2 text-xs leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
          Transactions that pay to this P2PKH script or spend an output locked to it (main chain + mempool). Newest first.
          This is only activity for this exact address — sends to other people, change back to other derived addresses in your wallet, and rows that are only “send” summaries do not appear here, so the count can be lower than the wallet Transactions tab.
        </p>
        <p className="mt-2 text-xs font-semibold tabular-nums" style={{ color: "var(--color-btc-text)" }}>
          {data.count} transaction{data.count !== 1 ? "s" : ""} found
        </p>
      </div>

      <div className={cardClass()} style={cardStyle()}>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
            Transactions
          </h2>
          {data.txids.length > PAGE_SIZE && (
            <span className="text-[10px] uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
              Page {page + 1} / {pageCount}
            </span>
          )}
        </div>
        {data.txids.length > PAGE_SIZE && (
          <div className="mb-3 flex flex-wrap gap-2">
            <button
              type="button"
              disabled={page <= 0}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              className="rounded-md border px-2.5 py-1 text-[11px] font-semibold disabled:opacity-40"
              style={{ borderColor: "var(--color-btc-border)", color: "var(--color-btc-text)", background: "var(--color-btc-deep)" }}
            >
              ← Newer
            </button>
            <button
              type="button"
              disabled={page >= pageCount - 1}
              onClick={() => setPage((p) => Math.min(pageCount - 1, p + 1))}
              className="rounded-md border px-2.5 py-1 text-[11px] font-semibold disabled:opacity-40"
              style={{ borderColor: "var(--color-btc-border)", color: "var(--color-btc-text)", background: "var(--color-btc-deep)" }}
            >
              Older →
            </button>
          </div>
        )}
        {slice.length === 0 ? (
          <p className="text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
            No matching transactions on this chain.
          </p>
        ) : (
          <ul className="space-y-1.5 font-mono text-xs">
            {slice.map((txid) => (
              <li key={txid} className="break-all">
                <span title={txid}>
                  <ExplorerLink to={`/explorer/tx/${encodeURIComponent(txid)}`}>{txid}</ExplorerLink>
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
