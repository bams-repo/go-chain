import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ExplorerGetBlock } from "../../../wailsjs/go/main/App";
import { cardClass, cardStyle, CopyChip, ExplorerLink, shortHash } from "./shared";

const TX_PAGE_SIZE = 40;

export function ExplorerBlockPage() {
  const { id } = useParams<{ id: string }>();
  const rawId = id ? decodeURIComponent(id) : "";
  const [block, setBlock] = useState<Record<string, unknown> | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [txPage, setTxPage] = useState(0);

  useEffect(() => {
    setTxPage(0);
  }, [rawId]);

  useEffect(() => {
    if (!rawId) {
      setErr("Missing block reference");
      setLoading(false);
      return;
    }
    setLoading(true);
    setErr(null);
    ExplorerGetBlock(rawId)
      .then((b) => setBlock(b as Record<string, unknown>))
      .catch((e: Error) => {
        setBlock(null);
        setErr(e.message || String(e));
      })
      .finally(() => setLoading(false));
  }, [rawId]);

  const txids = useMemo(
    () => (Array.isArray(block?.tx) ? (block!.tx as string[]) : []),
    [block],
  );

  const txPageCount = Math.max(1, Math.ceil(txids.length / TX_PAGE_SIZE));

  useEffect(() => {
    if (txPage > txPageCount - 1) setTxPage(txPageCount - 1);
  }, [txPage, txPageCount]);

  const txSlice = useMemo(() => {
    const start = txPage * TX_PAGE_SIZE;
    return txids.slice(start, start + TX_PAGE_SIZE);
  }, [txids, txPage]);

  if (loading) {
    return (
      <div className="text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
        Loading block…
      </div>
    );
  }

  if (err || !block) {
    return (
      <div className="mx-auto max-w-3xl">
        <Link to="/explorer" className="mb-4 inline-block text-sm text-[var(--color-btc-blue)] hover:underline">
          ← Explorer home
        </Link>
        <div
          className="rounded-lg border px-3 py-2 text-sm"
          style={{ borderColor: "var(--color-btc-red)", color: "var(--color-btc-red)", background: "rgba(248,81,73,0.08)" }}
        >
          {err || "Block not found"}
        </div>
      </div>
    );
  }

  const hash = String(block.hash || "");
  const prev = String(block.previousblockhash || "");
  const next = block.nextblockhash != null ? String(block.nextblockhash) : "";

  return (
    <div className="mx-auto max-w-4xl space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/explorer" className="text-sm text-[var(--color-btc-blue)] hover:underline">
          ← Explorer home
        </Link>
        <span className="text-[11px] font-medium uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
          Block {String(block.height ?? "")}
        </span>
      </div>

      <div className={cardClass()} style={cardStyle()}>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-lg font-semibold" style={{ color: "var(--color-btc-text)" }}>
            Block {String(block.height ?? "")}
          </h1>
          <CopyChip text={hash} label="Copy block hash" />
        </div>
        <p className="break-all font-mono text-xs" style={{ color: "var(--color-btc-text-muted)" }}>
          {hash}
        </p>

        <dl className="mt-4 grid gap-2 text-xs sm:grid-cols-2">
          {[
            ["Confirmations", String(block.confirmations ?? "—")],
            ["Timestamp", new Date(Number(block.time) * 1000).toLocaleString()],
            ["Size (bytes)", String(block.size ?? "—")],
            ["Weight", String(block.weight ?? "—")],
            ["Version", String(block.version ?? "—")],
            ["Merkle root", shortHash(String(block.merkleroot || ""), 16, 16)],
            ["Bits", String(block.bits ?? "—")],
            ["Nonce", String(block.nonce ?? "—")],
            ["Difficulty", Number(block.difficulty || 0).toLocaleString(undefined, { maximumFractionDigits: 6 })],
            ["Transactions", String(block.nTx ?? txids.length)],
          ].map(([k, v]) => (
            <div key={k} className="flex flex-col gap-0.5 border-t pt-2 sm:border-t-0 sm:pt-0" style={{ borderColor: "var(--color-btc-border)" }}>
              <dt className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
                {k}
              </dt>
              <dd className="font-mono tabular-nums" style={{ color: "var(--color-btc-text)" }}>
                {v}
              </dd>
            </div>
          ))}
        </dl>

        <div className="mt-4 flex flex-wrap gap-3 text-xs">
          {prev && (
            <div>
              <span style={{ color: "var(--color-btc-text-dim)" }}>Previous</span>
              <div className="mt-1">
                <ExplorerLink to={`/explorer/block/${encodeURIComponent(prev)}`}>{shortHash(prev, 12, 10)}</ExplorerLink>
              </div>
            </div>
          )}
          {next && (
            <div>
              <span style={{ color: "var(--color-btc-text-dim)" }}>Next</span>
              <div className="mt-1">
                <ExplorerLink to={`/explorer/block/${encodeURIComponent(next)}`}>{shortHash(next, 12, 10)}</ExplorerLink>
              </div>
            </div>
          )}
        </div>
      </div>

      <div className={cardClass()} style={cardStyle()}>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
            Transactions ({txids.length})
          </h2>
          {txids.length > TX_PAGE_SIZE && (
            <span className="text-[10px] uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
              Page {txPage + 1} / {txPageCount}
            </span>
          )}
        </div>
        {txids.length > TX_PAGE_SIZE && (
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <button
              type="button"
              disabled={txPage <= 0}
              onClick={() => setTxPage((p) => Math.max(0, p - 1))}
              className="rounded-md border px-2.5 py-1 text-[11px] font-semibold disabled:opacity-40"
              style={{ borderColor: "var(--color-btc-border)", color: "var(--color-btc-text)", background: "var(--color-btc-deep)" }}
            >
              ← Earlier in block
            </button>
            <button
              type="button"
              disabled={txPage >= txPageCount - 1}
              onClick={() => setTxPage((p) => Math.min(txPageCount - 1, p + 1))}
              className="rounded-md border px-2.5 py-1 text-[11px] font-semibold disabled:opacity-40"
              style={{ borderColor: "var(--color-btc-border)", color: "var(--color-btc-text)", background: "var(--color-btc-deep)" }}
            >
              Later in block →
            </button>
          </div>
        )}
        <ul className="space-y-1.5 font-mono text-xs">
          {txSlice.map((txid) => (
            <li key={txid} className="flex flex-wrap items-center gap-2 break-all">
              <ExplorerLink to={`/explorer/tx/${encodeURIComponent(txid)}`}>{txid}</ExplorerLink>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
