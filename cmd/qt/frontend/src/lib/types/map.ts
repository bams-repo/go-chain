export interface DebugInfo {
    clientVersion: string;
    userAgent: string;
    datadir: string;
    startupTime: string;
    network: string;
    connections: number;
    inbound: number;
    outbound: number;
}

export interface PeerEntry {
    addr: string;
    subver: string;
    version: number;
    inbound: boolean;
    connTime: number;
    bytesSent: number;
    bytesRecv: number;
    pingTime: number;
}

export interface GeoPoint {
    ip: string;
    lat: number;
    lon: number;
    city?: string;
    region?: string;
    country?: string;
    org?: string;
}

export interface PeerWithGeo {
    peer: PeerEntry;
    geo: GeoPoint;
    isSelf?: boolean;
}