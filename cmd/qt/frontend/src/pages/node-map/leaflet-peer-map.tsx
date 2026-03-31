import { useEffect, useRef } from "react";
import type { LatLngBoundsExpression } from "leaflet";
import { CircleMarker, MapContainer, Popup, TileLayer, useMap } from "react-leaflet";
import "leaflet/dist/leaflet.css";
import { Button } from "@/components/ui/button";
import { formatPeerPing } from "@/lib/peer-normalize";
import { PeerWithGeo } from "@/lib/types";
import { globals as g } from "@/lib/globals";

function FitToPeers({ bounds, nonce }: { bounds: LatLngBoundsExpression | null; nonce: number }) {
  const map = useMap();
  const lastNonce = useRef(nonce);

  useEffect(() => {
    if (!bounds) return;
    if (nonce === lastNonce.current) return;
    lastNonce.current = nonce;
    map.fitBounds(bounds, { padding: [28, 28], maxZoom: 8 });
  }, [map, bounds, nonce]);
  return null;
}

function MapToolbar({
  onRefit,
  pointCount,
  resolving,
}: {
  onRefit: () => void;
  pointCount: number;
  resolving: boolean;
}) {
  return (
    <div className="absolute right-5 top-5 z-999 flex items-center gap-2 rounded-md border border-(--color-btc-border) bg-btc-card/95 px-2 py-1.5 shadow-lg backdrop-blur">
      <span className="text-[11px] text-(--color-btc-text-muted)">
        {resolving ? "Resolving peers..." : `${pointCount} mapped`}
      </span>
      <Button variant="outline" size="sm" onClick={onRefit}>
        Refit
      </Button>
    </div>
  );
}

export default function LeafletPeerMap({
  points,
  pointCount,
  fitNonce,
  resolving,
  onRefit,
  formatBytes,
}: {
  points: PeerWithGeo[];
  pointCount: number;
  fitNonce: number;
  resolving: boolean;
  onRefit: () => void;
  formatBytes: (bytes: number) => string;
}) {
  const bounds: LatLngBoundsExpression | null = points.length
    ? (points.map((p) => [p.geo.lat, p.geo.lon]) as LatLngBoundsExpression)
    : null;

  return (
    <>
      <MapToolbar pointCount={pointCount} resolving={resolving} onRefit={onRefit} />
      <MapContainer
        center={[g.FALLBACK_CENTER[0], g.FALLBACK_CENTER[1]]}
        zoom={g.FALLBACK_ZOOM}
        minZoom={g.MIN_ZOOM}
        maxZoom={g.MAX_ZOOM}
        maxBounds={g.WORLD_BOUNDS as LatLngBoundsExpression}
        maxBoundsViscosity={1}
        worldCopyJump={false}
        className="h-full w-full rounded-lg border border-(--color-btc-border)"
      >
        <TileLayer
          attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'
          url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
          noWrap
        />
        {bounds ? <FitToPeers bounds={bounds} nonce={fitNonce} /> : null}
        {points.map(({ peer, geo, isSelf }) => {
          const recv = Number.isFinite(peer.bytesRecv) ? peer.bytesRecv : 0;
          const sent = Number.isFinite(peer.bytesSent) ? peer.bytesSent : 0;
          const traffic = recv + sent;
          const radius = Math.max(4, Math.min(14, 4 + Math.log10(Math.max(traffic, 1))));
          return (
            <CircleMarker
              key={`${peer.addr}-${peer.connTime}`}
              center={[geo.lat, geo.lon]}
              radius={radius}
              pathOptions={{
                color: isSelf
                  ? "rgb(63 185 80)"
                  : peer.inbound
                    ? "rgb(88 166 255)"
                    : "rgb(247 147 26)",
                fillOpacity: isSelf ? 0.8 : 0.55,
                weight: isSelf ? 2.5 : 1.5,
              }}
            >
              <Popup>
                <div className="space-y-1 text-xs">
                  <div>
                    <strong>{isSelf ? `This node (${peer.addr})` : peer.addr}</strong>
                  </div>
                  <div>
                    {[geo.city, geo.region, geo.country].filter(Boolean).join(", ") ||
                      "Unknown location"}
                  </div>
                  <div>{peer.inbound ? "Inbound" : "Outbound"}</div>
                  <div>Version: {peer.subver || peer.version}</div>
                  <div>Ping: {formatPeerPing(peer.pingTime)}</div>
                  <div>Traffic: {formatBytes(traffic)}</div>
                  {geo.org ? <div>ASN/Org: {geo.org}</div> : null}
                </div>
              </Popup>
            </CircleMarker>
          );
        })}
      </MapContainer>
    </>
  );
}
