import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ExplorerGetTransaction } from "../../../wailsjs/go/main/App";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import { cardClass, cardStyle, CopyChip, ExplorerLink, formatDisplayAmount, shortHash } from "./shared";

type Vin = Record<string, unknown>;
type Vout = Record<string, unknown>;

export function ExplorerTxPage() {
  const { txid } = useParams<{ txid: string }>();
  const raw = txid ? decodeURIComponent(txid) : "";
  const coinInfo = useCoinInfo();
  const ticker = coinInfo?.ticker || "FAIR";

  const [tx, setTx] = useState<Record<string, unknown> | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [showHex, setShowHex] = useState(false);

  useEffect(() => {
    if (!raw) {
      setErr("Missing transaction id");
      setLoading(false);
      return;
    }
    setLoading(true);
    setErr(null);
    ExplorerGetTransaction(raw)
      .then((t) => setTx(t as Record<string, unknown>))
      .catch((e: Error) => {
        setTx(null);
        setErr(e.message || String(e));
      })
      .finally(() => setLoading(false));
  }, [raw]);

  if (loading) {
    return (
      <div className="text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
        Loading transaction…
      </div>
    );
  }

  if (err || !tx) {
    return (
      <div className="mx-auto max-w-3xl">
        <Link to="/explorer" className="mb-4 inline-block text-sm text-[var(--color-btc-blue)] hover:underline">
          ← Explorer home
        </Link>
        <div
          className="rounded-lg border px-3 py-2 text-sm"
          style={{ borderColor: "var(--color-btc-red)", color: "var(--color-btc-red)", background: "rgba(248,81,73,0.08)" }}
        >
          {err || "Transaction not found"}
        </div>
      </div>
    );
  }

  const id = String(tx.txid || raw);
  const vins = Array.isArray(tx.vin) ? (tx.vin as Vin[]) : [];
  const vouts = Array.isArray(tx.vout) ? (tx.vout as Vout[]) : [];
  const hex = String(tx.hex || "");
  const blockhash = tx.blockhash != null ? String(tx.blockhash) : "";
  const blockheight = tx.blockheight;

  return (
    <div className="mx-auto max-w-4xl space-y-5">
      <Link to="/explorer" className="inline-block text-sm text-[var(--color-btc-blue)] hover:underline">
        ← Explorer home
      </Link>

      <div className={cardClass()} style={cardStyle()}>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-lg font-semibold" style={{ color: "var(--color-btc-text)" }}>
            Transaction
          </h1>
          <CopyChip text={id} label="Copy txid" />
        </div>
        <p className="break-all font-mono text-xs" style={{ color: "var(--color-btc-text-muted)" }}>
          {id}
        </p>
        <dl className="mt-4 grid gap-2 text-xs sm:grid-cols-3">
          <div>
            <dt className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
              Confirmations
            </dt>
            <dd className="mt-0.5 font-mono">{String(tx.confirmations ?? "0")}</dd>
          </div>
          <div>
            <dt className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
              Size
            </dt>
            <dd className="mt-0.5 font-mono">{String(tx.size ?? "—")} bytes</dd>
          </div>
          <div>
            <dt className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
              Locktime
            </dt>
            <dd className="mt-0.5 font-mono">{String(tx.locktime ?? "—")}</dd>
          </div>
        </dl>
        {blockhash && (
          <div className="mt-3 text-xs">
            <span style={{ color: "var(--color-btc-text-dim)" }}>Block </span>
            {blockheight != null && (
              <ExplorerLink to={`/explorer/block/${String(blockheight)}`}>#{String(blockheight)}</ExplorerLink>
            )}
            <span className="mx-1" style={{ color: "var(--color-btc-text-dim)" }}>
              ·
            </span>
            <ExplorerLink to={`/explorer/block/${encodeURIComponent(blockhash)}`}>{shortHash(blockhash, 14, 12)}</ExplorerLink>
          </div>
        )}
      </div>

      <div className={cardClass()} style={cardStyle()}>
        <h2 className="mb-3 text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
          Inputs ({vins.length})
        </h2>
        <div className="space-y-3 text-xs">
          {vins.map((vin, i) => {
            const coinbase = vin.coinbase != null ? String(vin.coinbase) : "";
            if (coinbase) {
              return (
                <div key={i} className="rounded border p-2 font-mono" style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)" }}>
                  <span className="font-semibold" style={{ color: "var(--color-btc-gold)" }}>
                    Coinbase
                  </span>
                  <div className="mt-1 max-h-24 overflow-auto break-all opacity-90">{shortHash(coinbase, 40, 20)}</div>
                </div>
              );
            }
            const prevTx = String(vin.txid || "");
            const vout = Number(vin.vout ?? 0);
            return (
              <div key={i} className="rounded border p-2" style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)" }}>
                <ExplorerLink to={`/explorer/tx/${encodeURIComponent(prevTx)}`}>{shortHash(prevTx, 16, 14)}</ExplorerLink>
                <span style={{ color: "var(--color-btc-text-muted)" }}> :{vout}</span>
              </div>
            );
          })}
        </div>
      </div>

      <div className={cardClass()} style={cardStyle()}>
        <h2 className="mb-3 text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
          Outputs ({vouts.length})
        </h2>
        <table className="w-full text-left text-xs">
          <thead>
            <tr style={{ color: "var(--color-btc-text-dim)" }}>
              <th className="pb-2 pr-2 font-medium">n</th>
              <th className="pb-2 pr-2 font-medium">Value ({ticker})</th>
              <th className="pb-2 font-medium">Script (hex)</th>
            </tr>
          </thead>
          <tbody>
            {vouts.map((o, idx) => {
              const n = Number(o.n ?? idx);
              const val = Number(o.value ?? 0);
              const spk = (o.scriptPubKey as Record<string, unknown> | undefined)?.hex;
              const hexStr = spk != null ? String(spk) : "";
              return (
                <tr key={n} className="border-t align-top font-mono" style={{ borderColor: "var(--color-btc-border)" }}>
                  <td className="py-2 pr-2 tabular-nums">{n}</td>
                  <td className="py-2 pr-2 tabular-nums" style={{ color: "var(--color-btc-text)" }}>
                    {formatDisplayAmount(val, ticker)}
                  </td>
                  <td className="max-w-[200px] break-all py-2 text-[10px] opacity-90">{shortHash(hexStr, 24, 12)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {hex && (
        <div className={cardClass()} style={cardStyle()}>
          <button
            type="button"
            className="mb-2 text-sm font-medium text-[var(--color-btc-blue)] hover:underline"
            onClick={() => setShowHex((v) => !v)}
          >
            {showHex ? "Hide" : "Show"} raw hex
          </button>
          {showHex && (
            <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded border p-2 text-[10px]" style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)" }}>
              {hex}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}
