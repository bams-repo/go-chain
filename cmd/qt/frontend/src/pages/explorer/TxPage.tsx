import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ExplorerGetTransaction } from "../../../wailsjs/go/main/App";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import { cardClass, cardStyle, CopyChip, ExplorerLink, formatDisplayAmount, shortHash } from "./shared";

type Vin = Record<string, unknown>;
type Vout = Record<string, unknown>;

function ScriptPubKeyPanel({ spk, title }: { spk: Record<string, unknown> | undefined; title: string }) {
  if (!spk || typeof spk !== "object") return null;
  const hex = spk.hex != null ? String(spk.hex) : "";
  const asm = spk.asm != null ? String(spk.asm) : "";
  const typ = spk.type != null ? String(spk.type) : "";
  const addrs = Array.isArray(spk.addresses) ? (spk.addresses as unknown[]).map((a) => String(a)) : [];
  return (
    <div className="space-y-1.5">
      <div className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
        {title}
        {typ ? ` · ${typ}` : ""}
      </div>
      {addrs.length > 0 && (
        <div className="space-y-0.5">
          {addrs.map((a) => (
            <div key={a} className="break-all font-mono text-[11px]" style={{ color: "var(--color-btc-text)" }}>
              {a}
            </div>
          ))}
        </div>
      )}
      {asm ? (
        <pre
          className="whitespace-pre-wrap break-all rounded border p-2 text-[10px] leading-relaxed"
          style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)", color: "var(--color-btc-text-muted)" }}
        >
          {asm}
        </pre>
      ) : null}
      {hex ? (
        <pre
          className="max-h-48 whitespace-pre-wrap break-all rounded border p-2 text-[10px] leading-relaxed"
          style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)", color: "var(--color-btc-text-muted)" }}
        >
          {hex}
        </pre>
      ) : null}
    </div>
  );
}

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
        <div className="space-y-4 text-xs">
          {vins.map((vin, i) => {
            const coinbase = vin.coinbase != null ? String(vin.coinbase) : "";
            if (coinbase) {
              return (
                <div key={i} className="rounded border p-3" style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)" }}>
                  <span className="font-semibold" style={{ color: "var(--color-btc-gold)" }}>
                    Coinbase
                  </span>
                  <div className="mt-2 space-y-2">
                    <div className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
                      Coinbase data (hex)
                    </div>
                    <pre className="max-h-32 overflow-auto whitespace-pre-wrap break-all text-[10px]" style={{ color: "var(--color-btc-text-muted)" }}>
                      {coinbase}
                    </pre>
                    {vin.coinbase_asm != null ? (
                      <>
                        <div className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
                          Coinbase (asm)
                        </div>
                        <pre className="max-h-32 overflow-auto whitespace-pre-wrap break-all text-[10px]" style={{ color: "var(--color-btc-text-muted)" }}>
                          {String(vin.coinbase_asm)}
                        </pre>
                      </>
                    ) : null}
                  </div>
                </div>
              );
            }
            const prevTx = String(vin.txid || "");
            const vout = Number(vin.vout ?? 0);
            const addr = vin.address != null ? String(vin.address) : "";
            const prevout = vin.prevout as Record<string, unknown> | undefined;
            const scriptSig = vin.scriptSig as Record<string, unknown> | undefined;
            const sigHex = scriptSig?.hex != null ? String(scriptSig.hex) : "";
            const sigAsm = scriptSig?.asm != null ? String(scriptSig.asm) : "";
            return (
              <div key={i} className="rounded border p-3" style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)" }}>
                <div className="flex flex-wrap items-baseline gap-2">
                  <ExplorerLink to={`/explorer/tx/${encodeURIComponent(prevTx)}`}>{shortHash(prevTx, 16, 14)}</ExplorerLink>
                  <span style={{ color: "var(--color-btc-text-muted)" }}> :{vout}</span>
                </div>
                {addr ? (
                  <div className="mt-2 break-all font-mono text-[11px]" style={{ color: "var(--color-btc-text)" }}>
                    <span style={{ color: "var(--color-btc-text-dim)" }}>Prevout address </span>
                    {addr}
                  </div>
                ) : null}
                {prevout && typeof prevout === "object" ? (
                  <div className="mt-3 space-y-2 border-t pt-3" style={{ borderColor: "var(--color-btc-border)" }}>
                    <div className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
                      Previous output
                    </div>
                    {prevout.value != null ? (
                      <div className="font-mono text-[11px] tabular-nums" style={{ color: "var(--color-btc-text)" }}>
                        {formatDisplayAmount(Number(prevout.value), ticker)}
                      </div>
                    ) : null}
                    <ScriptPubKeyPanel spk={prevout.scriptPubKey as Record<string, unknown> | undefined} title="scriptPubKey" />
                  </div>
                ) : null}
                {(sigHex || sigAsm) ? (
                  <div className="mt-3 space-y-2 border-t pt-3" style={{ borderColor: "var(--color-btc-border)" }}>
                    <div className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: "var(--color-btc-text-dim)" }}>
                      scriptSig
                    </div>
                    {sigAsm ? (
                      <pre className="whitespace-pre-wrap break-all text-[10px]" style={{ color: "var(--color-btc-text-muted)" }}>{sigAsm}</pre>
                    ) : null}
                    {sigHex ? (
                      <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-all text-[10px]" style={{ color: "var(--color-btc-text-muted)" }}>{sigHex}</pre>
                    ) : null}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      </div>

      <div className={cardClass()} style={cardStyle()}>
        <h2 className="mb-3 text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>
          Outputs ({vouts.length})
        </h2>
        <div className="space-y-4">
          {vouts.map((o, idx) => {
            const n = Number(o.n ?? idx);
            const val = Number(o.value ?? 0);
            const spk = o.scriptPubKey as Record<string, unknown> | undefined;
            return (
              <div
                key={n}
                className="rounded border p-3"
                style={{ borderColor: "var(--color-btc-border)", background: "var(--color-btc-deep)" }}
              >
                <div className="flex flex-wrap items-baseline gap-3 text-xs">
                  <span className="tabular-nums" style={{ color: "var(--color-btc-text-dim)" }}>n={n}</span>
                  <span className="font-mono font-semibold tabular-nums" style={{ color: "var(--color-btc-text)" }}>
                    {formatDisplayAmount(val, ticker)}
                  </span>
                </div>
                <div className="mt-3">
                  <ScriptPubKeyPanel spk={spk} title="scriptPubKey" />
                </div>
              </div>
            );
          })}
        </div>
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
