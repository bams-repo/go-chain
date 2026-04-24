import { useCallback, useEffect, useRef, useState } from "react";
import { useCoinInfo } from "@/hooks/useCoinInfo";
import {
  GetBalance,
  SendToAddress,
  ValidateAddress,
  GetAddressBook,
} from "../../../wailsjs/go/main/App";

type Step = "form" | "confirm" | "success" | "error";

function BookIcon({ size = 14 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="M4 19.5v-15A2.5 2.5 0 016.5 2H20v20H6.5a2.5 2.5 0 010-5H20" />
    </svg>
  );
}

function AddressBookDropdown({
  entries,
  onSelect,
  onClose,
}: {
  entries: { address: string; label: string }[];
  onSelect: (address: string) => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [onClose]);

  const [filter, setFilter] = useState("");
  const filtered = entries.filter((e) => {
    if (!filter.trim()) return true;
    const q = filter.toLowerCase();
    return e.address.toLowerCase().includes(q) || e.label.toLowerCase().includes(q);
  });

  return (
    <div
      ref={ref}
      className="absolute left-0 right-0 top-full z-50 mt-1 flex max-h-52 flex-col overflow-hidden rounded-lg"
      style={{
        background: "var(--color-btc-surface)",
        border: "1px solid var(--color-btc-border)",
        boxShadow: "0 8px 24px rgba(0,0,0,0.5)",
      }}
    >
      <div className="shrink-0 px-2 pt-2 pb-1">
        <input
          type="text"
          placeholder="Filter address book..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          autoFocus
          spellCheck={false}
          className="w-full rounded px-2 py-1.5 text-[11px] outline-none"
          style={{
            background: "var(--color-btc-deep)",
            color: "var(--color-btc-text)",
            border: "1px solid var(--color-btc-border)",
          }}
          onFocus={(e) => (e.target.style.borderColor = "var(--color-btc-gold)")}
          onBlur={(e) => (e.target.style.borderColor = "var(--color-btc-border)")}
        />
      </div>
      <div className="flex-1 overflow-y-auto overscroll-contain px-1 pb-1">
        {filtered.length === 0 ? (
          <p className="px-2 py-3 text-center text-[11px]" style={{ color: "var(--color-btc-text-dim)" }}>
            {entries.length === 0 ? "No labeled addresses. Add labels in Receive or Transactions." : "No matches"}
          </p>
        ) : (
          filtered.map((e) => (
            <button
              key={e.address}
              onClick={() => { onSelect(e.address); onClose(); }}
              className="flex w-full flex-col gap-0.5 rounded px-2 py-1.5 text-left transition-colors"
              style={{ color: "var(--color-btc-text)" }}
              onMouseEnter={(ev) => (ev.currentTarget.style.background = "var(--color-overlay-hover)")}
              onMouseLeave={(ev) => (ev.currentTarget.style.background = "transparent")}
            >
              <span className="text-[11px] font-semibold" style={{ color: "var(--color-btc-gold-light)" }}>
                {e.label}
              </span>
              <code className="truncate text-[10px] font-mono" style={{ color: "var(--color-btc-text-muted)" }}>
                {e.address}
              </code>
            </button>
          ))
        )}
      </div>
    </div>
  );
}

