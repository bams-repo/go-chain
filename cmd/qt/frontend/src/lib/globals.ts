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

  // Max geo lookups for the map
  MAX_GEO_LOOKUPS: 20,

  // Geo cache key for the map
  GEO_CACHE_KEY: "fairchain.nodeMap.geo.v1",

  // Geo cache ttl for the map
  GEO_CACHE_TTL_MS: 24 * 60 * 60 * 1000,

  // Fallback center for the map
  FALLBACK_CENTER: [20, 0],

  // Fallback zoom for the map
  FALLBACK_ZOOM: 2,

  // World bounds for the map
  WORLD_BOUNDS: [
    [-85, -179.999],
    [85, 179.999],
  ],

  // Max zoom for the map
  MAX_ZOOM: 18,

  // Min zoom for the map
  MIN_ZOOM: 2,
};
