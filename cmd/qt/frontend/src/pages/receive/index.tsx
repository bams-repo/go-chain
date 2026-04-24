import { useCallback, useEffect, useRef, useState } from "react";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import {
  GetWalletAddress,
  GetNewAddress,
  ListReceiveAddresses,
  GetAddressBook,
  SetAddressLabel,
  GetAddressTransactionCounts,
} from "../../../wailsjs/go/main/App";

function CopyIcon({ size = 14 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
    </svg>
  );
}

function CheckIcon({ size = 14 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round" strokeLinejoin="round">
      <polyline points="20 6 9 17 4 12" />
    </svg>
  );
}

function PencilIcon({ size = 12 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="M17 3a2.85 2.85 0 114 4L7.5 20.5 2 22l1.5-5.5Z" />
      <path d="M15 5l4 4" />
    </svg>
  );
}

function useCopyToClipboard(timeout = 2000) {
  const [copiedAddr, setCopiedAddr] = useState<string | null>(null);
  const copy = useCallback(
    (text: string) => {
      navigator.clipboard.writeText(text).then(() => {
        setCopiedAddr(text);
        setTimeout(() => setCopiedAddr(null), timeout);
      });
    },
    [timeout],
  );
  return { copiedAddr, copy };
}

function InlineLabelEditor({
  address,
  label,
  onSave,
}: {
  address: string;
  label: string;
  onSave: (address: string, label: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(label);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => { setValue(label); }, [label]);
  useEffect(() => { if (editing) inputRef.current?.focus(); }, [editing]);

  const commit = () => {
    const trimmed = value.trim();
    onSave(address, trimmed);
    setEditing(false);
  };

  if (editing) {
    return (
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") commit();
          if (e.key === "Escape") { setValue(label); setEditing(false); }
        }}
        placeholder="Add label..."
        spellCheck={false}
        className="rounded px-1.5 py-0.5 text-[11px] font-medium outline-none"
        style={{
          background: "var(--color-btc-deep)",
          color: "var(--color-btc-text)",
          border: "1px solid var(--color-btc-gold)",
          minWidth: 100,
          maxWidth: 200,
        }}
      />
    );
  }

  return (
    <button
      onClick={() => setEditing(true)}
      className="group/label flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] transition-colors"
      style={{
        background: label ? "rgba(247, 147, 26, 0.08)" : "transparent",
        color: label ? "var(--color-btc-gold-light)" : "var(--color-btc-text-dim)",
        border: label ? "1px solid rgba(247, 147, 26, 0.15)" : "1px solid transparent",
      }}
      title={label ? "Edit label" : "Add label"}
    >
      {label || "Add label"}
      <span className="opacity-0 transition-opacity group-hover/label:opacity-100">
        <PencilIcon size={10} />
      </span>
    </button>
  );
}

