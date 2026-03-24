type NoCoinInfoProps = {
  variant?: "loading" | "error";
};

export default function NoCoinInfo({ variant = "loading" }: NoCoinInfoProps) {
  const isError = variant === "error";
  return (
    <div
      className="flex h-full items-center justify-center"
      style={{ background: "var(--color-btc-deep)" }}
    >
      <div className="flex max-w-sm flex-col items-center gap-4 px-6 text-center">
        <div
          className={`h-10 w-10 rounded-full border-2 border-transparent ${isError ? "" : "animate-spin"}`}
          style={{ borderTopColor: "var(--color-btc-gold)" }}
        />
        <span style={{ color: "var(--color-btc-text-muted)" }} className="text-sm">
          {isError
            ? "Could not load wallet info. Check the node and try restarting the app."
            : "Starting node…"}
        </span>
      </div>
    </div>
  );
}
