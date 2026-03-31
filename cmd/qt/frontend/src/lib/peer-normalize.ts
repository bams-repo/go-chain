import type { PeerEntry } from "./types";

function num(v: unknown, fallback = 0): number {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    if (Number.isFinite(n)) return n;
  }
  return fallback;
}

function str(v: unknown): string {
  if (v === null || v === undefined) return "";
  return String(v);
}

function bool(v: unknown): boolean {
  return v === true || v === "true";
}

export function normalizePeerEntry(raw: Record<string, unknown>): PeerEntry {
  return {
    addr: str(raw.addr ?? raw.Addr),
    addrLocal: str(raw.addrLocal ?? raw.addrlocal ?? raw.AddrLocal),
    subver: str(raw.subver ?? raw.SubVer ?? raw.subVer),
    version: Math.trunc(num(raw.version ?? raw.Version)),
    inbound: bool(raw.inbound ?? raw.Inbound),
    connTime: Math.trunc(num(raw.connTime ?? raw.conntime ?? raw.ConnTime)),
    lastSend: Math.trunc(num(raw.lastSend ?? raw.lastsend ?? raw.LastSend)),
    lastRecv: Math.trunc(num(raw.lastRecv ?? raw.lastrecv ?? raw.LastRecv)),
    bytesSent: Math.trunc(num(raw.bytesSent ?? raw.bytessent ?? raw.BytesSent)),
    bytesRecv: Math.trunc(num(raw.bytesRecv ?? raw.bytesrecv ?? raw.BytesRecv)),
    pingTime: num(raw.pingTime ?? raw.pingtime ?? raw.PingTime),
    startingHeight: Math.trunc(num(raw.startingHeight ?? raw.startingheight ?? raw.StartingHeight)),
    banScore: Math.trunc(num(raw.banScore ?? raw.banscore ?? raw.BanScore)),
  };
}

export function normalizePeerList(raw: unknown): PeerEntry[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((row) =>
    normalizePeerEntry(row && typeof row === "object" ? (row as Record<string, unknown>) : {}),
  );
}

export function peerRowKey(p: Pick<PeerEntry, "addr" | "connTime">): string {
  return `${p.addr}\0${p.connTime}`;
}

export function formatPeerPing(pingTimeSeconds: number): string {
  if (!Number.isFinite(pingTimeSeconds) || pingTimeSeconds <= 0) return "—";
  return `${Math.round(pingTimeSeconds * 1000)} ms`;
}
