import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import {
  ChangeIRCNick,
  ConnectIRC,
  GetIRCMessages,
  GetIRCStatus,
  GetIRCUsers,
  SendIRCMessage,
} from "../../wailsjs/go/main/App";

type IRCStatus = {
  connected?: boolean;
  server?: string;
  channel?: string;
  nick?: string;
  error?: string;
  topic?: string;
  userCount?: number;
};

type IRCMessage = {
  time?: string;
  sender?: string;
  text?: string;
  self?: boolean;
  system?: boolean;
};

function timeLabel(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function Social() {
  const [status, setStatus] = useState<IRCStatus>({});
  const [messages, setMessages] = useState<IRCMessage[]>([]);
  const [users, setUsers] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editingNick, setEditingNick] = useState(false);
  const [nickDraft, setNickDraft] = useState("");
  const endRef = useRef<HTMLDivElement | null>(null);
  const nickInputRef = useRef<HTMLInputElement | null>(null);

  const connected = !!status.connected;
  const connectionLabel = useMemo(() => (connected ? "Connected" : "Offline"), [connected]);

  useEffect(() => {
    let mounted = true;
    const poll = () => {
      Promise.all([GetIRCStatus(), GetIRCMessages(), GetIRCUsers()])
        .then(([nextStatus, nextMessages, nextUsers]) => {
          if (!mounted) return;
          setStatus((nextStatus as IRCStatus) || {});
          setMessages(Array.isArray(nextMessages) ? (nextMessages as IRCMessage[]) : []);
          setUsers(Array.isArray(nextUsers) ? (nextUsers as string[]) : []);
        })
        .catch((e: unknown) => {
          if (!mounted) return;
          setError(e instanceof Error ? e.message : "Failed to refresh social chat");
        });
    };
    poll();
    const id = setInterval(poll, 1500);
    return () => { mounted = false; clearInterval(id); };
  }, []);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages.length]);

  const onReconnect = () => {
    setBusy(true);
    setError("");
    ConnectIRC()
      .then(() => GetIRCStatus().then((next) => setStatus((next as IRCStatus) || {})))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : "Unable to connect"))
      .finally(() => setBusy(false));
  };

  const onStartNickEdit = () => {
    setNickDraft(status.nick || "");
    setEditingNick(true);
    setTimeout(() => nickInputRef.current?.focus(), 0);
  };

  const onCancelNickEdit = () => { setEditingNick(false); setNickDraft(""); };

  const onSubmitNick = () => {
    const nick = nickDraft.trim();
    if (!nick || nick === status.nick) { onCancelNickEdit(); return; }
    setBusy(true);
    setError("");
    ChangeIRCNick(nick)
      .then(() => GetIRCStatus().then((next) => setStatus((next as IRCStatus) || {})))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : "Failed to change nickname"))
      .finally(() => { setBusy(false); setEditingNick(false); setNickDraft(""); });
  };

  const onSend = (e: FormEvent) => {
    e.preventDefault();
    const text = draft.trim();
    if (!text || busy) return;
    setBusy(true);
    setError("");
    SendIRCMessage(text)
      .then(() => {
        setDraft("");
        return GetIRCMessages().then((next) => {
          setMessages(Array.isArray(next) ? (next as IRCMessage[]) : []);
        });
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Unable to send message"))
      .finally(() => setBusy(false));
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      {/* Header bar */}
      <div
        className="btc-noise btc-glow relative shrink-0 overflow-hidden rounded-xl px-5 py-3"
        style={{
          background: 'linear-gradient(135deg, var(--color-btc-card) 0%, var(--color-btc-surface) 100%)',
          border: '1px solid var(--color-btc-border)',
        }}
      >
        <div className="relative z-10 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold" style={{ color: 'var(--color-btc-text)' }}>Social Chat</h3>
            <p className="text-xs" style={{ color: 'var(--color-btc-text-muted)' }}>
              {status.server || "irc.libera.chat:6697"}{" "}
              <span style={{ color: 'var(--color-btc-gold)' }}>{status.channel || "#test112221"}</span>
            </p>
            {status.topic && status.topic.trim() && (
              <p className="mt-0.5 text-xs italic" style={{ color: 'var(--color-btc-text-dim)' }}>
                {status.topic}
              </p>
            )}
          </div>
          <div className="flex items-center gap-3">
            <span
              className="flex items-center gap-1.5 rounded-full px-3 py-1 text-[11px] font-medium"
              style={{
                background: connected ? 'rgba(63, 185, 80, 0.1)' : 'rgba(248, 81, 73, 0.1)',
                color: connected ? 'var(--color-btc-green)' : 'var(--color-btc-red)',
                border: `1px solid ${connected ? 'rgba(63, 185, 80, 0.25)' : 'rgba(248, 81, 73, 0.25)'}`,
              }}
            >
              <div
                className="h-1.5 w-1.5 rounded-full"
                style={{ background: connected ? 'var(--color-btc-green)' : 'var(--color-btc-red)' }}
              />
              {connectionLabel}
            </span>

            {editingNick ? (
              <span className="flex items-center gap-1.5">
                <input
                  ref={nickInputRef}
                  type="text"
                  value={nickDraft}
                  onChange={(e) => setNickDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") onSubmitNick();
                    if (e.key === "Escape") onCancelNickEdit();
                  }}
                  maxLength={16}
                  disabled={busy}
                  className="w-28 rounded-lg px-2 py-1 text-xs outline-none"
                  style={{
                    background: 'var(--color-btc-deep)',
                    color: 'var(--color-btc-text)',
                    border: '1px solid var(--color-btc-gold)',
                    boxShadow: '0 0 8px rgba(247, 147, 26, 0.15)',
                  }}
                />
                <button type="button" onClick={onSubmitNick} disabled={busy || !nickDraft.trim()}
                  className="rounded-lg px-2.5 py-1 text-[11px] font-semibold transition-all disabled:opacity-40"
                  style={{ background: 'var(--color-btc-gold)', color: '#000' }}>
                  Save
                </button>
                <button type="button" onClick={onCancelNickEdit} disabled={busy}
                  className="rounded-lg px-2 py-1 text-[11px] font-medium transition-colors"
                  style={{ color: 'var(--color-btc-text-muted)' }}>
                  Cancel
                </button>
              </span>
            ) : (
              <button type="button" onClick={onStartNickEdit} disabled={!connected}
                title={connected ? "Click to change nickname" : "Connect to change nickname"}
                className="text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-40"
                style={{ color: 'var(--color-btc-text-muted)' }}>
                {status.nick ? <><span style={{ color: 'var(--color-btc-text-dim)' }}>nick:</span> <span style={{ color: 'var(--color-btc-gold-light)' }}>{status.nick}</span></> : "nick pending"}
              </button>
            )}

            <span className="text-xs" style={{ color: 'var(--color-btc-text-dim)' }}>
              {users.length} user{users.length === 1 ? "" : "s"}
            </span>

            {!connected && (
              <button type="button" onClick={onReconnect} disabled={busy}
                className="rounded-lg px-3.5 py-1.5 text-xs font-semibold transition-all disabled:opacity-40"
                style={{ background: 'linear-gradient(135deg, var(--color-btc-gold) 0%, var(--color-btc-gold-dark) 100%)', color: '#000' }}>
                Reconnect
              </button>
            )}
          </div>
        </div>
        {(status.error || error) && (
          <p
            className="relative z-10 mt-2 rounded-lg px-3 py-1.5 text-xs"
            style={{
              background: 'rgba(248, 81, 73, 0.08)',
              color: 'var(--color-btc-red)',
              border: '1px solid rgba(248, 81, 73, 0.2)',
            }}
          >
            {status.error || error}
          </p>
        )}
      </div>

      {/* Chat + Users */}
      <div className="flex min-h-0 flex-1 gap-3">
        {/* Messages */}
        <div
          className="btc-noise relative min-h-0 flex-1 overflow-hidden rounded-xl"
          style={{
            background: 'var(--color-btc-deep)',
            border: '1px solid var(--color-btc-border)',
          }}
        >
          <div className="relative z-10 h-full overflow-y-auto px-4 py-3">
            <div className="space-y-2.5">
              {messages.length === 0 && (
                <div
                  className="rounded-xl px-4 py-8 text-center text-sm"
                  style={{ color: 'var(--color-btc-text-dim)' }}
                >
                  <svg className="mx-auto mb-3 h-10 w-10 opacity-20" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1}>
                    <path strokeLinecap="round" strokeLinejoin="round" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
                  </svg>
                  Waiting for messages from {status.channel || "#test112221"}...
                </div>
              )}

              {messages.map((m, idx) => {
                const key = `${m.time || "t"}-${m.sender || "s"}-${idx}`;
                if (m.system) {
                  return (
                    <div key={key} className="text-center text-[11px]" style={{ color: 'var(--color-btc-text-dim)' }}>
                      {m.text}
                    </div>
                  );
                }
                const own = !!m.self;
                return (
                  <div key={key} className={`flex ${own ? "justify-end" : "justify-start"}`}>
                    <div
                      className="max-w-[80%] rounded-xl px-3.5 py-2.5"
                      style={{
                        background: own
                          ? 'linear-gradient(135deg, var(--color-btc-gold) 0%, var(--color-btc-gold-dark) 100%)'
                          : 'var(--color-btc-card)',
                        border: own ? 'none' : '1px solid var(--color-btc-border)',
                        boxShadow: own ? '0 2px 12px rgba(247, 147, 26, 0.2)' : '0 1px 4px rgba(0,0,0,0.3)',
                      }}
                    >
                      <div className="mb-0.5 flex items-center gap-2 text-[11px]">
                        <span
                          className="font-semibold"
                          style={{ color: own ? 'rgba(0,0,0,0.7)' : 'var(--color-btc-gold)' }}
                        >
                          {m.sender || "unknown"}
                        </span>
                        <span style={{ color: own ? 'rgba(0,0,0,0.4)' : 'var(--color-btc-text-dim)' }}>
                          {timeLabel(m.time)}
                        </span>
                      </div>
                      <p
                        className="whitespace-pre-wrap break-words text-sm leading-relaxed"
                        style={{ color: own ? '#000' : 'var(--color-btc-text)' }}
                      >
                        {m.text}
                      </p>
                    </div>
                  </div>
                );
              })}
              <div ref={endRef} />
            </div>
          </div>
        </div>

        {/* Users sidebar */}
        <div
          className="btc-glow flex w-44 shrink-0 flex-col overflow-hidden rounded-xl"
          style={{
            background: 'var(--color-btc-card)',
            border: '1px solid var(--color-btc-border)',
          }}
        >
          <div className="px-3 py-2.5" style={{ borderBottom: '1px solid var(--color-btc-border)' }}>
            <h4 className="text-[11px] font-semibold uppercase tracking-wider" style={{ color: 'var(--color-btc-text-dim)' }}>Users</h4>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
            <div className="space-y-0.5">
              {users.length === 0 && (
                <p className="px-2 py-1 text-xs" style={{ color: 'var(--color-btc-text-dim)' }}>No users visible.</p>
              )}
              {users.map((u) => {
                const isSelf = status.nick === u;
                return (
                  <div
                    key={u}
                    className="flex items-center justify-between rounded-lg px-2.5 py-1.5 text-xs transition-colors"
                    style={{
                      background: isSelf ? 'rgba(247, 147, 26, 0.08)' : 'transparent',
                      color: isSelf ? 'var(--color-btc-gold-light)' : 'var(--color-btc-text-muted)',
                    }}
                  >
                    <span className="truncate">{u}</span>
                    {isSelf && (
                      <span className="text-[10px] uppercase tracking-wide" style={{ color: 'var(--color-btc-gold)', opacity: 0.6 }}>you</span>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </div>

      {/* Input bar */}
      <form
        onSubmit={onSend}
        className="btc-glow shrink-0 rounded-xl p-3"
        style={{
          background: 'var(--color-btc-card)',
          border: '1px solid var(--color-btc-border)',
        }}
      >
        <div className="flex items-stretch gap-3">
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                if (draft.trim() && connected && !busy) onSend(e);
              }
            }}
            rows={2}
            maxLength={400}
            placeholder={connected ? `Message ${status.channel || "#test112221"}...` : "Connect to start chatting..."}
            disabled={!connected || busy}
            className="flex-1 resize-none rounded-lg px-3 py-2.5 text-sm outline-none transition disabled:cursor-not-allowed disabled:opacity-40"
            style={{
              background: 'var(--color-btc-deep)',
              color: 'var(--color-btc-text)',
              border: '1px solid var(--color-btc-border)',
            }}
          />
          <button
            type="submit"
            disabled={!connected || busy || draft.trim().length === 0}
            className="rounded-lg px-5 text-sm font-semibold transition-all disabled:cursor-not-allowed disabled:opacity-30"
            style={{
              background: 'linear-gradient(135deg, var(--color-btc-gold) 0%, var(--color-btc-gold-dark) 100%)',
              color: '#000',
            }}
          >
            Send
          </button>
        </div>
      </form>
    </div>
  );
}
