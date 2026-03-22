import { useEffect, useState } from "react";
import { CoinInfo, CoinInfoContext } from "./hooks/useCoinInfo";
import { Sidebar } from "./components/Sidebar";
import { Header } from "./components/Header";
import { Overview } from "./pages/Overview";
import { Social } from "./pages/Social";
import { CoinInfo as GetCoinInfo } from "../wailsjs/go/main/App";

type Page = "overview" | "social" | "send" | "receive" | "transactions" | "network" | "mining" | "console";

function App() {
  const [coinInfo, setCoinInfo] = useState<CoinInfo | null>(null);
  const [page, setPage] = useState<Page>("overview");

  useEffect(() => {
    GetCoinInfo().then((info) => setCoinInfo(info as unknown as CoinInfo));
  }, []);

  if (!coinInfo) {
    return (
      <div className="flex h-full items-center justify-center" style={{ background: 'var(--color-btc-deep)' }}>
        <div className="flex flex-col items-center gap-4">
          <div className="h-10 w-10 animate-spin rounded-full border-2 border-transparent" style={{ borderTopColor: 'var(--color-btc-gold)' }} />
          <span style={{ color: 'var(--color-btc-text-muted)' }} className="text-sm">Starting node...</span>
        </div>
      </div>
    );
  }

  const renderPage = () => {
    switch (page) {
      case "overview":
        return <Overview />;
      case "social":
        return <Social />;
      default:
        return (
          <div className="flex h-full items-center justify-center" style={{ color: 'var(--color-btc-text-dim)' }}>
            <div className="text-center">
              <svg className="mx-auto mb-3 h-12 w-12 opacity-30" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              <p className="text-sm font-medium">Coming in Phase 2</p>
            </div>
          </div>
        );
    }
  };

  return (
    <CoinInfoContext.Provider value={coinInfo}>
      <div className="flex h-full" style={{ background: 'var(--color-btc-deep)' }}>
        <Sidebar currentPage={page} onNavigate={setPage} />
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <Header />
          <main className="relative min-h-0 flex-1 overflow-auto p-5">
            {renderPage()}
          </main>
        </div>
      </div>
    </CoinInfoContext.Provider>
  );
}

export default App;