export function Receive() {
  const coinInfo = useCoinInfo();
  const [defaultAddress, setDefaultAddress] = useState("");
  const [addresses, setAddresses] = useState<string[]>([]);
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [txCounts, setTxCounts] = useState<Record<string, number>>({});
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const { copiedAddr, copy } = useCopyToClipboard();

  const loadData = useCallback(() => {
    GetWalletAddress().then((a) => { if (a) setDefaultAddress(a); }).catch(() => {});
    ListReceiveAddresses().then((list) => { if (list) setAddresses(list); }).catch(() => {});
    GetAddressBook().then((book) => { if (book) setLabels(book); }).catch(() => {});
    GetAddressTransactionCounts().then((c) => { if (c) setTxCounts(c); }).catch(() => {});
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  const handleNewAddress = async () => {
    setGenerating(true);
    setError("");
    try {
      const addr = await GetNewAddress();
      if (addr) {
        setAddresses((prev) => [...prev, addr]);
        copy(addr);
      }
    } catch (err: any) {
      setError(typeof err === "string" ? err : err?.message || "Failed to generate address");
    } finally {
      setGenerating(false);
    }
  };

  const handleSaveLabel = (address: string, label: string) => {
    SetAddressLabel(address, label).then(() => {
      setLabels((prev) => {
        const next = { ...prev };
        if (label) next[address] = label; else delete next[address];
        return next;
      });
    }).catch(() => {});
  };

  const filtered = addresses.filter((addr) => {
    if (!search.trim()) return true;
    const q = search.toLowerCase();
    return addr.toLowerCase().includes(q) || (labels[addr] || "").toLowerCase().includes(q);
  });

  const sectionLabel: React.CSSProperties = {
    color: "var(--color-btc-text-dim)",
    fontSize: "10px",
    fontWeight: 600,
    textTransform: "uppercase",
    letterSpacing: "0.12em",
    marginBottom: "8px",
  };

  const addressCodeStyle: React.CSSProperties = {
    background: "var(--color-btc-deep)",
    color: "var(--color-btc-gold-light)",
    border: "1px solid var(--color-btc-border)",
    borderRadius: "8px",
    padding: "12px 14px",
    fontSize: "13px",
    fontFamily: "monospace",
    wordBreak: "break-all",
    lineHeight: 1.5,
  };

  const btnPrimary: React.CSSProperties = {
    background: "linear-gradient(135deg, #f7931a 0%, #c67200 100%)",
    color: "#fff",
    border: "none",
    borderRadius: "8px",
    padding: "10px 24px",
    fontSize: "13px",
    fontWeight: 600,
    cursor: "pointer",
    letterSpacing: "0.02em",
    transition: "opacity 0.2s",
  };

  return (
    <div className="flex h-full flex-col gap-3">
      {/* Current receive address */}
      <div
        className="btc-noise btc-glow-active relative overflow-hidden rounded-xl p-5"
        style={{
          background: "linear-gradient(135deg, var(--color-btc-card) 0%, var(--color-btc-surface) 100%)",
          border: "1px solid var(--color-btc-border)",
        }}
      >
        <div className="absolute -right-8 -top-8 h-32 w-32 rounded-full opacity-[0.04]" style={{ background: "var(--color-btc-gold)" }} />
        <div className="relative z-10 flex flex-col gap-3">
          <div className="flex items-start justify-between gap-2">
            <div>
              <h3 style={sectionLabel}>Your Receive Address</h3>
              <p className="text-[11px]" style={{ color: "var(--color-btc-text-muted)" }}>
                Share this address to receive {coinInfo.ticker}
              </p>
            </div>
            {defaultAddress && (
              <InlineLabelEditor address={defaultAddress} label={labels[defaultAddress] || ""} onSave={handleSaveLabel} />
            )}
          </div>

          <div className="flex items-start gap-2">
            <code className="min-w-0 flex-1" style={addressCodeStyle}>
              {defaultAddress || "Loading..."}
            </code>
            {defaultAddress && (
              <button
                onClick={() => copy(defaultAddress)}
                className="shrink-0 rounded-lg p-2.5 transition-colors"
                style={{
                  background: copiedAddr === defaultAddress ? "rgba(63, 185, 80, 0.15)" : "rgba(247, 147, 26, 0.12)",
                  color: copiedAddr === defaultAddress ? "var(--color-btc-green)" : "var(--color-btc-gold)",
                  border: `1px solid ${copiedAddr === defaultAddress ? "rgba(63, 185, 80, 0.3)" : "rgba(247, 147, 26, 0.25)"}`,
                }}
                title="Copy address"
              >
                {copiedAddr === defaultAddress ? <CheckIcon size={16} /> : <CopyIcon size={16} />}
              </button>
            )}
          </div>
          {copiedAddr === defaultAddress && (
            <p className="text-[11px] font-medium" style={{ color: "var(--color-btc-green)" }}>
              Address copied to clipboard
            </p>
          )}
        </div>
      </div>

      {/* Generate new address */}
      <div
        className="btc-glow flex items-center justify-between rounded-xl p-4"
        style={{ background: "var(--color-btc-card)", border: "1px solid var(--color-btc-border)" }}
      >
        <div className="min-w-0 flex-1">
          <h3 className="text-xs font-semibold" style={{ color: "var(--color-btc-text)" }}>Generate New Address</h3>
          <p className="mt-0.5 text-[11px]" style={{ color: "var(--color-btc-text-muted)" }}>Derive a fresh receiving address for better privacy</p>
        </div>
        <button
          onClick={handleNewAddress}
          disabled={generating}
          style={{ ...btnPrimary, opacity: generating ? 0.6 : 1, cursor: generating ? "not-allowed" : "pointer", padding: "8px 18px", fontSize: "12px" }}
        >
          {generating ? "Generating..." : "New Address"}
        </button>
      </div>

      {error && (
        <div className="rounded-lg px-4 py-2 text-[11px] font-medium" style={{ background: "rgba(248, 81, 73, 0.1)", color: "var(--color-btc-red)", border: "1px solid rgba(248, 81, 73, 0.25)" }}>
          {error}
        </div>
      )}

      {/* Address list */}
      <div
        className="btc-glow flex flex-1 flex-col rounded-xl"
        style={{ background: "var(--color-btc-card)", border: "1px solid var(--color-btc-border)", minHeight: 0 }}
      >
        <div className="flex shrink-0 items-center justify-between gap-3 px-4 pt-4 pb-2">
          <h3 style={sectionLabel} className="mb-0!">All Receive Addresses ({addresses.length})</h3>
          <input
            type="text"
            placeholder="Search addresses or labels..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            spellCheck={false}
            className="rounded-lg px-2.5 py-1.5 text-[11px] outline-none"
            style={{
              background: "var(--color-btc-deep)",
              color: "var(--color-btc-text)",
              border: "1px solid var(--color-btc-border)",
              minWidth: 180,
              maxWidth: 260,
              transition: "border-color 0.2s",
            }}
            onFocus={(e) => (e.target.style.borderColor = "var(--color-btc-gold)")}
            onBlur={(e) => (e.target.style.borderColor = "var(--color-btc-border)")}
          />
        </div>

        <div className="flex-1 overflow-y-auto overscroll-contain px-4 pb-4" style={{ minHeight: 0 }}>
          {filtered.length === 0 ? (
            <p className="py-6 text-center text-xs" style={{ color: "var(--color-btc-text-dim)" }}>
              {search ? "No matching addresses" : "No addresses yet"}
            </p>
          ) : (
            <div className="flex flex-col gap-1">
              {[...filtered].reverse().map((addr) => {
                const isDefault = addr === defaultAddress;
                const isCopied = copiedAddr === addr;
                const label = labels[addr] || "";
                const count = txCounts[addr] || 0;
                const idx = addresses.indexOf(addr);

                return (
                  <div
                    key={addr}
                    className="group flex items-center gap-2 rounded-lg px-3 py-2.5 transition-colors"
                    style={{
                      background: isDefault ? "rgba(247, 147, 26, 0.06)" : "transparent",
                      border: isDefault ? "1px solid rgba(247, 147, 26, 0.15)" : "1px solid transparent",
                    }}
                  >
                    <span className="shrink-0 text-[10px] font-mono tabular-nums" style={{ color: "var(--color-btc-text-dim)", minWidth: "24px" }}>
                      #{idx + 1}
                    </span>

                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <code className="truncate text-[12px] font-mono" style={{ color: "var(--color-btc-gold-light)" }} title={addr}>
                          {addr}
                        </code>
                        {isDefault && (
                          <span className="shrink-0 rounded px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider" style={{ background: "rgba(247, 147, 26, 0.15)", color: "var(--color-btc-gold)", border: "1px solid rgba(247, 147, 26, 0.25)" }}>
                            Default
                          </span>
                        )}
                      </div>
                      <div className="mt-1 flex items-center gap-2">
                        <InlineLabelEditor address={addr} label={label} onSave={handleSaveLabel} />
                        {count > 0 && (
                          <span className="text-[10px] font-mono tabular-nums" style={{ color: "var(--color-btc-text-dim)" }}>
                            {count} UTXO{count !== 1 ? "s" : ""}
                          </span>
                        )}
                      </div>
                    </div>

                    <button
                      onClick={() => copy(addr)}
                      className="shrink-0 rounded p-1.5 transition-opacity"
                      style={{
                        background: isCopied ? "rgba(63, 185, 80, 0.15)" : "rgba(255, 255, 255, 0.06)",
                        color: isCopied ? "var(--color-btc-green)" : "var(--color-btc-text-muted)",
                        opacity: isCopied ? 1 : undefined,
                      }}
                      title="Copy"
                    >
                      {isCopied ? <CheckIcon size={12} /> : <CopyIcon size={12} />}
                    </button>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
