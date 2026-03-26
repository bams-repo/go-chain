import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { GeoPoint } from "./types";
import { globals as g } from "./globals";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}


export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(index === 0 ? 0 : 2)} ${units[index]}`;
}

export function extractIpFromAddr(addr: string): string | null {
  const raw = (addr || "").trim();
  if (!raw) return null;
  if (raw.startsWith("[")) {
    const end = raw.indexOf("]");
    if (end <= 1) return null;
    return raw.slice(1, end);
  }
  const parts = raw.split(":");
  if (parts.length === 2) return parts[0];
  if (parts.length > 2) return raw; // likely ipv6 without brackets
  return raw;
}

export function isPublicRoutableIp(ip: string): boolean {
  if (!ip) return false;
  const lower = ip.toLowerCase();
  if (lower.includes("onion") || lower === "localhost") return false;
  if (ip.includes(":")) {
    const norm = lower;
    return !(
      norm === "::1" ||
      norm.startsWith("fe80:") ||
      norm.startsWith("fc") ||
      norm.startsWith("fd")
    );
  }
  const octets = ip.split(".").map((x) => Number.parseInt(x, 10));
  if (octets.length !== 4 || octets.some((o) => Number.isNaN(o) || o < 0 || o > 255)) return false;
  const [a, b] = octets;
  if (a === 10 || a === 127 || a === 0) return false;
  if (a === 192 && b === 168) return false;
  if (a === 172 && b >= 16 && b <= 31) return false;
  if (a === 169 && b === 254) return false;
  return true;
}

export function loadGeoCache(): Record<string, GeoPoint> {
  if (typeof window === "undefined") return {};
  try {
    const raw = window.localStorage.getItem(g.GEO_CACHE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as {
      ts?: number;
      entries?: Record<string, GeoPoint>;
    };
    if (!parsed.ts || Date.now() - parsed.ts > g.GEO_CACHE_TTL_MS || !parsed.entries) {
      window.localStorage.removeItem(g.GEO_CACHE_KEY);
      return {};
    }
    return parsed.entries;
  } catch {
    return {};
  }
}

export async function resolveSelfGeo(): Promise<GeoPoint | null> {
  const providers = [
    "https://ipwho.is/",
    "https://ipapi.co/json/",
    "https://ipinfo.io/json",
  ];
  for (const url of providers) {
    try {
      const res = await fetch(url);
      if (!res.ok) continue;
      const payload = (await res.json()) as Record<string, unknown>;
      let ip = "";
      let lat = NaN;
      let lon = NaN;

      if (url.includes("ipwho.is")) {
        if (payload.success !== true) continue;
        ip = String(payload.ip || "");
        lat = Number(payload.latitude);
        lon = Number(payload.longitude);
      } else if (url.includes("ipapi.co")) {
        ip = String(payload.ip || "");
        lat = Number(payload.latitude);
        lon = Number(payload.longitude);
      } else {
        ip = String(payload.ip || "");
        const loc = String(payload.loc || "");
        const [a, b] = loc.split(",");
        lat = Number(a);
        lon = Number(b);
      }

      if (!ip || !Number.isFinite(lat) || !Number.isFinite(lon)) continue;
      const org =
        (payload.connection as { org?: string } | undefined)?.org ||
        (payload.org as string | undefined);
      return {
        ip,
        lat,
        lon,
        city: (payload.city as string | undefined) || undefined,
        region: (payload.region as string | undefined) || undefined,
        country:
          (payload.country as string | undefined) ||
          (payload.country_name as string | undefined) ||
          undefined,
        org,
      };
    } catch {
      // Try next provider.
    }
  }
  return null;
}
