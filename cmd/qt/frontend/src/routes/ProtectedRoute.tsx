import { useEffect, useState } from "react";
import { Outlet } from "react-router-dom";
import { CoinInfo as GetCoinInfo } from "../../wailsjs/go/main/App";
import { CoinInfoContext } from "@/hooks/useCoinInfo";
import type { CoinInfo } from "@/lib/types";
import NoCoinInfo from "@/pages/NoCoinInfo";

type BootstrapPhase = "loading" | "ready" | "error";

/**
 * Loads coin metadata from the host, then provides {@link CoinInfoContext} for all nested routes.
 * Shows {@link NoCoinInfo} until the backend responds.
 */
export default function ProtectedRoute() {
  const [phase, setPhase] = useState<BootstrapPhase>("loading");
  const [coinInfo, setCoinInfo] = useState<CoinInfo | null>(null);

  useEffect(() => {
    let cancelled = false;
    GetCoinInfo()
      .then((raw) => {
        if (cancelled) return;
        setCoinInfo(raw as unknown as CoinInfo);
        setPhase("ready");
      })
      .catch(() => {
        if (!cancelled) setPhase("error");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (phase === "loading") {
    return <NoCoinInfo variant="loading" />;
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
