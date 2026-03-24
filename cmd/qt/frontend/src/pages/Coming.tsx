export function Coming() {
  return (
    <div
      className="flex h-full items-center justify-center"
      style={{ color: "var(--color-btc-text-dim)" }}
    >
      <div className="text-center">
        <svg
          className="mx-auto mb-3 h-12 w-12 opacity-30"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          strokeWidth={1}
        >
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
        </svg>
        <p className="text-sm font-medium">Coming in Phase 2</p>
      </div>
    </div>
  );
}
