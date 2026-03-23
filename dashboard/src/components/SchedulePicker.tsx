import { useState, useMemo } from 'react';

type Frequency =
  | 'every5m' | 'every30m' | 'hourly' | 'every2h' | 'every6h'
  | 'daily' | 'weekly' | 'monthly' | 'yearly' | 'custom';

interface Preset {
  key: Frequency;
  label: string;
  cron: string | null;
  needsTime: boolean;
  needsDow: boolean;
  needsDom: boolean;
  needsMonth: boolean;
}

const PRESETS: Preset[] = [
  { key: 'every5m',  label: 'Every 5 min',  cron: '*/5 * * * *',  needsTime: false, needsDow: false, needsDom: false, needsMonth: false },
  { key: 'every30m', label: 'Every 30 min', cron: '*/30 * * * *', needsTime: false, needsDow: false, needsDom: false, needsMonth: false },
  { key: 'hourly',   label: 'Hourly',       cron: '0 * * * *',    needsTime: false, needsDow: false, needsDom: false, needsMonth: false },
  { key: 'every2h',  label: 'Every 2h',     cron: '0 */2 * * *',  needsTime: false, needsDow: false, needsDom: false, needsMonth: false },
  { key: 'every6h',  label: 'Every 6h',     cron: '0 */6 * * *',  needsTime: false, needsDow: false, needsDom: false, needsMonth: false },
  { key: 'daily',    label: 'Daily',         cron: null,           needsTime: true,  needsDow: false, needsDom: false, needsMonth: false },
  { key: 'weekly',   label: 'Weekly',        cron: null,           needsTime: true,  needsDow: true,  needsDom: false, needsMonth: false },
  { key: 'monthly',  label: 'Monthly',       cron: null,           needsTime: true,  needsDow: false, needsDom: true,  needsMonth: false },
  { key: 'yearly',   label: 'Yearly',        cron: null,           needsTime: true,  needsDow: false, needsDom: true,  needsMonth: true },
];

const DAYS_OF_WEEK = [
  { value: '1', label: 'Mon' },
  { value: '2', label: 'Tue' },
  { value: '3', label: 'Wed' },
  { value: '4', label: 'Thu' },
  { value: '5', label: 'Fri' },
  { value: '6', label: 'Sat' },
  { value: '0', label: 'Sun' },
];

const MONTHS = [
  { value: '1', label: 'Jan' }, { value: '2', label: 'Feb' },
  { value: '3', label: 'Mar' }, { value: '4', label: 'Apr' },
  { value: '5', label: 'May' }, { value: '6', label: 'Jun' },
  { value: '7', label: 'Jul' }, { value: '8', label: 'Aug' },
  { value: '9', label: 'Sep' }, { value: '10', label: 'Oct' },
  { value: '11', label: 'Nov' }, { value: '12', label: 'Dec' },
];

/** True when a cron field is a single plain value (no ranges, lists, or steps). */
function isSimple(field: string): boolean {
  return /^\d+$/.test(field);
}

/** Parse an existing cron expression into picker state. */
function parseCron(expr: string): { freq: Frequency; time: string; dow: string; dom: string; month: string } {
  const defaults = { freq: 'daily' as Frequency, time: '09:00', dow: '1', dom: '1', month: '1' };
  if (!expr) return defaults;

  const fields = expr.trim().split(/\s+/);
  if (fields.length !== 5) return defaults;

  const [minute, hour, dom, month, dow] = fields;
  const simpleTime = isSimple(minute) && isSimple(hour);

  // Fixed-cron presets
  for (const p of PRESETS) {
    if (p.cron && p.cron === expr) {
      return { ...defaults, freq: p.key };
    }
  }

  // Daily: MM HH * * *
  if (dom === '*' && month === '*' && dow === '*' && simpleTime) {
    return { ...defaults, freq: 'daily', time: `${pad(hour)}:${pad(minute)}` };
  }

  // Weekly: MM HH * * DOW (DOW may be a single day or comma-separated list like 1,3,5)
  if (dom === '*' && month === '*' && dow !== '*' && simpleTime) {
    return { ...defaults, freq: 'weekly', time: `${pad(hour)}:${pad(minute)}`, dow };
  }

  // Monthly: MM HH DOM * *
  if (month === '*' && dow === '*' && isSimple(dom) && simpleTime) {
    return { ...defaults, freq: 'monthly', time: `${pad(hour)}:${pad(minute)}`, dom };
  }

  // Yearly: MM HH DOM MON *
  if (dow === '*' && isSimple(month) && isSimple(dom) && simpleTime) {
    return { ...defaults, freq: 'yearly', time: `${pad(hour)}:${pad(minute)}`, dom, month };
  }

  return { ...defaults, freq: 'custom' };
}

function pad(v: string): string {
  return v.length === 1 ? '0' + v : v;
}

