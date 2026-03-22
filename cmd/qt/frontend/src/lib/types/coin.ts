export interface CoinInfo {
  name: string;
  nameLower: string;
  ticker: string;
  decimals: number;
  baseUnitName: string;
  displayUnitName: string;
  version: string;
  copyright: string;
  network: "mainnet" | "testnet" | "regtest";
}
