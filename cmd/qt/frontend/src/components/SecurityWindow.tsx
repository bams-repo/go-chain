import { useCallback, useEffect, useState } from "react";
import { walletRpc } from "@/lib/walletRpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Copy, RefreshCw } from "lucide-react";

type WalletInfo = {
  encrypted?: boolean;
  locked?: boolean;
  keypoolsize?: number;
  balance?: number;
  hdseedid?: string;
  unlocked_until?: number;
};

type DumpWalletResult = {
  mnemonic?: string;
  addresses?: string[];
  keypoolsize?: number;
};

export type SecurityHighlight = "encrypt" | "passphrase" | null;

interface SecurityWindowProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  highlight?: SecurityHighlight;
}

export function SecurityWindow({ open, onOpenChange, highlight }: SecurityWindowProps) {
  const [info, setInfo] = useState<WalletInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState<{ type: "ok" | "err"; text: string } | null>(null);

  const [unlockPass, setUnlockPass] = useState("");
  const [unlockTimeout, setUnlockTimeout] = useState("600");

  const [encPass, setEncPass] = useState("");
  const [encPass2, setEncPass2] = useState("");

  const [oldPass, setOldPass] = useState("");
  const [newPass, setNewPass] = useState("");
  const [newPass2, setNewPass2] = useState("");

  const [backupName, setBackupName] = useState("wallet-backup.json");

  const [dumpData, setDumpData] = useState<DumpWalletResult | null>(null);

  const [importWif, setImportWif] = useState("");
  const [dumpAddr, setDumpAddr] = useState("");
  const [exportedWif, setExportedWif] = useState("");

  const refreshInfo = useCallback(async () => {
    try {
      const w = await walletRpc<WalletInfo>("getwalletinfo", []);
      setInfo(w);
    } catch (e) {
      setInfo(null);
      setMessage({ type: "err", text: e instanceof Error ? e.message : String(e) });
    }
  }, []);

  useEffect(() => {
    if (!open) {
      return;
    }
    setMessage(null);
    void refreshInfo();
  }, [open, refreshInfo]);

  useEffect(() => {
    if (!open || !highlight) {
      return;
    }
    const t = window.setTimeout(() => {
      document.getElementById(`sec-${highlight}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
    }, 200);
    return () => window.clearTimeout(t);
  }, [open, highlight]);

  const flash = (type: "ok" | "err", text: string) => {
    setMessage({ type, text });
  };

  const run = async (label: string, fn: () => Promise<void>) => {
    setLoading(true);
    setMessage(null);
    try {
      await fn();
      flash("ok", `${label} succeeded.`);
      await refreshInfo();
    } catch (e) {
      flash("err", e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  const encrypted = !!info?.encrypted;
  const locked = !!info?.locked;

  const sectionCard =
    "bg-card/80 shadow-sm ring-1 ring-border/70 backdrop-blur-sm";

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex h-full w-full max-w-xl flex-col gap-0 p-0 lg:max-w-2xl"
      >
        <SheetHeader className="shrink-0 space-y-3 border-b border-border/80 px-6 pb-6 pt-6 text-left">
          <SheetTitle className="text-lg font-semibold tracking-tight">Wallet security</SheetTitle>
          <SheetDescription className="text-sm leading-relaxed text-muted-foreground">
            Encryption, recovery phrase, backup, and private key tools. Never share your passphrase or seed with
            anyone.
          </SheetDescription>
        </SheetHeader>

        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain px-6 py-6">
          <div className="mx-auto flex w-full max-w-full flex-col gap-8 pb-10">
            {message && (
              <div
                role="status"
                className={
                  message.type === "ok"
                    ? "rounded-lg border border-emerald-800/50 bg-emerald-950/35 px-4 py-3 text-sm leading-relaxed text-emerald-100"
                    : "rounded-lg border border-red-800/50 bg-red-950/35 px-4 py-3 text-sm leading-relaxed text-red-100"
                }
              >
                {message.text}
              </div>
            )}

            <Card className={sectionCard}>
              <CardHeader className="gap-3 pb-0">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0 space-y-1">
                    <CardTitle>Status</CardTitle>
                    <CardDescription className="text-sm leading-relaxed">
                      Current wallet protection state.
                    </CardDescription>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={loading}
                    className="shrink-0"
                    onClick={() => void refreshInfo()}
                  >
                    <RefreshCw className="mr-2 size-4" />
                    Refresh
                  </Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-3 pt-4 text-sm leading-relaxed text-muted-foreground">
                {info ? (
                  <dl className="grid gap-3">
                    <div className="flex flex-col gap-0.5 sm:flex-row sm:items-baseline sm:gap-3">
                      <dt className="shrink-0 text-foreground/85 font-medium">Encrypted</dt>
                      <dd>{encrypted ? "Yes" : "No"}</dd>
                    </div>
                    <div className="flex flex-col gap-0.5 sm:flex-row sm:items-baseline sm:gap-3">
                      <dt className="shrink-0 text-foreground/85 font-medium">Locked</dt>
                      <dd>{encrypted ? (locked ? "Yes" : "No (unlocked)") : "—"}</dd>
                    </div>
                    <div className="flex flex-col gap-0.5 sm:flex-row sm:items-baseline sm:gap-3">
                      <dt className="shrink-0 text-foreground/85 font-medium">Keys / addresses</dt>
                      <dd>{info.keypoolsize ?? "—"}</dd>
                    </div>
                    <div className="flex flex-col gap-1">
                      <dt className="text-foreground/85 font-medium">Default receive</dt>
                      <dd className="break-all font-mono text-xs text-foreground/90">{info.hdseedid || "—"}</dd>
                    </div>
                  </dl>
                ) : (
                  <p>Could not load wallet info.</p>
                )}
              </CardContent>
            </Card>

            <Card className={sectionCard} id="sec-unlock">
              <CardHeader className="gap-3 pb-0">
                <CardTitle>Unlock / lock</CardTitle>
                <CardDescription className="text-sm leading-relaxed">
                  Unlock to spend, sign, or reveal secrets. Lock when finished.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-5 pt-5">
                <div className="space-y-2">
                  <label htmlFor="sec-unlock-pass" className="text-sm font-medium text-foreground/90">
                    Passphrase
                  </label>
                  <Input
                    id="sec-unlock-pass"
                    type="password"
                    autoComplete="current-password"
                    className="h-10"
                    value={unlockPass}
                    onChange={(e) => setUnlockPass(e.target.value)}
                    disabled={!encrypted || loading}
                    placeholder={encrypted ? "Wallet passphrase" : "Wallet is not encrypted"}
                  />
                </div>
                <div className="space-y-2">
                  <label htmlFor="sec-unlock-timeout" className="text-sm font-medium text-foreground/90">
                    Unlock for (seconds)
                  </label>
                  <Input
                    id="sec-unlock-timeout"
                    type="number"
                    min={1}
                    className="h-10 max-w-[12rem]"
                    value={unlockTimeout}
                    onChange={(e) => setUnlockTimeout(e.target.value)}
                    disabled={!encrypted || loading}
                  />
                </div>
                <div className="flex flex-wrap gap-3 pt-1">
                  <Button
                    type="button"
                    disabled={!encrypted || loading || !unlockPass}
                    onClick={() =>
                      void run("Unlock", async () => {
                        const t = Math.max(1, parseInt(unlockTimeout, 10) || 600);
                        await walletRpc("walletpassphrase", [unlockPass, t]);
                        setUnlockPass("");
                      })
                    }
                  >
                    Unlock
                  </Button>
                  <Button
                    type="button"
                    variant="secondary"
                    disabled={!encrypted || loading || locked}
                    onClick={() =>
                      void run("Lock", async () => {
                        await walletRpc("walletlock", []);
                      })
                    }
                  >
                    Lock now
                  </Button>
                </div>
              </CardContent>
            </Card>

            <Card className={sectionCard} id="sec-encrypt">
              <CardHeader className="gap-3 pb-0">
                <CardTitle>Encrypt wallet</CardTitle>
                <CardDescription className="text-sm leading-relaxed">
                  Protect this wallet with a passphrase. You will need it to spend and to view the recovery phrase.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-5">
                <div className="space-y-2">
                  <label htmlFor="sec-enc-1" className="text-sm font-medium text-foreground/90">
                    New passphrase
                  </label>
                  <Input
                    id="sec-enc-1"
                    type="password"
                    autoComplete="new-password"
                    className="h-10"
                    value={encPass}
                    onChange={(e) => setEncPass(e.target.value)}
                    disabled={encrypted || loading}
                    placeholder="Choose a strong passphrase"
                  />
                </div>
                <div className="space-y-2">
                  <label htmlFor="sec-enc-2" className="text-sm font-medium text-foreground/90">
                    Confirm passphrase
                  </label>
                  <Input
                    id="sec-enc-2"
                    type="password"
                    autoComplete="new-password"
                    className="h-10"
                    value={encPass2}
                    onChange={(e) => setEncPass2(e.target.value)}
                    disabled={encrypted || loading}
                    placeholder="Repeat passphrase"
                  />
                </div>
                <div className="pt-1">
                  <Button
                    type="button"
                    disabled={encrypted || loading || !encPass || encPass !== encPass2}
                    onClick={() =>
                      void run("Encrypt wallet", async () => {
                        await walletRpc("encryptwallet", [encPass]);
                        setEncPass("");
                        setEncPass2("");
                      })
                    }
                  >
                    Encrypt wallet
                  </Button>
                </div>
              </CardContent>
            </Card>

            <Card className={sectionCard} id="sec-passphrase">
              <CardHeader className="gap-3 pb-0">
                <CardTitle>Change passphrase</CardTitle>
                <CardDescription className="text-sm leading-relaxed">
                  Only for encrypted wallets. Requires the current passphrase.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-5">
                <div className="space-y-2">
                  <label htmlFor="sec-old-pass" className="text-sm font-medium text-foreground/90">
                    Current passphrase
                  </label>
                  <Input
                    id="sec-old-pass"
                    type="password"
                    className="h-10"
                    value={oldPass}
                    onChange={(e) => setOldPass(e.target.value)}
                    disabled={!encrypted || loading}
                    placeholder="Current passphrase"
                  />
                </div>
                <div className="space-y-2">
                  <label htmlFor="sec-new-pass" className="text-sm font-medium text-foreground/90">
                    New passphrase
                  </label>
                  <Input
                    id="sec-new-pass"
                    type="password"
                    autoComplete="new-password"
                    className="h-10"
                    value={newPass}
                    onChange={(e) => setNewPass(e.target.value)}
                    disabled={!encrypted || loading}
                    placeholder="New passphrase"
                  />
                </div>
                <div className="space-y-2">
                  <label htmlFor="sec-new-pass2" className="text-sm font-medium text-foreground/90">
                    Confirm new passphrase
                  </label>
                  <Input
                    id="sec-new-pass2"
                    type="password"
                    autoComplete="new-password"
                    className="h-10"
                    value={newPass2}
                    onChange={(e) => setNewPass2(e.target.value)}
                    disabled={!encrypted || loading}
                    placeholder="Repeat new passphrase"
                  />
                </div>
                <div className="pt-1">
                  <Button
                    type="button"
                    disabled={
                      !encrypted || loading || !oldPass || !newPass || newPass !== newPass2 || newPass.length < 1
                    }
                    onClick={() =>
                      void run("Change passphrase", async () => {
                        await walletRpc("walletpassphrasechange", [oldPass, newPass]);
                        setOldPass("");
                        setNewPass("");
                        setNewPass2("");
                      })
                    }
                  >
                    Change passphrase
                  </Button>
                </div>
              </CardContent>
            </Card>

            <Card className={sectionCard}>
              <CardHeader className="gap-3 pb-0">
                <CardTitle>Backup wallet file</CardTitle>
                <CardDescription className="text-sm leading-relaxed">
                  Saves a copy under your data directory in the{" "}
                  <code className="rounded bg-muted px-1.5 py-0.5 text-xs">backups</code> folder.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-5">
                <div className="space-y-2">
                  <label htmlFor="sec-backup-name" className="text-sm font-medium text-foreground/90">
                    Backup file name
                  </label>
                  <Input
                    id="sec-backup-name"
                    className="h-10 font-mono text-sm"
                    value={backupName}
                    onChange={(e) => setBackupName(e.target.value)}
                    disabled={loading}
                  />
                </div>
                <div className="pt-1">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={loading || !backupName.trim()}
                    onClick={() =>
                      void run("Backup", async () => {
                        await walletRpc("backupwallet", [backupName.trim()]);
                      })
                    }
                  >
                    Save backup
                  </Button>
                </div>
              </CardContent>
            </Card>

            <Card className={`${sectionCard} border-amber-900/35 bg-amber-950/15 ring-amber-900/25`}>
              <CardHeader className="gap-3 pb-0">
                <CardTitle className="text-amber-50">Recovery phrase</CardTitle>
                <CardDescription className="text-sm leading-relaxed text-amber-100/85">
                  Anyone with this phrase controls your funds. Unlock the wallet first if it is encrypted. Store it
                  offline only.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-5">
                <Button
                  type="button"
                  variant="destructive"
                  disabled={loading || (encrypted && locked)}
                  onClick={() =>
                    void run("Reveal recovery data", async () => {
                      const d = await walletRpc<DumpWalletResult>("dumpwallet", []);
                      setDumpData(d);
                    })
                  }
                >
                  Show recovery phrase &amp; addresses
                </Button>
                {dumpData?.mnemonic && (
                  <div className="space-y-4 rounded-lg border border-border/80 bg-background/50 p-4">
                    <div className="flex items-start justify-between gap-4">
                      <p className="font-mono text-sm leading-7 break-words text-foreground">{dumpData.mnemonic}</p>
                      <Button
                        type="button"
                        size="icon-sm"
                        variant="ghost"
                        className="shrink-0"
                        title="Copy mnemonic"
                        onClick={() => {
                          void navigator.clipboard.writeText(dumpData.mnemonic ?? "");
                          flash("ok", "Mnemonic copied to clipboard.");
                        }}
                      >
                        <Copy className="size-4" />
                      </Button>
                    </div>
                    <Separator className="bg-border/60" />
                    <p className="text-sm text-muted-foreground">
                      {dumpData.addresses?.length ?? 0} derived address(es). Key pool: {dumpData.keypoolsize ?? "—"}
                    </p>
                  </div>
                )}
              </CardContent>
            </Card>

            <Card className={sectionCard}>
              <CardHeader className="gap-3 pb-0">
                <CardTitle>Import private key</CardTitle>
                <CardDescription className="text-sm leading-relaxed">
                  WIF or hex. Wallet must be unlocked if encrypted.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-5">
                <div className="space-y-2">
                  <label htmlFor="sec-import-wif" className="text-sm font-medium text-foreground/90">
                    Private key
                  </label>
                  <textarea
                    id="sec-import-wif"
                    className="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex min-h-[100px] w-full resize-y rounded-md border px-3 py-3 text-sm leading-relaxed focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
                    value={importWif}
                    onChange={(e) => setImportWif(e.target.value)}
                    disabled={loading || (encrypted && locked)}
                    placeholder="Private key (WIF or hex)"
                  />
                </div>
                <div className="pt-1">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={loading || !importWif.trim() || (encrypted && locked)}
                    onClick={() =>
                      void run("Import key", async () => {
                        await walletRpc<{ address?: string }>("importprivkey", [importWif.trim()]);
                        setImportWif("");
                      })
                    }
                  >
                    Import
                  </Button>
                </div>
              </CardContent>
            </Card>

            <Card className={sectionCard}>
              <CardHeader className="gap-3 pb-0">
                <CardTitle>Export private key</CardTitle>
                <CardDescription className="text-sm leading-relaxed">
                  For a single address you own. Unlock required if encrypted.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-5">
                <div className="space-y-2">
                  <label htmlFor="sec-dump-addr" className="text-sm font-medium text-foreground/90">
                    Your address
                  </label>
                  <Input
                    id="sec-dump-addr"
                    className="h-10 font-mono text-sm"
                    value={dumpAddr}
                    onChange={(e) => setDumpAddr(e.target.value)}
                    disabled={loading || (encrypted && locked)}
                    placeholder="Address"
                  />
                </div>
                <div className="pt-1">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={loading || !dumpAddr.trim() || (encrypted && locked)}
                    onClick={() =>
                      void run("Export key", async () => {
                        const wif = await walletRpc<string>("dumpprivkey", [dumpAddr.trim()]);
                        setExportedWif(typeof wif === "string" ? wif : "");
                      })
                    }
                  >
                    Reveal WIF
                  </Button>
                </div>
                {exportedWif && (
                  <div className="rounded-lg border border-amber-900/40 bg-amber-950/20 p-4">
                    <p className="break-all font-mono text-sm leading-relaxed text-amber-100">{exportedWif}</p>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
