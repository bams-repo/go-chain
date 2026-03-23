import { useEffect, useState } from "react";
import { useCoinInfo } from "../hooks/useCoinInfo";
import {
  GetBalance,
  GetWalletAddress,
  GetBlockchainInfo,
} from "../../wailsjs/go/main/App";

export function Overview() {
  const coinInfo = useCoinInfo();
  const [confirmed, setConfirmed] = useState(0);
  const [unconfirmed, setUnconfirmed] = useState(0);
  const [address, setAddress] = useState("");
  const [height, setHeight] = useState(0);
  const [bestHash, setBestHash] = useState("");

  useEffect(() => {
    const poll = () => {
      GetBalance().then((b) => {
        setConfirmed(b.confirmed as number);
        setUnconfirmed(b.unconfirmed as number);
      });
      GetBlockchainInfo().then((info) => {
        setHeight(info.height as number);
        setBestHash(info.bestHash as string);
      });
      if (!address) {
        GetWalletAddress().then((a) => { if (a) setAddress(a); }).catch(() => {});
      }
    };
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, [address]);

  return (
    <div className="flex h-full flex-col gap-4">
      {/* Balance */}
      <div
        className="btc-noise btc-glow-active relative overflow-hidden rounded-xl p-6"
        style={{
          background: 'linear-gradient(135deg, var(--color-btc-card) 0%, var(--color-btc-surface) 100%)',
          border: '1px solid var(--color-btc-border)',
        }}
      >
        <div className="absolute -right-8 -top-8 h-32 w-32 rounded-full opacity-[0.04]" style={{ background: 'var(--color-btc-gold)' }} />
        <div className="relative z-10">
          <h3 className="mb-1 text-xs font-medium uppercase tracking-wider" style={{ color: 'var(--color-btc-text-dim)' }}>Balance</h3>
          <p className="text-3xl font-bold" style={{ color: 'var(--color-btc-text)' }}>
            {confirmed.toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)}{" "}
            <span className="text-lg font-medium" style={{ color: 'var(--color-btc-gold)' }}>{coinInfo.ticker}</span>
          </p>
          {unconfirmed > 0 && (
            <p className="mt-1.5 text-sm" style={{ color: 'var(--color-btc-gold-light)' }}>
              +{unconfirmed.toFixed(4)} {coinInfo.ticker} unconfirmed
            </p>
          )}
        </div>
      </div>

      {/* Address */}
      <div
        className="btc-glow rounded-xl p-5"
        style={{
          background: 'var(--color-btc-card)',
          border: '1px solid var(--color-btc-border)',
        }}
      >
        <h3 className="mb-2 text-xs font-medium uppercase tracking-wider" style={{ color: 'var(--color-btc-text-dim)' }}>
          Default Address
        </h3>
        <code
          className="block break-all rounded-lg px-3 py-2.5 text-sm font-mono"
          style={{
            background: 'var(--color-btc-deep)',
            color: 'var(--color-btc-gold-light)',
            border: '1px solid var(--color-btc-border)',
          }}
        >
          {address || "Loading..."}
        </code>
      </div>

      {/* Chain Status */}
      <div
        className="btc-glow rounded-xl p-5"
        style={{
          background: 'var(--color-btc-card)',
          border: '1px solid var(--color-btc-border)',
        }}
      >
        <h3 className="mb-3 text-xs font-medium uppercase tracking-wider" style={{ color: 'var(--color-btc-text-dim)' }}>
          Chain Status
        </h3>
        <dl className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <dt style={{ color: 'var(--color-btc-text-muted)' }} className="text-xs">Block Height</dt>
            <dd className="font-mono font-medium" style={{ color: 'var(--color-btc-text)' }}>
              {height.toLocaleString()}
            </dd>
          </div>
          <div>
            <dt style={{ color: 'var(--color-btc-text-muted)' }} className="text-xs">Best Block</dt>
            <dd className="truncate font-mono font-medium" style={{ color: 'var(--color-btc-text)' }} title={bestHash}>
              {bestHash ? bestHash.slice(0, 16) + "\u2026" : "\u2014"}
            </dd>
          </div>
        </dl>
      </div>
    </div>
  );
}
