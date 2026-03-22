import { useCoinInfo } from "../hooks/useCoinInfo";

type Page = "overview" | "social" | "send" | "receive" | "transactions" | "network" | "mining" | "console";

interface SidebarProps {
  currentPage: Page;
  onNavigate: (page: Page) => void;
}

const navItems: { id: Page; label: string; icon: string; enabled: boolean }[] = [
  { id: "overview", label: "Overview", icon: "M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-4 0h4", enabled: true },
  { id: "social", label: "Social", icon: "M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z", enabled: true },
  { id: "send", label: "Send", icon: "M12 19V5m0 0l-7 7m7-7l7 7", enabled: false },
  { id: "receive", label: "Receive", icon: "M12 5v14m0 0l7-7m-7 7l-7-7", enabled: false },
  { id: "transactions", label: "Transactions", icon: "M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2", enabled: false },
  { id: "network", label: "Network", icon: "M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9", enabled: false },
  { id: "mining", label: "Mining", icon: "M19.428 15.428a2 2 0 00-1.022-.547l-2.387-.477a6 6 0 00-3.86.517l-.318.158a6 6 0 01-3.86.517L6.05 15.21a2 2 0 00-1.806.547M8 4h8l-1 1v5.172a2 2 0 00.586 1.414l5 5c1.26 1.26.367 3.414-1.415 3.414H4.828c-1.782 0-2.674-2.154-1.414-3.414l5-5A2 2 0 009 10.172V5L8 4z", enabled: false },
  { id: "console", label: "Console", icon: "M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z", enabled: false },
];

export function Sidebar({ currentPage, onNavigate }: SidebarProps) {
  const coinInfo = useCoinInfo();

  return (
    <aside
      className="btc-noise relative flex w-56 flex-col overflow-hidden"
      style={{
        background: 'linear-gradient(180deg, var(--color-btc-surface) 0%, var(--color-btc-deep) 100%)',
        borderRight: '1px solid var(--color-btc-border)',
      }}
    >
      {/* Brand */}
      <div className="relative z-10 px-5 pt-6 pb-4">
        <div className="flex items-center gap-2.5">
          <div
            className="flex h-8 w-8 items-center justify-center rounded-lg text-sm font-black"
            style={{ background: 'linear-gradient(135deg, var(--color-btc-gold) 0%, var(--color-btc-gold-dark) 100%)', color: '#000' }}
          >
            F
          </div>
          <div>
            <h1 className="text-base font-bold" style={{ color: 'var(--color-btc-text)' }}>{coinInfo.name}</h1>
            <span className="text-[11px]" style={{ color: 'var(--color-btc-text-dim)' }}>v{coinInfo.version}</span>
          </div>
        </div>
      </div>

      {/* Divider with gold accent */}
      <div className="mx-4 mb-2" style={{ height: '1px', background: 'linear-gradient(90deg, var(--color-btc-gold) 0%, transparent 100%)', opacity: 0.2 }} />

      {/* Nav */}
      <nav className="relative z-10 flex-1 space-y-0.5 px-3">
        {navItems.map((item) => {
          const active = currentPage === item.id;
          return (
            <button
              key={item.id}
              onClick={() => item.enabled && onNavigate(item.id)}
              disabled={!item.enabled}
              className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-[13px] transition-all duration-150"
              style={{
                background: active ? 'linear-gradient(90deg, rgba(247,147,26,0.12) 0%, rgba(247,147,26,0.04) 100%)' : 'transparent',
                color: active ? 'var(--color-btc-gold-light)' : item.enabled ? 'var(--color-btc-text-muted)' : 'var(--color-btc-text-dim)',
                borderLeft: active ? '2px solid var(--color-btc-gold)' : '2px solid transparent',
                cursor: item.enabled ? 'pointer' : 'not-allowed',
                opacity: item.enabled ? 1 : 0.4,
              }}
            >
              <svg className="h-4 w-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={active ? 2 : 1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d={item.icon} />
              </svg>
              <span className={active ? "font-semibold" : "font-normal"}>{item.label}</span>
            </button>
          );
        })}
      </nav>

      {/* Footer */}
      <div className="relative z-10 px-5 py-3" style={{ borderTop: '1px solid var(--color-btc-border)' }}>
        <p className="text-[11px]" style={{ color: 'var(--color-btc-text-dim)' }}>{coinInfo.copyright}</p>
      </div>
    </aside>
  );
}
