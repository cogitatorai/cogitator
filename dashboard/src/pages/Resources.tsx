import { fetchJSON, usePolling, fetchDailyTokenStats } from '../api';
import type { SystemStatus, DailyTokenStats } from '../api';
import TokenChart from '../components/TokenChart';

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}

export default function Resources() {
  const { data: status, error, loading } = usePolling<SystemStatus>(
    () => fetchJSON('/api/status'),
    5000,
  );

  const { data: usageData } = usePolling<{ stats: DailyTokenStats[] }>(
    () => fetchDailyTokenStats(14),
    30000,
  );

  if (loading && !status) {
    return (
      <div className="min-h-full">
        <HudHeader />
        <p className="text-base text-zinc-600 animate-pulse mt-8 ml-2">Connecting to daemon...</p>
      </div>
    );
  }

  if (error && !status) {
    return (
      <div className="min-h-full">
        <HudHeader />
        <div className="hud-panel mt-8">
          <p className="text-red-500 text-base">{error}</p>
          <p className="text-zinc-600 text-sm mt-2">
            Make sure the cogitator daemon is running.
          </p>
        </div>
      </div>
    );
  }

  const s = status!;
  const stats = usageData?.stats ?? [];

  return (
    <div className="min-h-full relative overflow-hidden">
      <HudHeader />

      {/* Status bar */}
      <div className="flex items-center gap-4 mb-6 mt-1 px-1">
        <span className="flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-green-500 animate-pulse" />
          <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-green-500">Active</span>
        </span>
        <span className="text-[13px] uppercase tracking-[0.2em] text-zinc-600">
          {new Date().toLocaleTimeString('en-US', { hour12: false })} UTC
        </span>
      </div>

      {/* System */}
      <div className="hud-panel mb-6">
        <SectionLabel text="System" />
        <div className="grid grid-cols-3 gap-6 mt-4">
          <MemStat label="Uptime" value={formatUptime(s.uptime_seconds)} />
          <MemStat label="Memory in Use" value={`${s.memory.alloc_mb}`} unit="MB" />
          <MemStat label="Memory Reserved" value={`${s.memory.sys_mb}`} unit="MB" />
        </div>
      </div>

      {/* Component Counts */}
      <div className="hud-panel mb-6">
        <SectionLabel text="Components" />
        <div className="grid grid-cols-5 gap-4 mt-4">
          <ComponentCard label="Sessions" count={s.components.sessions} color="orange" />
          <ComponentCard label="Memory Nodes" count={s.components.memory_nodes} color="blue" />
          <ComponentCard label="Tasks" count={s.components.tasks} color="green" />
          <ComponentCard label="Tools" count={s.components.tools} color="orange" />
          <ComponentCard label="Skills" count={s.components.skills} color="blue" />
        </div>
      </div>

      {/* Token Usage Chart */}
      <div className="hud-panel hud-panel-orange">
        <SectionLabel text="Token Usage (Last 14 Days)" />
        <div className="h-[380px] mt-4">
          <TokenChart stats={stats} />
        </div>
      </div>
    </div>
  );
}

function HudHeader() {
  return (
    <div className="mb-4">
      <h2 className="text-4xl font-semibold uppercase tracking-[0.1em] text-zinc-100">
        Resources
      </h2>
      <div className="h-[2px] w-16 bg-orange-600 mt-2" />
      <p className="text-[13px] text-zinc-500 mt-3 uppercase tracking-[0.2em]">
        System diagnostics and token consumption
      </p>
    </div>
  );
}

function SectionLabel({ text }: { text: string }) {
  return (
    <div className="flex items-center gap-3">
      <h3 className="text-sm uppercase tracking-[0.2em] font-medium text-zinc-400">
        {text}
      </h3>
      <div className="flex-1 border-t border-zinc-700/60" />
    </div>
  );
}

function MemStat({ label, value, unit }: { label: string; value: string; unit?: string }) {
  return (
    <div>
      <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500 block mb-2">{label}</span>
      <span className="text-3xl font-semibold text-zinc-100">{value}</span>
      {unit && <span className="text-base text-zinc-500 ml-1.5">{unit}</span>}
    </div>
  );
}

function ComponentCard({ label, count, color }: {
  label: string; count: number; color: 'orange' | 'blue' | 'green';
}) {
  const textMap = {
    orange: 'text-orange-500',
    blue: 'text-blue-400',
    green: 'text-green-500',
  };

  return (
    <div className="text-center py-2">
      <span className={`text-4xl font-semibold ${textMap[color]} block mb-1`}>{count}</span>
      <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500">{label}</span>
    </div>
  );
}
