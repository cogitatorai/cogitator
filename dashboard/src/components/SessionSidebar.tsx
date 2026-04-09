import { useState } from 'react';
import { Plus, PanelLeftOpen, PanelLeftClose, Trash2, Lock, ListTodo } from 'lucide-react';
import type { Session, MeteringStatus } from '../api';

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return n.toString();
}

interface SessionSidebarProps {
  sessions: Session[];
  activeKey: string | null;
  onSelect: (key: string) => void;
  onNew: (isPrivate?: boolean) => void;
  onDelete: (key: string) => void;
  collapsed: boolean;
  onToggle: () => void;
  pinnedSession?: Session | null;
  unreadSessions?: Set<string>;
  metering?: MeteringStatus | null;
}

function timeAgo(iso: string): string {
  const seconds = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

function sessionLabel(s: Session): string {
  if (s.summary) return s.summary;
  if (s.chat_id && s.chat_id !== 'dashboard') return s.chat_id;
  return new Date(s.created_at).toLocaleDateString([], { month: 'short', day: 'numeric' });
}

const CHANNEL_LABELS: Record<string, string> = {
  telegram: 'Telegram',
  whatsapp: 'WhatsApp',
};

function channelLabel(channel: string): string {
  return CHANNEL_LABELS[channel] || channel;
}

export default function SessionSidebar({
  sessions, activeKey, onSelect, onNew, onDelete, collapsed, onToggle, pinnedSession, unreadSessions, metering,
}: SessionSidebarProps) {
  const [confirmKey, setConfirmKey] = useState<string | null>(null);

  if (collapsed) {
    return (
      <div className="w-10 shrink-0 border-r border-zinc-700 bg-zinc-900/80 flex flex-col items-center py-2 gap-2">
        <button
          onClick={onToggle}
          className="p-2 text-zinc-500 hover:text-zinc-300 transition-colors cursor-pointer"
          title="Show sessions"
        >
          <PanelLeftOpen size={16} />
        </button>
        <button
          onClick={() => onNew()}
          className="p-2 text-orange-500 hover:text-orange-400 transition-colors cursor-pointer"
          title="New chat"
        >
          <Plus size={16} />
        </button>
        <button
          onClick={() => onNew(true)}
          className="p-2 text-amber-500 hover:text-amber-400 transition-colors cursor-pointer"
          title="New private chat"
        >
          <Lock size={12} />
        </button>
        {pinnedSession && (
          <button
            onClick={() => onSelect(pinnedSession.key)}
            className={`p-2 transition-colors cursor-pointer ${
              pinnedSession.key === activeKey ? 'text-orange-500' : 'text-zinc-500 hover:text-zinc-300'
            }`}
            title="Tasks"
          >
            <ListTodo size={16} />
          </button>
        )}
      </div>
    );
  }

  const chatSessions = sessions.filter((s) => s.channel === 'web');
  const appSessions = sessions.filter((s) => s.channel !== 'web' && s.channel !== 'task');

  return (
    <div className="w-64 shrink-0 border-r border-zinc-700 bg-zinc-900/80 flex flex-col min-h-0">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-zinc-700">
        <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500">
          Chats
        </span>
        <button
          onClick={onToggle}
          className="p-1 text-zinc-500 hover:text-zinc-300 transition-colors cursor-pointer"
        >
          <PanelLeftClose size={14} />
        </button>
      </div>

      {/* New chat buttons */}
      <div className="px-3 py-2 border-b border-zinc-700 space-y-1.5">
        <button
          onClick={() => onNew()}
          className="w-full flex items-center justify-center gap-2 px-3 py-2 text-[13px] uppercase tracking-[0.2em] font-medium text-orange-500 border border-orange-600/40 hover:border-orange-500 hover:bg-orange-900/20 transition-colors cursor-pointer"
        >
          <Plus size={12} />
          New Chat
        </button>
        <button
          onClick={() => onNew(true)}
          className="w-full flex items-center justify-center gap-2 px-3 py-2 text-[13px] uppercase tracking-[0.2em] font-medium text-amber-500 border border-amber-600/40 hover:border-amber-500 hover:bg-amber-900/20 transition-colors cursor-pointer"
        >
          <Lock size={12} />
          Private Chat
        </button>
      </div>

      {/* Pinned sessions */}
      {pinnedSession && (
        <div className="px-1 py-1 border-b border-zinc-700">
          <div
            role="button"
            tabIndex={0}
            onClick={() => onSelect(pinnedSession.key)}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onSelect(pinnedSession.key); }}
            className={`w-full text-left px-2 py-[14px] border-l-2 transition-colors cursor-pointer ${
              pinnedSession.key === activeKey
                ? 'border-l-orange-500 bg-orange-900/10'
                : 'border-l-zinc-600 hover:border-l-zinc-400 hover:bg-zinc-800/30'
            }`}
          >
            <span className="text-[13px] uppercase tracking-[0.15em] font-bold text-zinc-300 flex items-center gap-2">
              <ListTodo size={14} className="text-orange-500/70 shrink-0" />
              Tasks
            </span>
          </div>
        </div>
      )}

      {/* Chat sessions (web) */}
      <div className="flex-1 overflow-y-auto py-1">
        {chatSessions.length === 0 ? (
          <p className="text-sm text-zinc-600 px-3 py-4 text-center">No conversations yet.</p>
        ) : (
          <div className="space-y-0.5 px-1">
            {chatSessions.map((s) => {
              const isActive = s.key === activeKey;
              const isConfirming = confirmKey === s.key;
              const isUnread = !isActive && unreadSessions?.has(s.key);

              return (
                <div key={s.key} className="group relative">
                  <div
                    role="button"
                    tabIndex={0}
                    onClick={() => !isConfirming && onSelect(s.key)}
                    onKeyDown={(e) => { if (!isConfirming && (e.key === 'Enter' || e.key === ' ')) onSelect(s.key); }}
                    className={`w-full text-left px-2 py-2 border transition-colors ${
                      isConfirming
                        ? 'border-red-500/30 bg-red-900/10'
                        : isActive
                          ? 'border-orange-600/50 bg-orange-900/10 cursor-pointer'
                          : 'border-transparent hover:border-zinc-600 hover:bg-zinc-800/30 cursor-pointer'
                    }`}
                  >
                    <div className="flex items-center justify-between gap-1">
                      <span className="text-base text-zinc-300 truncate flex items-center gap-1.5">
                        {isUnread && <span className="w-1.5 h-1.5 rounded-full bg-orange-500 shrink-0" />}
                        {s.private && <Lock size={12} className="text-amber-500 shrink-0" />}
                        {sessionLabel(s)}
                      </span>
                      {!isConfirming && (
                        <button
                          onClick={(e) => { e.stopPropagation(); setConfirmKey(s.key); }}
                          className="p-0.5 text-zinc-700 hover:text-red-500 opacity-0 group-hover:opacity-100 transition-opacity cursor-pointer shrink-0"
                          title="Delete session"
                        >
                          <Trash2 size={12} />
                        </button>
                      )}
                    </div>
                    {isConfirming ? (
                      <div className="flex items-center gap-3 mt-0.5">
                        <span className="text-red-500 font-medium uppercase tracking-widest text-[11px]">Delete?</span>
                        <button
                          onClick={(e) => { e.stopPropagation(); onDelete(s.key); setConfirmKey(null); }}
                          className="text-red-500 hover:text-red-400 font-medium text-[11px] uppercase tracking-widest cursor-pointer"
                        >
                          Yes
                        </button>
                        <button
                          onClick={(e) => { e.stopPropagation(); setConfirmKey(null); }}
                          className="text-zinc-500 hover:text-zinc-300 font-medium text-[11px] uppercase tracking-widest cursor-pointer"
                        >
                          Cancel
                        </button>
                      </div>
                    ) : (
                      <span className="text-[12px] text-zinc-600">{timeAgo(s.last_active)}</span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {/* App sessions (Telegram, etc.) */}
        {appSessions.length > 0 && (
          <>
            <div className="px-3 py-2 mt-2 border-t border-zinc-700">
              <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500">
                Apps
              </span>
            </div>
            <div className="space-y-0.5 px-1">
              {appSessions.map((s) => {
                const isActive = s.key === activeKey;
                const isUnread = !isActive && unreadSessions?.has(s.key);
                return (
                  <div
                    key={s.key}
                    role="button"
                    tabIndex={0}
                    onClick={() => onSelect(s.key)}
                    onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onSelect(s.key); }}
                    className={`w-full text-left px-2 py-2 border transition-colors cursor-pointer ${
                      isActive
                        ? 'border-orange-600/50 bg-orange-900/10'
                        : 'border-transparent hover:border-zinc-700 hover:bg-zinc-800/30'
                    }`}
                  >
                    <div className="flex items-center gap-2">
                      {isUnread && <span className="w-1.5 h-1.5 rounded-full bg-orange-500 shrink-0" />}
                      <span className="text-[11px] uppercase tracking-widest font-medium text-blue-400/70 shrink-0">
                        {channelLabel(s.channel)}
                      </span>
                      <span className="text-base text-zinc-300 truncate">{sessionLabel(s)}</span>
                    </div>
                    <span className="text-[12px] text-zinc-600">{timeAgo(s.last_active)}</span>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </div>

      {/* Metering indicator (SaaS) */}
      {metering && !metering.uncapped && (
        <div className="px-3 py-2 border-t border-zinc-700">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] uppercase tracking-[0.15em] text-zinc-500">Usage</span>
            <span className="text-[11px] tabular-nums text-zinc-400">{metering.usage_pct.toFixed(2)}%</span>
          </div>
          <div className="w-full h-1 bg-zinc-700 rounded-full overflow-hidden">
            <div
              className={`h-full rounded-full ${
                metering.usage_pct < 80 ? 'bg-green-500' :
                metering.usage_pct < 95 ? 'bg-yellow-500' : 'bg-red-500'
              }`}
              style={{ width: `${Math.min(100, metering.usage_pct)}%` }}
            />
          </div>
          <div className="flex items-center justify-between mt-1">
            <span className="text-[10px] text-zinc-600">{formatTokens(metering.weighted_usage)} / {formatTokens(metering.token_limit)}</span>
            <span className="text-[10px] text-zinc-600 uppercase">{metering.tier}</span>
          </div>
        </div>
      )}
    </div>
  );
}
