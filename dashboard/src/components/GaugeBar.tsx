interface GaugeBarProps {
  used: number;
  total: number;
  label: string;
  unit?: string;
  color?: 'orange' | 'green' | 'blue';
}

const COLOR_MAP = {
  orange: {
    bar: 'bg-orange-600',
    text: 'text-orange-500',
    dot: 'bg-orange-500',
    track: 'bg-orange-900/20',
  },
  green: {
    bar: 'bg-green-500',
    text: 'text-green-500',
    dot: 'bg-green-500',
    track: 'bg-green-900/20',
  },
  blue: {
    bar: 'bg-blue-400',
    text: 'text-blue-400',
    dot: 'bg-blue-400',
    track: 'bg-blue-400/10',
  },
};

export default function GaugeBar({ used, total, label, unit = 'ACTIVE', color = 'orange' }: GaugeBarProps) {
  const pct = total > 0 ? Math.min((used / total) * 100, 100) : 0;
  const c = COLOR_MAP[color];

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500">
          {label}
        </span>
        <div className="flex items-center gap-2">
          {used > 0 && (
            <span className={`w-2 h-2 rounded-full ${c.dot} animate-pulse`} />
          )}
          <span className={`text-base font-medium ${c.text}`}>
            {used} / {total} {unit}
          </span>
        </div>
      </div>
      <div className={`h-2 w-full ${c.track}`}>
        <div
          className={`h-full ${c.bar} transition-all duration-500`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}
