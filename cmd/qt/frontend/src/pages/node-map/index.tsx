import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { GetDebugInfo, GetPeerList } from "../../../wailsjs/go/main/App";
import { DebugInfo, PeerEntry, GeoPoint, PeerWithGeo } from "@/lib/types";
import { globals as g } from "@/lib/globals";
import {
  loadGeoCache,
  resolveSelfGeo,
  extractIpFromAddr,
  isPublicRoutableIp,
  formatBytes,
} from "@/lib/utils";
const LeafletPeerMap = lazy(() => import("./leaflet-peer-map"));

export function NodeMap() {
  const [info, setInfo] = useState<DebugInfo | null>(null);
  const [peers, setPeers] = useState<PeerEntry[]>([]);
  const [geoByIp, setGeoByIp] = useState<Record<string, GeoPoint>>(() => loadGeoCache());
  const [selfGeo, setSelfGeo] = useState<GeoPoint | null>(null);
  const [fitNonce, setFitNonce] = useState<number>(0);
  const [resolvingGeo, setResolvingGeo] = useState<boolean>(false);
  const [viewMode, setViewMode] = useState<"map" | "table">("map");
  const inflight = useRef<Set<string>>(new Set());

  useEffect(() => {
    let mounted = true;
    const poll = () => {
      Promise.all([GetDebugInfo(), GetPeerList()])
        .then(([i, p]) => {
          if (!mounted) return;
          setInfo((i as DebugInfo) || null);
          setPeers(Array.isArray(p) ? (p as PeerEntry[]) : []);
        })
        .catch((e: unknown) => {
          if (!mounted) return;
          console.error(e);
        });
    };
    poll();
    const id = setInterval(poll, 2500);
    return () => {
      mounted = false;
      clearInterval(id);
    };
  }, []);

  useEffect(() => {
    if (typeof window === "undefined") return;
    try {
      window.localStorage.setItem(
        g.GEO_CACHE_KEY,
        JSON.stringify({
          ts: Date.now(),
          entries: geoByIp,
        }),
      );
    } catch {
      // Best-effort cache only.
    }
  }, [geoByIp]);

  useEffect(() => {
    let disposed = false;
    resolveSelfGeo().then((resolved) => {
      if (disposed || !resolved) return;
      setSelfGeo(resolved);
      setGeoByIp((prev) => ({ ...prev, [resolved.ip]: resolved }));
    });
    return () => {
      disposed = true;
    };
  }, []);

  useEffect(() => {
    const uniqueIps = Array.from(
      new Set(
        peers
          .map((peer) => extractIpFromAddr(peer.addr))
          .filter((ip): ip is string => !!ip && isPublicRoutableIp(ip)),
      ),
    ).slice(0, g.MAX_GEO_LOOKUPS);
    const pending = uniqueIps.filter((ip) => !geoByIp[ip] && !inflight.current.has(ip));
    if (pending.length === 0) return;
    let disposed = false;
    setResolvingGeo(true);

    Promise.all(
      pending.map(async (ip) => {
        inflight.current.add(ip);
        try {
          const res = await fetch(`https://ipwho.is/${encodeURIComponent(ip)}`);
          const payload = (await res.json()) as {
            success?: boolean;
            latitude?: number;
            longitude?: number;
            city?: string;
            region?: string;
            country?: string;
            connection?: { org?: string };
          };
          if (!payload.success || !payload.latitude || !payload.longitude) return;
          if (disposed) return;
          setGeoByIp((prev) => ({
            ...prev,
            [ip]: {
              ip,
              lat: payload.latitude!,
              lon: payload.longitude!,
              city: payload.city,
              region: payload.region,
              country: payload.country,
              org: payload.connection?.org,
            },
          }));
        } catch {
          // Ignore transient geolocation failures per IP.
        } finally {
          inflight.current.delete(ip);
        }
      }),
    ).finally(() => {
      if (!disposed) setResolvingGeo(false);
    });

    return () => {
      disposed = true;
    };
  }, [peers, geoByIp]);

  const points = useMemo<PeerWithGeo[]>(() => {
    const byKey = new Map<string, PeerWithGeo>();
    for (const peer of peers) {
      const ip = extractIpFromAddr(peer.addr);
      if (!ip) continue;
      const geo = geoByIp[ip];
      if (!geo) continue;
      byKey.set(`${peer.addr}-${peer.connTime}`, { peer, geo });
    }
    if (selfGeo) {
      byKey.set(`self-${selfGeo.ip}`, {
        peer: {
          addr: selfGeo.ip,
          subver: info?.userAgent || "local-node",
          version: 0,
          inbound: false,
          connTime: 0,
          bytesSent: 0,
          bytesRecv: 0,
          pingTime: 0,
        },
        geo: selfGeo,
        isSelf: true,
      });
    }
    return Array.from(byKey.values());
  }, [peers, geoByIp, selfGeo, info?.userAgent]);

  const peerOnlyCount = useMemo(() => points.filter((p) => !p.isSelf).length, [points]);

  const mappedCountries = useMemo(
    () => new Set(points.map((p) => p.geo.country).filter(Boolean)).size,
    [points],
  );

  return (
    <div className="mx-auto flex h-full min-h-0 w-full max-w-[1500px] flex-col gap-4">
      <Card className="btc-glow border-(--color-btc-border) bg-(--color-btc-card)">
        <CardHeader>
          <CardTitle>Node Map</CardTitle>
          <CardDescription>
            Global peer visualization using Leaflet, plus live node/core diagnostics.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
          <Stat label="Node Version" value={info?.clientVersion || "—"} />
          <Stat label="Network" value={info?.network || "—"} />
          <Stat
            label="Connections"
            value={info ? `${info.connections} (in ${info.inbound} / out ${info.outbound})` : "—"}
          />
          <Stat label="Mapped Peers" value={`${peerOnlyCount} / ${peers.length}`} />
          <Stat label="Countries" value={`${mappedCountries}`} />
        </CardContent>
      </Card>

      <div className="grid min-h-0 flex-1 gap-4">
        <Card className="flex min-h-[520px] flex-col overflow-hidden border-(--color-btc-border) bg-(--color-btc-card)">
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="text-base">Peer Geomap</CardTitle>
              <div className="flex items-center border border-(--color-btc-border) rounded-md">
                <Button
                  className={"rounded-r-none! w-16"}
                  variant={viewMode === "map" ? "outline" : "secondary"}
                  size="sm"
                  onClick={() => setViewMode("map")}
                >
                  Map
                </Button>
                <Button
                  className={"rounded-l-none! w-16"}
                  variant={viewMode === "table" ? "outline" : "secondary"}
                  size="sm"
                  onClick={() => setViewMode("table")}
                >
                  Table
                </Button>
              </div>
            </div>
            <CardDescription>
              Dot size tracks peer traffic volume; color indicates inbound vs outbound. Geolocation
              is cached locally for 24h.
            </CardDescription>
          </CardHeader>
          <CardContent className="relative min-h-0 flex-1 p-0">
            {viewMode === "map" ? (
              <Suspense
                fallback={
                  <div className="m-4 flex h-[calc(100%-2rem)] min-h-[420px] items-center justify-center rounded-lg border border-(--color-btc-border) text-sm text-(--color-btc-text-muted)">
                    Loading map...
                  </div>
                }
              >
                <div className="h-full min-h-[420px] p-4 pb-0">
                  <LeafletPeerMap
                    points={points}
                    pointCount={points.length}
                    fitNonce={fitNonce}
                    resolving={resolvingGeo}
                    onRefit={() => setFitNonce((n) => n + 1)}
                    formatBytes={formatBytes}
                  />
                </div>
              </Suspense>
            ) : (
              <div className="h-full p-4">
                <div className="h-full overflow-auto rounded-lg border border-(--color-btc-border)">
                  <Table className="min-w-[760px] text-xs">
                    <TableHeader className="sticky top-0 z-10 bg-(--color-btc-surface) [&_tr]:border-(--color-btc-border)">
                      <TableRow className="text-(--color-btc-text-dim) hover:bg-transparent">
                        <TableHead className="px-3 py-2 font-semibold">Peer</TableHead>
                        <TableHead className="px-3 py-2 font-semibold">Location</TableHead>
                        <TableHead className="px-3 py-2 font-semibold">Dir</TableHead>
                        <TableHead className="px-3 py-2 font-semibold">Version</TableHead>
                        <TableHead className="px-3 py-2 font-semibold">Ping</TableHead>
                        <TableHead className="px-3 py-2 font-semibold">Traffic</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {points.length === 0 ? (
                        <TableRow className="border-(--color-btc-border)">
                          <TableCell
                            className="px-3 py-4 text-(--color-btc-text-muted)"
                            colSpan={6}
                          >
                            No mapped peers yet.
                          </TableCell>
                        </TableRow>
                      ) : (
                        points.map(({ peer, geo, isSelf }) => (
                          <TableRow
                            key={`${peer.addr}-${peer.connTime}`}
                            className="border-(--color-btc-border)"
                          >
                            <TableCell className="px-3 py-2 font-mono text-(--color-btc-text)">
                              {isSelf ? `This node (${peer.addr})` : peer.addr}
                            </TableCell>
                            <TableCell className="px-3 py-2 text-(--color-btc-text-muted)">
                              {[geo.city, geo.region, geo.country].filter(Boolean).join(", ") ||
                                "Unknown"}
                            </TableCell>
                            <TableCell className="px-3 py-2 text-(--color-btc-text-muted)">
                              {isSelf ? "Self" : peer.inbound ? "Inbound" : "Outbound"}
                            </TableCell>
                            <TableCell className="px-3 py-2 text-(--color-btc-text-muted)">
                              {peer.subver || peer.version}
                            </TableCell>
                            <TableCell className="px-3 py-2 text-(--color-btc-text-muted)">
                              {peer.pingTime > 0 ? `${peer.pingTime.toFixed(3)}s` : "—"}
                            </TableCell>
                            <TableCell className="px-3 py-2 text-(--color-btc-text-muted)">
                              {formatBytes(peer.bytesRecv + peer.bytesSent)}
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-(--color-btc-border) bg-(--color-btc-surface) px-3 py-2">
      <p className="text-[11px] text-(--color-btc-text-dim)">{label}</p>
      <p className="mt-1 text-sm font-medium text-(--color-btc-text)">{value}</p>
    </div>
  );
}
