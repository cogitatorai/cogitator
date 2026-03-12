import { useState, useEffect, useCallback, useRef } from 'react';
import { Bell } from 'lucide-react';
import { listNotifications, markNotificationRead, markAllNotificationsRead } from '../api';
import type { NotificationItem } from '../api';

function timeAgo(iso: string): string {
  if (!iso) return '';
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

interface Props {
  unreadCount: number;
  onUnreadChange: (count: number) => void;
  refreshKey?: number;
  onNavigateToTasks?: () => void;
}

export default function NotificationBell({ unreadCount, onUnreadChange, refreshKey, onNavigateToTasks }: Props) {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<NotificationItem[]>([]);
  const ref = useRef<HTMLDivElement>(null);

  const refresh = useCallback(async () => {
    try {
      const data = await listNotifications(20, 0);
      setItems(data.notifications);
      onUnreadChange(data.unread);
    } catch { /* ignore */ }
  }, [onUnreadChange]);

  useEffect(() => { if (open) refresh(); }, [open, refresh, refreshKey]);

  // Click-away
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open]);

  // Escape
  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false); };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [open]);

  const handleClick = async (item: NotificationItem) => {
    if (!item.read) {
      try {
        await markNotificationRead(item.id);
        setItems((prev) => prev.map((n) => n.id === item.id ? { ...n, read: true } : n));
        onUnreadChange(Math.max(0, unreadCount - 1));
      } catch { /* ignore */ }
    }
    setOpen(false);
    onNavigateToTasks?.();
  };

  const handleMarkAllRead = async () => {
    try {
      await markAllNotificationsRead();
      setItems((prev) => prev.map((n) => ({ ...n, read: true })));
      onUnreadChange(0);
    } catch { /* ignore */ }
  };

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="relative p-1.5 text-zinc-500 hover:text-zinc-300 transition-colors cursor-pointer"
        title="Notifications"
      >
        <Bell size={16} />
        {unreadCount > 0 && (
          <span className="absolute -top-1 -right-1 text-[9px] font-bold bg-orange-600 text-white px-1 min-w-[14px] text-center leading-[14px]">
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute left-0 top-full mt-2 w-80 bg-zinc-900 border border-zinc-700 shadow-xl z-50 max-h-96 flex flex-col">
          <div className="flex items-center justify-between px-3 py-2 border-b border-zinc-700">
            <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">
              Notifications
            </span>
            {unreadCount > 0 && (
              <button
                onClick={handleMarkAllRead}
                className="text-[11px] uppercase tracking-widest text-zinc-500 hover:text-orange-400 transition-colors cursor-pointer"
              >
                Mark all read
              </button>
            )}
          </div>
          <div className="overflow-y-auto flex-1">
            {items.length === 0 ? (
              <p className="text-sm text-zinc-600 px-3 py-4 text-center">No notifications.</p>
            ) : (
              items.map((n) => (
                <button
                  key={n.id}
                  onClick={() => handleClick(n)}
                  className={`w-full text-left px-3 py-2.5 border-b border-zinc-800 hover:bg-zinc-800/50 transition-colors cursor-pointer flex items-center gap-2 ${
                    !n.read ? 'bg-zinc-800/40' : ''
                  }`}
                >
                  {!n.read && (
                    <span className={`w-2 h-2 rounded-full shrink-0 ${n.status === 'failed' ? 'bg-red-500' : 'bg-orange-500'}`} />
                  )}
                  <span className={`flex-1 text-sm truncate ${n.read ? 'text-zinc-500' : 'text-zinc-100 font-medium'}`}>
                    {n.task_name}
                  </span>
                  <span className={`text-[11px] uppercase tracking-widest font-medium ${
                    n.status === 'failed' ? 'text-red-500' : 'text-emerald-500'
                  }`}>
                    {n.status === 'failed' ? 'fail' : 'ok'}
                  </span>
                  <span className="text-[11px] text-zinc-600 shrink-0">{timeAgo(n.created_at)}</span>
                </button>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}
