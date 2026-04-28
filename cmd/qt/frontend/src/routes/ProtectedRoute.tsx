import { useEffect, useState } from "react";
import { Outlet } from "react-router-dom";
import { CoinInfo as GetCoinInfo, WalletExists } from "../../wailsjs/go/main/App";
import { CoinInfoContext } from "@/hooks/useCoinInfo";
import type { CoinInfo } from "@/lib/types";
import NoCoinInfo from "@/pages/NoCoinInfo";
import { WalletSetup } from "@/pages/WalletSetup";

type BootstrapPhase = "loading" | "setup" | "ready" | "error";

/**
 * Gates the app on wallet existence. If no wallet file is found, shows the
 * first-run setup wizard (create / import). Once a wallet exists, loads coin
 * metadata and renders nested routes.
 */
export default function ProtectedRoute() {
  const [phase, setPhase] = useState<BootstrapPhase>("loading");
  const [coinInfo, setCoinInfo] = useState<CoinInfo | null>(null);

  const bootstrap = () => {
    setPhase("loading");
    WalletExists()
      .then((exists) => {
        if (!exists) {
          setPhase("setup");
          return;
        }
        return GetCoinInfo().then((raw) => {
          setCoinInfo(raw as unknown as CoinInfo);
          setPhase("ready");
        });
      })
      .catch(() => {
        setPhase("error");
      });
  };

  useEffect(() => {
    bootstrap();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (phase === "loading") {
    return <NoCoinInfo variant="loading" />;
  }
  if (phase === "setup") {
    return <WalletSetup onComplete={bootstrap} />;
  }
  if (phase === "error" || !coinInfo) {
    return <NoCoinInfo variant="error" />;
  }

  return (
    <CoinInfoContext.Provider value={coinInfo}>
      <Outlet />
    </CoinInfoContext.Provider>
  );
}
