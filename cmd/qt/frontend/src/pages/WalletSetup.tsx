import { useCallback, useState } from "react";
import { CreateWallet, ImportWallet } from "../../wailsjs/go/main/App";

type Step = "choose" | "creating" | "show-mnemonic" | "confirm-mnemonic" | "import" | "importing" | "done";

export function WalletSetup({ onComplete }: { onComplete: () => void }) {
  const [step, setStep] = useState<Step>("choose");
  const [mnemonic, setMnemonic] = useState("");
  const [address, setAddress] = useState("");
  const [error, setError] = useState("");
  const [importInput, setImportInput] = useState("");
  const [confirmInput, setConfirmInput] = useState("");
  const [mnemonicCopied, setMnemonicCopied] = useState(false);
  const [confirmAcknowledge, setConfirmAcknowledge] = useState(false);

  const handleCreate = useCallback(async () => {
    setStep("creating");
    setError("");
    try {
      const result = await CreateWallet();
      setMnemonic(result.mnemonic as string);
      setAddress(result.address as string);
      setStep("show-mnemonic");
    } catch (err: unknown) {
      setError(String(err));
      setStep("choose");
    }
  }, []);

  const handleConfirmMnemonic = useCallback(() => {
    const words = mnemonic.split(/\s+/);
    const inputWords = confirmInput.trim().split(/\s+/);
    if (words.length !== inputWords.length || words.some((w, i) => w !== inputWords[i])) {
      setError("The phrase you entered does not match. Please try again.");
      return;
    }
    setError("");
    setStep("done");
    setTimeout(onComplete, 600);
  }, [mnemonic, confirmInput, onComplete]);

  const handleImport = useCallback(async () => {
    setStep("importing");
    setError("");
    try {
      const result = await ImportWallet(importInput.trim());
      setAddress(result.address as string);
      setStep("done");
      setTimeout(onComplete, 600);
    } catch (err: unknown) {
      setError(String(err));
      setStep("import");
    }
  }, [importInput, onComplete]);

  const copyMnemonic = useCallback(() => {
    navigator.clipboard.writeText(mnemonic).then(() => {
      setMnemonicCopied(true);
      setTimeout(() => setMnemonicCopied(false), 2000);
    });
  }, [mnemonic]);

  const cardStyle: React.CSSProperties = {
    background: "var(--color-btc-card)",
    border: "1px solid var(--color-btc-border)",
    borderRadius: "16px",
    padding: "32px",
    maxWidth: "540px",
    width: "100%",
  };

  const btnPrimary = "rounded-lg px-6 py-3 text-sm font-semibold transition-colors";
  const btnPrimaryStyle: React.CSSProperties = {
    background: "var(--color-btc-gold)",
    color: "#000",
  };
  const btnSecondary = "rounded-lg px-6 py-3 text-sm font-semibold transition-colors";
  const btnSecondaryStyle: React.CSSProperties = {
    background: "transparent",
    color: "var(--color-btc-text)",
    border: "1px solid var(--color-btc-border)",
  };

  return (
    <div
      className="flex h-full min-h-screen items-center justify-center p-6"
      style={{ background: "var(--color-btc-deep)" }}
    >
      {step === "choose" && (
        <div style={cardStyle} className="flex flex-col items-center gap-6">
          <div
            className="flex h-14 w-14 items-center justify-center rounded-full text-2xl font-bold"
            style={{ background: "rgba(247, 147, 26, 0.15)", color: "var(--color-btc-gold)" }}
          >
            F
          </div>
          <h1 className="text-xl font-bold tracking-tight" style={{ color: "var(--color-btc-text)" }}>
            Welcome to Fairchain
          </h1>
          <p className="text-center text-sm leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
            Create a new wallet to get started, or import an existing wallet using your recovery phrase.
          </p>
          {error && (
            <p className="text-sm font-medium" style={{ color: "var(--color-btc-red)" }}>{error}</p>
          )}
          <div className="flex w-full flex-col gap-3">
            <button className={btnPrimary} style={btnPrimaryStyle} onClick={handleCreate}>
              Create new wallet
            </button>
            <button className={btnSecondary} style={btnSecondaryStyle} onClick={() => { setStep("import"); setError(""); }}>
              Import existing wallet
            </button>
          </div>
        </div>
      )}

      {step === "creating" && (
        <div style={cardStyle} className="flex flex-col items-center gap-6">
          <svg className="h-8 w-8 animate-spin" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-gold)" strokeWidth={2}>
            <path d="M21 12a9 9 0 11-6.219-8.56" />
          </svg>
          <p className="text-sm" style={{ color: "var(--color-btc-text-muted)" }}>Creating your wallet...</p>
        </div>
      )}

      {step === "show-mnemonic" && (
        <div style={cardStyle} className="flex flex-col gap-5">
          <h2 className="text-lg font-bold" style={{ color: "var(--color-btc-text)" }}>
            Back up your recovery phrase
          </h2>
          <div
            className="rounded-lg p-4"
            style={{
              background: "rgba(245, 158, 11, 0.08)",
              border: "1px solid rgba(245, 158, 11, 0.25)",
            }}
          >
            <p className="text-xs font-semibold uppercase tracking-wider" style={{ color: "rgb(245, 158, 11)" }}>
              Important
            </p>
            <p className="mt-1 text-sm leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
              Write down these 24 words in order and store them somewhere safe.
              This is the <strong style={{ color: "var(--color-btc-text)" }}>only way</strong> to recover your wallet
              if you lose access to this computer. Never share it with anyone.
            </p>
          </div>
          <div
            className="rounded-lg p-4 font-mono text-sm leading-8"
            style={{
              background: "var(--color-btc-deep)",
              color: "var(--color-btc-text)",
              border: "1px solid var(--color-btc-border)",
              wordSpacing: "0.2em",
            }}
          >
            {mnemonic.split(/\s+/).map((word, i) => (
              <span key={i} className="mr-3 inline-block">
                <span style={{ color: "var(--color-btc-text-dim)", fontSize: "10px" }}>{i + 1}.</span>{" "}
                {word}
              </span>
            ))}
          </div>
          <div className="flex items-center gap-3">
            <button
              className="rounded-lg px-4 py-2 text-xs font-semibold transition-colors"
              style={{
                background: mnemonicCopied ? "rgba(63, 185, 80, 0.15)" : "rgba(88, 166, 255, 0.12)",
                color: mnemonicCopied ? "var(--color-btc-green)" : "var(--color-btc-blue)",
                border: `1px solid ${mnemonicCopied ? "rgba(63, 185, 80, 0.3)" : "rgba(88, 166, 255, 0.25)"}`,
              }}
              onClick={copyMnemonic}
            >
              {mnemonicCopied ? "Copied!" : "Copy to clipboard"}
            </button>
          </div>
          {address && (
            <p className="text-xs" style={{ color: "var(--color-btc-text-dim)" }}>
              Your first address: <code className="font-mono" style={{ color: "var(--color-btc-gold-light)" }}>{address}</code>
            </p>
          )}
          <label className="flex items-start gap-2 text-sm" style={{ color: "var(--color-btc-text-muted)" }}>
            <input
              type="checkbox"
              checked={confirmAcknowledge}
              onChange={(e) => setConfirmAcknowledge(e.target.checked)}
              className="mt-0.5 accent-[var(--color-btc-gold)]"
            />
            I have written down my recovery phrase and stored it securely.
          </label>
          <button
            className={btnPrimary}
            style={{
              ...btnPrimaryStyle,
              opacity: confirmAcknowledge ? 1 : 0.4,
              cursor: confirmAcknowledge ? "pointer" : "not-allowed",
            }}
            disabled={!confirmAcknowledge}
            onClick={() => { setError(""); setStep("confirm-mnemonic"); }}
          >
            Continue
          </button>
        </div>
      )}

      {step === "confirm-mnemonic" && (
        <div style={cardStyle} className="flex flex-col gap-5">
          <h2 className="text-lg font-bold" style={{ color: "var(--color-btc-text)" }}>
            Confirm your recovery phrase
          </h2>
          <p className="text-sm leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
            Type or paste your 24-word recovery phrase below to confirm you have backed it up correctly.
          </p>
          {error && (
            <p className="text-sm font-medium" style={{ color: "var(--color-btc-red)" }}>{error}</p>
          )}
          <textarea
            className="min-h-[120px] w-full resize-y rounded-lg px-4 py-3 font-mono text-sm"
            style={{
              background: "var(--color-btc-deep)",
              color: "var(--color-btc-text)",
              border: "1px solid var(--color-btc-border)",
            }}
            placeholder="Enter your 24-word recovery phrase..."
            value={confirmInput}
            onChange={(e) => setConfirmInput(e.target.value)}
          />
          <div className="flex gap-3">
            <button className={btnSecondary} style={btnSecondaryStyle} onClick={() => { setStep("show-mnemonic"); setError(""); }}>
              Back
            </button>
            <button
              className={btnPrimary}
              style={{
                ...btnPrimaryStyle,
                opacity: confirmInput.trim().split(/\s+/).length >= 24 ? 1 : 0.4,
              }}
              disabled={confirmInput.trim().split(/\s+/).length < 24}
              onClick={handleConfirmMnemonic}
            >
              Verify and finish
            </button>
          </div>
        </div>
      )}

      {step === "import" && (
        <div style={cardStyle} className="flex flex-col gap-5">
          <h2 className="text-lg font-bold" style={{ color: "var(--color-btc-text)" }}>
            Import wallet
          </h2>
          <p className="text-sm leading-relaxed" style={{ color: "var(--color-btc-text-muted)" }}>
            Enter your BIP-39 recovery phrase (typically 12 or 24 words) to restore your wallet.
          </p>
          {error && (
            <p className="text-sm font-medium" style={{ color: "var(--color-btc-red)" }}>{error}</p>
          )}
          <textarea
            className="min-h-[120px] w-full resize-y rounded-lg px-4 py-3 font-mono text-sm"
            style={{
              background: "var(--color-btc-deep)",
              color: "var(--color-btc-text)",
              border: "1px solid var(--color-btc-border)",
            }}
            placeholder="word1 word2 word3 ..."
            value={importInput}
            onChange={(e) => setImportInput(e.target.value)}
          />
          <div className="flex gap-3">
            <button className={btnSecondary} style={btnSecondaryStyle} onClick={() => { setStep("choose"); setError(""); }}>
              Back
            </button>
            <button
              className={btnPrimary}
              style={{
                ...btnPrimaryStyle,
                opacity: importInput.trim().split(/\s+/).length >= 12 ? 1 : 0.4,
              }}
              disabled={importInput.trim().split(/\s+/).length < 12}
              onClick={handleImport}
            >
              Import wallet
            </button>
          </div>
        </div>
      )}

      {step === "importing" && (
        <div style={cardStyle} className="flex flex-col items-center gap-6">
          <svg className="h-8 w-8 animate-spin" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-gold)" strokeWidth={2}>
            <path d="M21 12a9 9 0 11-6.219-8.56" />
          </svg>
          <p className="text-sm" style={{ color: "var(--color-btc-text-muted)" }}>Importing your wallet...</p>
        </div>
      )}

      {step === "done" && (
        <div style={cardStyle} className="flex flex-col items-center gap-5">
          <svg className="h-12 w-12" viewBox="0 0 24 24" fill="none" stroke="var(--color-btc-green)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M22 11.08V12a10 10 0 11-5.93-9.14" />
            <polyline points="22 4 12 14.01 9 11.01" />
          </svg>
          <h2 className="text-lg font-bold" style={{ color: "var(--color-btc-text)" }}>
            Wallet ready
          </h2>
          {address && (
            <p className="text-xs text-center" style={{ color: "var(--color-btc-text-dim)" }}>
              Default address: <code className="font-mono" style={{ color: "var(--color-btc-gold-light)" }}>{address}</code>
            </p>
          )}
          <p className="text-sm" style={{ color: "var(--color-btc-text-muted)" }}>Starting node...</p>
        </div>
      )}
    </div>
  );
}
