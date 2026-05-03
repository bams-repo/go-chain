import { useState, type CSSProperties, type ReactNode } from "react";
import { Link } from "react-router-dom";

const BASE = 1e8;

export function formatDisplayAmount(baseUnits: number, ticker: string, decimals = 8): string {
  if (!Number.isFinite(baseUnits)) return "—";
  const v = baseUnits / BASE;
  return `${v.toFixed(Math.min(decimals, 8))} ${ticker}`;
}

export function shortHash(hex: string, head = 10, tail = 8): string {
  const s = String(hex || "");
  if (s.length <= head + tail + 1) return s;
  return `${s.slice(0, head)}…${s.slice(-tail)}`;
}

export function formatUnix(ts: number): string {
  if (!Number.isFinite(ts) || ts <= 0) return "—";
  return new Date(ts * 1000).toLocaleString();
}

export function CopyChip({ text, label = "Copy" }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  const onClick = () => {
    void navigator.clipboard.writeText(text).then(() => {
      setDone(true);
      setTimeout(() => setDone(false), 1500);
    });
  };
  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded border px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide transition-colors"
      style={{
        borderColor: "var(--color-btc-border)",
        color: "var(--color-btc-text-muted)",
        background: "var(--color-btc-deep)",
      }}
      title={label}
    >
      {done ? "Copied" : label}
    </button>
  );
}

export function ExplorerLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link
      to={to}
      className="font-mono text-xs text-[var(--color-btc-blue)] underline-offset-2 hover:underline"
    >
      {children}
    </Link>
  );
}

export function cardClass(): string {
  return "rounded-xl border p-4 md:p-5";
}

export function cardStyle(): CSSProperties {
  return {
    borderColor: "var(--color-btc-border)",
    background: "var(--color-btc-card)",
  };
}