export function Send() {
  const coinInfo = useCoinInfo();
  const [step, setStep] = useState<Step>("form");

  const [address, setAddress] = useState("");
  const [amount, setAmount] = useState("");
  const [addressError, setAddressError] = useState("");
  const [amountError, setAmountError] = useState("");
  const [isMine, setIsMine] = useState(false);

  const [confirmed, setConfirmed] = useState(0);
  const [sending, setSending] = useState(false);
  const [txid, setTxid] = useState("");
  const [sendError, setSendError] = useState("");

  const [addressBook, setAddressBook] = useState<Record<string, string>>({});
  const [showBook, setShowBook] = useState(false);

  const addressRef = useRef<HTMLInputElement>(null);
  const validateTimer = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    GetBalance().then((b) => setConfirmed(b.confirmed as number)).catch(() => {});
  }, [step]);

  useEffect(() => {
    GetAddressBook().then((book) => { if (book) setAddressBook(book); }).catch(() => {});
  }, []);

  const resolvedLabel = addressBook[address.trim()] || "";

  const validateAddress = useCallback(
    (addr: string) => {
      clearTimeout(validateTimer.current);
      if (!addr.trim()) { setAddressError(""); setIsMine(false); return; }
      validateTimer.current = setTimeout(() => {
        ValidateAddress(addr.trim())
          .then((result) => {
            if (!result.isvalid) { setAddressError("Invalid address"); setIsMine(false); }
            else { setAddressError(""); setIsMine(!!result.ismine); }
          })
          .catch(() => { setAddressError("Could not validate address"); setIsMine(false); });
      }, 300);
    },
    [],
  );

  const handleAddressChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    setAddress(val);
    validateAddress(val);
  };

  const handleAmountChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    if (val !== "" && !/^\d*\.?\d*$/.test(val)) return;
    setAmount(val);
    setAmountError("");
  };

  const handleSetMax = () => {
    if (confirmed > 0) {
      const maxAmount = Math.max(0, confirmed - 0.00001);
      setAmount(maxAmount.toFixed(coinInfo.decimals > 8 ? 8 : coinInfo.decimals));
      setAmountError("");
    }
  };

  const canProceed = (): boolean => {
    let ok = true;
    if (!address.trim()) { setAddressError("Address is required"); ok = false; }
    if (addressError && address.trim()) ok = false;
    const parsed = parseFloat(amount);
    if (!amount || isNaN(parsed) || parsed <= 0) { setAmountError("Enter a valid amount"); ok = false; }
    else if (parsed > confirmed) { setAmountError("Insufficient funds"); ok = false; }
    return ok;
  };

  const handleReview = () => { if (canProceed()) setStep("confirm"); };

  const handleConfirmSend = async () => {
    setSending(true);
    setSendError("");
    try {
      const result = await SendToAddress(address.trim(), parseFloat(amount));
      setTxid(result.txid as string);
      setStep("success");
    } catch (err: any) {
      setSendError(typeof err === "string" ? err : err?.message || "Transaction failed");
      setStep("error");
    } finally {
      setSending(false);
    }
  };

  const handleReset = () => {
    setStep("form");
    setAddress("");
    setAmount("");
    setAddressError("");
    setAmountError("");
    setIsMine(false);
    setTxid("");
    setSendError("");
    setTimeout(() => addressRef.current?.focus(), 100);
  };

  const parsedAmount = parseFloat(amount) || 0;

  const labelStyle: React.CSSProperties = {
    color: "var(--color-btc-text-muted)", fontSize: "11px", fontWeight: 600,
    textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: "6px",
  };
  const inputStyle: React.CSSProperties = {
    width: "100%", background: "var(--color-btc-deep)", color: "var(--color-btc-text)",
    border: "1px solid var(--color-btc-border)", borderRadius: "8px", padding: "10px 12px",
    fontSize: "13px", fontFamily: "var(--font-sans)", outline: "none", transition: "border-color 0.2s",
  };
  const btnPrimary: React.CSSProperties = {
    background: "linear-gradient(135deg, #f7931a 0%, #c67200 100%)", color: "#fff",
    border: "none", borderRadius: "8px", padding: "10px 24px", fontSize: "13px",
    fontWeight: 600, cursor: "pointer", letterSpacing: "0.02em", transition: "opacity 0.2s",
  };
  const btnSecondary: React.CSSProperties = {
    background: "var(--color-btc-card)", color: "var(--color-btc-text)",
    border: "1px solid var(--color-btc-border)", borderRadius: "8px", padding: "10px 24px",
    fontSize: "13px", fontWeight: 600, cursor: "pointer", transition: "background 0.2s",
  };

  const bookEntries = Object.entries(addressBook)
    .filter(([, lbl]) => lbl)
    .map(([addr, lbl]) => ({ address: addr, label: lbl }))
    .sort((a, b) => a.label.localeCompare(b.label));

  if (step === "form") {
    return (
      <div className="flex h-full flex-col gap-3">
        {/* Balance */}
        <div
          className="btc-noise btc-glow-active relative overflow-hidden rounded-xl p-5"
          style={{ background: "linear-gradient(135deg, var(--color-btc-card) 0%, var(--color-btc-surface) 100%)", border: "1px solid var(--color-btc-border)" }}
        >
          <div className="absolute -right-8 -top-8 h-32 w-32 rounded-full opacity-[0.04]" style={{ background: "var(--color-btc-gold)" }} />
          <div className="relative z-10">
            <h3 className="mb-1 text-[10px] font-semibold uppercase tracking-wider" style={{ color: "var(--color-btc-text-dim)" }}>Available Balance</h3>
            <p className="text-2xl font-bold" style={{ color: "var(--color-btc-text)" }}>
              {confirmed.toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)}{" "}
              <span className="text-base font-medium" style={{ color: "var(--color-btc-gold)" }}>{coinInfo.ticker}</span>
            </p>
          </div>
        </div>

        {/* Send form */}
        <div
          className="btc-glow flex flex-col gap-4 rounded-xl p-5"
          style={{ background: "var(--color-btc-card)", border: "1px solid var(--color-btc-border)" }}
        >
          <h2 className="text-sm font-semibold" style={{ color: "var(--color-btc-text)" }}>Send {coinInfo.ticker}</h2>

          {/* Address field with address book */}
          <div style={{ position: "relative" }}>
            <div className="flex items-center justify-between">
              <label style={labelStyle}>Recipient Address</label>
              <button
                type="button"
                onClick={() => setShowBook(!showBook)}
                className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide transition-colors"
                style={{
                  background: showBook ? "rgba(247, 147, 26, 0.15)" : "rgba(88, 166, 255, 0.12)",
                  color: showBook ? "var(--color-btc-gold)" : "var(--color-btc-blue)",
                  border: `1px solid ${showBook ? "rgba(247, 147, 26, 0.3)" : "rgba(88, 166, 255, 0.25)"}`,
                  marginBottom: "6px",
                }}
              >
                <BookIcon size={11} /> Address Book
              </button>
            </div>
            <input
              ref={addressRef}
              type="text"
              placeholder={`Enter ${coinInfo.ticker} address`}
              value={address}
              onChange={handleAddressChange}
              spellCheck={false}
              autoComplete="off"
              style={{
                ...inputStyle,
                fontFamily: "monospace",
                fontSize: "12px",
                borderColor: addressError ? "var(--color-btc-red)" : address && !addressError ? "var(--color-btc-green)" : "var(--color-btc-border)",
              }}
              onFocus={(e) => (e.target.style.borderColor = addressError ? "var(--color-btc-red)" : "var(--color-btc-gold)")}
              onBlur={(e) => (e.target.style.borderColor = addressError ? "var(--color-btc-red)" : address && !addressError ? "var(--color-btc-green)" : "var(--color-btc-border)")}
            />
            {showBook && (
              <AddressBookDropdown
                entries={bookEntries}
                onSelect={(a) => { setAddress(a); validateAddress(a); }}
                onClose={() => setShowBook(false)}
              />
            )}
            {addressError && (
              <p className="mt-1 text-[11px] font-medium" style={{ color: "var(--color-btc-red)" }}>{addressError}</p>
            )}
            {resolvedLabel && !addressError && address.trim() && (
              <p className="mt-1 text-[11px] font-medium" style={{ color: "var(--color-btc-gold-light)" }}>
                {resolvedLabel}
                {isMine && <span style={{ color: "var(--color-btc-gold)", marginLeft: 6 }}>(your address)</span>}
              </p>
            )}
            {!resolvedLabel && isMine && !addressError && (
              <p className="mt-1 text-[11px] font-medium" style={{ color: "var(--color-btc-gold)" }}>This is your own address</p>
            )}
          </div>

          {/* Amount */}
          <div>
            <label style={labelStyle}>Amount ({coinInfo.ticker})</label>
            <div style={{ position: "relative" }}>
              <input
                type="text"
                inputMode="decimal"
                placeholder="0.00000000"
                value={amount}
                onChange={handleAmountChange}
                style={{ ...inputStyle, paddingRight: "60px", borderColor: amountError ? "var(--color-btc-red)" : "var(--color-btc-border)" }}
                onFocus={(e) => (e.target.style.borderColor = amountError ? "var(--color-btc-red)" : "var(--color-btc-gold)")}
                onBlur={(e) => (e.target.style.borderColor = amountError ? "var(--color-btc-red)" : "var(--color-btc-border)")}
              />
              <button
                type="button"
                onClick={handleSetMax}
                style={{
                  position: "absolute", right: "8px", top: "50%", transform: "translateY(-50%)",
                  background: "rgba(247, 147, 26, 0.12)", color: "var(--color-btc-gold)",
                  border: "1px solid rgba(247, 147, 26, 0.25)", borderRadius: "4px",
                  padding: "2px 8px", fontSize: "10px", fontWeight: 700, cursor: "pointer",
                  letterSpacing: "0.06em", textTransform: "uppercase",
                }}
              >
                Max
              </button>
            </div>
            {amountError && <p className="mt-1 text-[11px] font-medium" style={{ color: "var(--color-btc-red)" }}>{amountError}</p>}
          </div>

          {/* Fee */}
          <div className="rounded-lg px-3 py-2" style={{ background: "var(--color-btc-deep)", border: "1px solid var(--color-btc-border)" }}>
            <div className="flex items-center justify-between text-[11px]">
              <span style={{ color: "var(--color-btc-text-muted)" }}>Network Fee</span>
              <span style={{ color: "var(--color-btc-text)" }}>~1 sat/byte (auto)</span>
            </div>
          </div>

          <button
            type="button"
            onClick={handleReview}
            disabled={!address.trim() || !amount || !!addressError}
            style={{ ...btnPrimary, opacity: !address.trim() || !amount || !!addressError ? 0.5 : 1, cursor: !address.trim() || !amount || !!addressError ? "not-allowed" : "pointer" }}
          >
            Review Transaction
          </button>
        </div>
      </div>
    );
  }

  if (step === "confirm") {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4 px-4">
        <div className="btc-glow w-full max-w-md rounded-xl p-6" style={{ background: "var(--color-btc-card)", border: "1px solid var(--color-btc-border)" }}>
          <h2 className="mb-4 text-center text-sm font-semibold uppercase tracking-wider" style={{ color: "var(--color-btc-gold)" }}>Confirm Transaction</h2>
          <div className="flex flex-col gap-3">
            <div>
              <p className="text-[10px] font-semibold uppercase tracking-wider" style={{ color: "var(--color-btc-text-dim)" }}>Sending</p>
              <p className="text-xl font-bold" style={{ color: "var(--color-btc-text)" }}>
                {parsedAmount.toFixed(coinInfo.decimals > 8 ? 8 : coinInfo.decimals)}{" "}
                <span className="text-sm font-medium" style={{ color: "var(--color-btc-gold)" }}>{coinInfo.ticker}</span>
              </p>
            </div>
            <div className="h-px" style={{ background: "var(--color-btc-border)" }} />
            <div>
              <p className="text-[10px] font-semibold uppercase tracking-wider" style={{ color: "var(--color-btc-text-dim)" }}>To</p>
              {resolvedLabel && (
                <p className="mt-0.5 text-[12px] font-semibold" style={{ color: "var(--color-btc-gold-light)" }}>{resolvedLabel}</p>
              )}
              <code className="mt-1 block break-all rounded-lg px-3 py-2 text-[11px] font-mono" style={{ background: "var(--color-btc-deep)", color: "var(--color-btc-gold-light)", border: "1px solid var(--color-btc-border)" }}>
                {address}
              </code>
              {isMine && <p className="mt-1 text-[10px] font-medium" style={{ color: "var(--color-btc-gold)" }}>This is your own address</p>}
            </div>
            <div className="h-px" style={{ background: "var(--color-btc-border)" }} />
            <div className="flex items-center justify-between text-xs">
              <span style={{ color: "var(--color-btc-text-muted)" }}>Fee</span>
              <span style={{ color: "var(--color-btc-text)" }}>~1 sat/byte</span>
            </div>
            <div className="flex items-center justify-between text-xs">
              <span style={{ color: "var(--color-btc-text-muted)" }}>Remaining Balance</span>
              <span style={{ color: "var(--color-btc-text)" }}>
                ~{(confirmed - parsedAmount).toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)} {coinInfo.ticker}
              </span>
            </div>
          </div>
          <div className="mt-5 flex gap-3">
            <button type="button" onClick={() => setStep("form")} disabled={sending} style={{ ...btnSecondary, flex: 1 }}>Back</button>
            <button type="button" onClick={handleConfirmSend} disabled={sending} style={{ ...btnPrimary, flex: 1, opacity: sending ? 0.6 : 1 }}>
              {sending ? "Sending..." : "Send Now"}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (step === "success") {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4 px-4">
        <div className="btc-glow w-full max-w-md rounded-xl p-6 text-center" style={{ background: "var(--color-btc-card)", border: "1px solid var(--color-btc-border)" }}>
          <svg className="mx-auto mb-3 h-12 w-12" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-green)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M22 11.08V12a10 10 0 11-5.93-9.14" />
            <polyline points="22 4 12 14.01 9 11.01" />
          </svg>
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-wider" style={{ color: "var(--color-btc-green)" }}>Transaction Sent</h2>
          <p className="mb-3 text-lg font-bold" style={{ color: "var(--color-btc-text)" }}>
            {parsedAmount.toFixed(coinInfo.decimals > 8 ? 8 : coinInfo.decimals)}{" "}
            <span className="text-sm" style={{ color: "var(--color-btc-gold)" }}>{coinInfo.ticker}</span>
          </p>
          {resolvedLabel && (
            <p className="mb-2 text-[11px] font-semibold" style={{ color: "var(--color-btc-gold-light)" }}>to {resolvedLabel}</p>
          )}
          <div className="mb-4">
            <p className="text-[10px] font-semibold uppercase tracking-wider" style={{ color: "var(--color-btc-text-dim)" }}>Transaction ID</p>
            <code className="mt-1 block break-all rounded-lg px-3 py-2 text-[11px] font-mono" style={{ background: "var(--color-btc-deep)", color: "var(--color-btc-gold-light)", border: "1px solid var(--color-btc-border)" }}>
              {txid}
            </code>
          </div>
          <button type="button" onClick={handleReset} style={btnPrimary}>Send Another</button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 px-4">
      <div className="btc-glow w-full max-w-md rounded-xl p-6 text-center" style={{ background: "var(--color-btc-card)", border: "1px solid var(--color-btc-border)" }}>
        <svg className="mx-auto mb-3 h-12 w-12" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-red)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="12" r="10" />
          <line x1="15" y1="9" x2="9" y2="15" />
          <line x1="9" y1="9" x2="15" y2="15" />
        </svg>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wider" style={{ color: "var(--color-btc-red)" }}>Transaction Failed</h2>
        <p className="mb-4 text-xs" style={{ color: "var(--color-btc-text-muted)" }}>{sendError}</p>
        <div className="flex justify-center gap-3">
          <button type="button" onClick={handleReset} style={btnSecondary}>Start Over</button>
          <button type="button" onClick={() => setStep("confirm")} style={btnPrimary}>Try Again</button>
        </div>
      </div>
    </div>
  );
}
