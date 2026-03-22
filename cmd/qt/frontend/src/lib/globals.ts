import { CoinInfo } from "./types";

export const globals = {
    // Fallback coin info if the coin info is not available
    FALLBACK_COIN_INFO: {
        name: "Fairchain",
        nameLower: "fairchain",
        ticker: "FAIR",
        decimals: 8,
        baseUnitName: "base unit",
        displayUnitName: "FAIR",
        version: "0.0.0",
        copyright: "",
        network: "regtest",
    } as CoinInfo,
};