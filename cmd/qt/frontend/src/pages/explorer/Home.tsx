import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  ExplorerChainOverview,
  ExplorerMempoolSlice,
  ExplorerRecentBlocksPage,
  ExplorerSearch,
} from "../../../wailsjs/go/main/App";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cardClass, cardStyle, CopyChip, ExplorerLink, formatDisplayAmount, shortHash } from "./shared";

type Overview = Record<string, unknown>;
type BlockRow = Record<string, unknown>;
type MempoolRow = Record<string, unknown>;

const BLOCKS_PAGE_SIZE = 25;

export function ExplorerHome() {
  const navigate = useNavigate();
  const [overview, setOverview] = useState<Overview | null>(null);
  const [blocks, setBlocks] = useState<BlockRow[]>([]);
  const [blocksPage, setBlocksPage] = useState(0);
  const [blocksMeta, setBlocksMeta] = useState<{ hasMoreOlder: boolean; hasNewer: boolean; tip: number }>({
    hasMoreOlder: false,
    hasNewer: false,
    tip: 0,
  });
  const [mempool, setMempool] = useState<MempoolRow[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [searchQ, setSearchQ] = useState("");
  const [searching, setSearching] = useState(false);

  const ticker = String(overview?.display_ticker ?? "FAIR");

  const load = useCallback(() => {
    setErr(null);
    Promise.all([
      ExplorerChainOverview(),
      ExplorerRecentBlocksPage(blocksPage, BLOCKS_PAGE_SIZE),
      ExplorerMempoolSlice(40),
    ])
      .then(([o, pageRes, m]) => {
        setOverview((o as Overview) || null);
        const pr = (pageRes as Record<string, unknown>) || {};
        const list = Array.isArray(pr.blocks) ? (pr.blocks as BlockRow[]) : [];
        setBlocks(list);
        setBlocksMeta({
          hasMoreOlder: !!pr.has_more_older,
          hasNewer: !!pr.has_newer,
          tip: Number(pr.tip_height ?? 0),
        });
        setMempool(Array.isArray(m) ? (m as MempoolRow[]) : []);
      })
      .catch((e: Error) => setErr(e.message || String(e)));
  }, [blocksPage]);

  useEffect(() => {
    load();
    const id = setInterval(load, 12000);
    return () => clearInterval(id);
  }, [load]);

  const onSearch = (e: React.FormEvent) => {
    e.preventDefault();
    const q = searchQ.trim();
    if (!q) return;
    setSearching(true);
    setErr(null);
    ExplorerSearch(q)
      .then((res) => {
        const kind = String(res?.kind || "");
        if (kind === "block") {
          const b = res.block as Record<string, unknown> | undefined;
          const h = b?.height;
          const hash = String(b?.hash || q);
          if (typeof h === "number" || typeof h === "string") {
            navigate(`/explorer/block/${encodeURIComponent(String(h))}`);
          } else {
            navigate(`/explorer/block/${encodeURIComponent(hash)}`);
          }
        } else if (kind === "transaction") {
          const tx = res.transaction as Record<string, unknown> | undefined;
          const idTx = String(tx?.txid || q);
          navigate(`/explorer/tx/${encodeURIComponent(idTx)}`);
        } else if (kind === "address") {
          const a = String(res.address || q);
          navigate(`/explorer/address/${encodeURIComponent(a)}`);
        }
      })
      .catch((e: Error) => setErr(e.message || String(e)))
      .finally(() => setSearching(false));
  };

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <form onSubmit={onSearch} className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <Input
          value={searchQ}
          onChange={(ev) => setSearchQ(ev.target.value)}
          placeholder="Block height, hash, txid, or wallet address…"
          className="font-mono text-sm"
          style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)" }}
        />
        <Button
          type="submit"
          disabled={searching}
          className="shrink-0"
          style={{ background: "var(--color-btc-gold)", color: "#0d1117" }}
        >
          {searching ? "Searching…" : "Search chain"}
        </Button>
      </form>

      {err && (
        <div
          className="rounded-lg border px-3 py-2 text-sm"
          style={{ borderColor: "var(--color-btc-red)", color: "var(--color-btc-red)", background: "rgba(248,81,73,0.08)" }}
        >
          {err}
        </div>
      )}

      {overview && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {[
            { label: "Chain height", value: String(overview.height ?? "—") },
            { label: "Difficulty", value: Number(overview.difficulty || 0).toLocaleString(undefined, { maximumFractionDigits: 4 }) },
            { label: "Mempool", value: `${overview.mempool_tx ?? 0} tx · ${((Number(overview.mempool_bytes) || 0) / 1024).toFixed(1)} KB` },
            { label: "Retarget", value: `epoch ${overview.retarget_epoch ?? "—"} · ${overview.epoch_blocks_left ?? "—"} blocks left` },
          ].map((c) => (
            <div key={c.label} className={cardClass()} style={cardStyle()}>
              <div className="text-[10px] font-semibold uppercase tracking-wider" style={{ color: "var(--color-btc-text-dim)" }}>
                {c.label}
              </div>
              <div className="mt-1 text-sm font-semibold tabular-nums" style={{ color: "var(--color-btc-text)" }}>
                {c.value}
              </div>
            </div>
          ))}
        </div>
      )}

      {overview && (
        <div className={cardClass()} style={cardStyle()}>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
              Best block
            </h2>
            <CopyChip text={String(overview.bestblockhash || "")} label="Copy hash" />
          </div>
          <p className="break-all font-mono text-xs leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
            {String(overview.bestblockhash || "")}
          </p>
          <div className="mt-3 grid gap-2 text-[11px] sm:grid-cols-2" style={{ color: "var(--color-btc-text-muted)" }}>
            <div>
              <span className="font-medium" style={{ color: "var(--color-btc-text-dim)" }}>Chain work</span>
              <div className="mt-0.5 break-all font-mono">{String(overview.chainwork || "—")}</div>
            </div>
            <div>
              <span className="font-medium" style={{ color: "var(--color-btc-text-dim)" }}>Genesis</span>
              <div className="mt-0.5 break-all font-mono">{shortHash(String(overview.genesisblockhash || ""), 12, 12)}</div>
            </div>
          </div>
        </div>
      )}

      <div className="grid gap-6 lg:grid-cols-2">
        <div className={cardClass()} style={cardStyle()}>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
              Blocks
            </h2>
            <span className="text-[10px] uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
              Tip {blocksMeta.tip} · page {blocksPage + 1}
            </span>
          </div>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <button
              type="button"
              disabled={!blocksMeta.hasNewer}
              onClick={() => setBlocksPage((p) => Math.max(0, p - 1))}
              className="rounded-md border px-2.5 py-1 text-[11px] font-semibold disabled:opacity-40"
              style={{ borderColor: "var(--color-btc-border)", color: "var(--color-btc-text)", background: "var(--color-btc-deep)" }}
            >
              ← Newer
            </button>
            <button
              type="button"
              disabled={!blocksMeta.hasMoreOlder}
              onClick={() => setBlocksPage((p) => p + 1)}
              className="rounded-md border px-2.5 py-1 text-[11px] font-semibold disabled:opacity-40"
              style={{ borderColor: "var(--color-btc-border)", color: "var(--color-btc-text)", background: "var(--color-btc-deep)" }}
            >
              Older →
            </button>
            <span className="text-[10px]" style={{ color: "var(--color-btc-text-muted)" }}>
              {BLOCKS_PAGE_SIZE} per page toward genesis
            </span>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[320px] text-left text-xs">
              <thead>
                <tr style={{ color: "var(--color-btc-text-dim)" }}>
                  <th className="pb-2 pr-2 font-medium">Height</th>
                  <th className="pb-2 pr-2 font-medium">Hash</th>
                  <th className="pb-2 pr-2 font-medium">Tx</th>
                  <th className="pb-2 font-medium">Time</th>
                </tr>
              </thead>
              <tbody>
                {blocks.map((b) => {
                  const h = Number(b.height);
                  const hash = String(b.hash || "");
                  return (
                    <tr key={hash || h} className="border-t" style={{ borderColor: "var(--color-btc-border)" }}>
                      <td className="py-2 pr-2 font-mono tabular-nums">
                        <ExplorerLink to={`/explorer/block/${h}`}>{h}</ExplorerLink>
                      </td>
                      <td className="max-w-[140px] truncate py-2 pr-2 font-mono">
                        <ExplorerLink to={`/explorer/block/${encodeURIComponent(hash)}`}>{shortHash(hash, 8, 6)}</ExplorerLink>
                      </td>
                      <td className="py-2 pr-2 tabular-nums">{Number(b.nTx ?? 0)}</td>
                      <td className="py-2 font-mono tabular-nums" style={{ color: "var(--color-btc-text-muted)" }}>
                        {new Date(Number(b.time) * 1000).toLocaleString()}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>

        <div className={cardClass()} style={cardStyle()}>
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
              Mempool (by fee rate)
            </h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[280px] text-left text-xs">
              <thead>
                <tr style={{ color: "var(--color-btc-text-dim)" }}>
                  <th className="pb-2 pr-2 font-medium">Txid</th>
                  <th className="pb-2 pr-2 font-medium">Fee</th>
                  <th className="pb-2 font-medium">Bytes</th>
                </tr>
              </thead>
              <tbody>
                {mempool.map((row) => {
                  const id = String(row.txid || "");
                  return (
                    <tr key={id} className="border-t" style={{ borderColor: "var(--color-btc-border)" }}>
                      <td className="max-w-[160px] truncate py-2 pr-2 font-mono">
                        <ExplorerLink to={`/explorer/tx/${encodeURIComponent(id)}`}>{shortHash(id, 8, 6)}</ExplorerLink>
                      </td>
                      <td className="py-2 pr-2 tabular-nums" style={{ color: "var(--color-btc-text-muted)" }}>
                        {formatDisplayAmount(Number(row.fee), ticker)}
                      </td>
                      <td className="py-2 tabular-nums">{Number(row.size || 0)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  );
}
