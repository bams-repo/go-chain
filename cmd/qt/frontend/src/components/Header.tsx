import { useEffect, useState } from "react";
import { useCoinInfo } from "../hooks/useCoinInfo";
import {
  GetBlockchainInfo,
  GetPeerCount,
} from "../../wailsjs/go/main/App";

export function Header() {
  const coinInfo = useCoinInfo();
  const [height, setHeight] = useState(0);
  const [network, setNetwork] = useState("");
  const [peers, setPeers] = useState(0);

  useEffect(() => {
    const poll = () => {
      GetBlockchainInfo().then((info) => {
        setHeight(info.height as number);
        setNetwork(info.network as string);
      });
      GetPeerCount().then(setPeers);
    };
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, []);

  return (
    <header
      className="btc-noise relative flex items-center justify-between px-6 py-3"
      style={{
        background: 'var(--color-btc-surface)',
        borderBottom: '1px solid var(--color-btc-border)',
      }}
    >
      <div className="relative z-10 flex items-center gap-4">
        <h2 className="text-sm font-semibold" style={{ color: 'var(--color-btc-text)' }}>
          {coinInfo.name} <span style={{ color: 'var(--color-btc-text-dim)' }} className="font-normal">Wallet</span>
        </h2>
        {network && (
          <span
            className="rounded-full px-2.5 py-0.5 text-[11px] font-medium"
            style={{
              background: 'rgba(247, 147, 26, 0.1)',
              color: 'var(--color-btc-gold)',
              border: '1px solid rgba(247, 147, 26, 0.2)',
            }}
          >
            {network}
          </span>
        )}
      </div>
      <div className="relative z-10 flex items-center gap-6 text-xs" style={{ color: 'var(--color-btc-text-muted)' }}>
        <div className="flex items-center gap-1.5">
          <div className="h-1.5 w-1.5 rounded-full" style={{ background: peers > 0 ? 'var(--color-btc-green)' : 'var(--color-btc-red)' }} />
          <span>{peers} peer{peers !== 1 ? "s" : ""}</span>
        </div>
        <span className="font-mono" style={{ color: 'var(--color-btc-text-dim)' }}>
          #{height.toLocaleString()}
        </span>
      </div>
    </header>
  );
}
