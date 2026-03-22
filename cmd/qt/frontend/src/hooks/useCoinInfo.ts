import { createContext, useContext } from "react";
import { CoinInfo } from "../lib/types";

export const CoinInfoContext = createContext<CoinInfo | null>(null);

export function useCoinInfo(): CoinInfo {
  const ctx = useContext(CoinInfoContext);
  if (!ctx) {
    throw new Error("useCoinInfo must be used within CoinInfoContext.Provider");
  }
  return ctx;
}