function buildCron(freq: Frequency, time: string, dow: string, dom: string, month: string): string {
  const [hh, mm] = time.split(':').map((s) => parseInt(s, 10));
  const minute = isNaN(mm) ? '0' : String(mm);
  const hour = isNaN(hh) ? '9' : String(hh);

  const preset = PRESETS.find((p) => p.key === freq);
  if (preset?.cron) return preset.cron;

  switch (freq) {
    case 'daily':   return `${minute} ${hour} * * *`;
    case 'weekly':  return `${minute} ${hour} * * ${dow}`;
    case 'monthly': return `${minute} ${hour} ${dom} * *`;
    case 'yearly':  return `${minute} ${hour} ${dom} ${month} *`;
    default:        return `${minute} ${hour} * * *`;
  }
}

interface SchedulePickerProps {
  value: string;
  onChange: (cron: string) => void;
}

export default function SchedulePicker({ value, onChange }: SchedulePickerProps) {
  const initial = useMemo(() => parseCron(value), []);
  const [freq, setFreq] = useState<Frequency>(initial.freq);
  const [time, setTime] = useState(initial.time);
  const [dow, setDow] = useState(initial.dow);
  const [dom, setDom] = useState(initial.dom);
  const [month, setMonth] = useState(initial.month);

  const preset = PRESETS.find((p) => p.key === freq);
  const isCustom = freq === 'custom';

  const emit = (f: Frequency, t: string, d: string, dm: string, mo: string) => {
    if (f === 'custom') return;
    onChange(buildCron(f, t, d, dm, mo));
  };

  const handleFreq = (f: Frequency) => {
    setFreq(f);
    emit(f, time, dow, dom, month);
  };

  const handleTime = (t: string) => {
    setTime(t);
    emit(freq, t, dow, dom, month);
  };

  const handleDow = (d: string) => {
    setDow(d);
    emit(freq, time, d, dom, month);
  };

  const handleDom = (d: string) => {
    setDom(d);
    emit(freq, time, dow, d, month);
  };

  const handleMonth = (m: string) => {
    setMonth(m);
    emit(freq, time, dow, dom, m);
  };

  return (
    <div className="space-y-2">
      {/* Frequency buttons */}
      <div className="flex flex-wrap gap-0">
        {PRESETS.map((p, i) => (
          <button
            key={p.key}
            type="button"
            onClick={() => handleFreq(p.key)}
            className={`px-2.5 py-1.5 text-[11px] uppercase tracking-widest font-medium border transition-colors cursor-pointer ${
              freq === p.key
                ? 'bg-orange-900/30 border-orange-600 text-orange-500 z-10'
                : 'bg-zinc-900 border-zinc-700 text-zinc-500 hover:text-zinc-300'
            } ${i > 0 ? '-ml-px' : ''}`}
          >
            {p.label}
          </button>
        ))}
        {isCustom && (
          <span className="px-2.5 py-1.5 text-[11px] uppercase tracking-widest font-medium border bg-zinc-800 border-orange-600 text-orange-500">
            Custom
          </span>
        )}
      </div>

      {/* Contextual controls */}
      {!isCustom && preset && (preset.needsTime || preset.needsDow || preset.needsDom || preset.needsMonth) && (
        <div className="flex items-center gap-3 flex-wrap">
          {preset.needsMonth && (
            <div className="flex items-center gap-1.5">
              <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">Month</span>
              <select
                value={month}
                onChange={(e) => handleMonth(e.target.value)}
                className="bg-zinc-950 border border-zinc-800 text-zinc-300 text-sm px-2 py-1 focus:border-orange-600 focus:outline-none cursor-pointer"
              >
                {MONTHS.map((m) => (
                  <option key={m.value} value={m.value}>{m.label}</option>
                ))}
              </select>
            </div>
          )}
          {preset.needsDom && (
            <div className="flex items-center gap-1.5">
              <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">Day</span>
              <select
                value={dom}
                onChange={(e) => handleDom(e.target.value)}
                className="bg-zinc-950 border border-zinc-800 text-zinc-300 text-sm px-2 py-1 focus:border-orange-600 focus:outline-none cursor-pointer"
              >
                {Array.from({ length: 31 }, (_, i) => i + 1).map((d) => (
                  <option key={d} value={String(d)}>{d}</option>
                ))}
              </select>
            </div>
          )}
          {preset.needsDow && (
            <div className="flex items-center gap-1.5">
              <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">Day</span>
              <select
                value={dow}
                onChange={(e) => handleDow(e.target.value)}
                className="bg-zinc-950 border border-zinc-800 text-zinc-300 text-sm px-2 py-1 focus:border-orange-600 focus:outline-none cursor-pointer"
              >
                {DAYS_OF_WEEK.map((d) => (
                  <option key={d.value} value={d.value}>{d.label}</option>
                ))}
              </select>
            </div>
          )}
          {preset.needsTime && (
            <div className="flex items-center gap-1.5">
              <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">At</span>
              <input
                type="time"
                value={time}
                onChange={(e) => handleTime(e.target.value)}
                className="bg-zinc-950 border border-zinc-800 text-zinc-300 text-sm px-2 py-1 focus:border-orange-600 focus:outline-none"
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
