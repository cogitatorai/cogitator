interface StatusBadgeProps {
  status: string;
}

const STATUS_STYLES: Record<string, string> = {
  completed: 'text-green-500 border border-green-500/40 bg-green-900/10',
  failed: 'text-red-500 border border-red-500/40 bg-red-950/10',
  running: 'text-orange-500 border border-orange-500/40 bg-orange-900/10',
  retrying: 'text-orange-400 border border-orange-400/40 bg-orange-900/10',
  cancelled: 'text-zinc-400 border border-zinc-500/40 bg-zinc-800/30',
};

export default function StatusBadge({ status }: StatusBadgeProps) {
  const style = STATUS_STYLES[status] ?? 'text-zinc-500 border border-zinc-600/40 bg-zinc-800/30';

  return (
    <span className={`inline-block text-[13px] uppercase tracking-[0.2em] font-medium px-2 py-1 ${style}`}>
      {status || 'unknown'}
    </span>
  );
}
