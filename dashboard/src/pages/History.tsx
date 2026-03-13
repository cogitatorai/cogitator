import { useState, useCallback, useMemo, useEffect } from 'react';
import { Trash2 } from 'lucide-react';
import { fetchJSON, postJSON, usePolling } from '../api';
import type { RunListResult, Run, Task, ToolCallRecord } from '../api';
import Panel from '../components/Panel';
import PageHeader from '../components/PageHeader';
import StatusBadge from '../components/StatusBadge';

async function bulkDeleteRuns(ids: number[]): Promise<void> {
  const res = await fetch('/api/runs', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });
  if (!res.ok) throw new Error('Failed to delete runs. Please try again.');
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function formatTime(iso: string): string {
  if (!iso) return '';
  try {
    const d = new Date(iso);
    return d.toLocaleString([], {
      month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
  } catch {
    return iso;
  }
}

const STATUSES = ['', 'completed', 'failed', 'running', 'retrying', 'cancelled'];
const PAGE_SIZE = 50;

export default function HistoryPage() {
  const [status, setStatus] = useState('');
  const [taskId, setTaskId] = useState('');
  const [offset, setOffset] = useState(0);
  const [expandedRun, setExpandedRun] = useState<number | null>(null);
  const [runDetail, setRunDetail] = useState<Run | null>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [hiddenIds, setHiddenIds] = useState<Set<number>>(new Set());

  const buildUrl = useCallback(() => {
    const params = new URLSearchParams();
    params.set('limit', String(PAGE_SIZE));
    params.set('offset', String(offset));
    if (status) params.set('status', status);
    if (taskId) params.set('task_id', taskId);
    return `/api/runs?${params}`;
  }, [status, taskId, offset]);

  const { data: result, error } = usePolling<RunListResult>(
    () => fetchJSON(buildUrl()),
    5000,
    `${status}:${taskId}:${offset}`,
  );

  const { data: tasks } = usePolling<Task[]>(
    () => fetchJSON('/api/tasks'),
    30000,
  );

  const visibleRuns = useMemo(
    () => (result?.runs || []).filter((r) => !hiddenIds.has(r.id)),
    [result, hiddenIds],
  );
  const allIds = useMemo(() => visibleRuns.map((r) => r.id), [visibleRuns]);
  const allSelected = allIds.length > 0 && allIds.every((id) => selected.has(id));
  const someSelected = selected.size > 0;

  const toggleOne = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleAll = () => {
    if (allSelected) {
      setSelected(new Set());
    } else {
      setSelected(new Set(allIds));
    }
  };

  const handleExpand = async (runId: number) => {
    if (expandedRun === runId) {
      setExpandedRun(null);
      setRunDetail(null);
      return;
    }
    setExpandedRun(runId);
    try {
      const detail = await fetchJSON<Run>(`/api/runs/${runId}`);
      setRunDetail(detail);
    } catch {
      setRunDetail(null);
    }
  };

  const handleCancel = async (runId: number) => {
    try {
      await postJSON(`/api/runs/${runId}/cancel`, {});
      setExpandedRun(null);
      setRunDetail(null);
    } catch { /* polling will pick up the change */ }
  };

  const handleBulkDelete = (ids: number[]) => {
    setHiddenIds((prev) => new Set([...prev, ...ids]));
    setSelected(new Set());
    setExpandedRun(null);
    setRunDetail(null);
    bulkDeleteRuns(ids).catch(() => {
      setHiddenIds((prev) => {
        const next = new Set(prev);
        ids.forEach((id) => next.delete(id));
        return next;
      });
    });
  };

  const totalPages = result ? Math.ceil(result.total / PAGE_SIZE) : 0;
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  return (
    <div>
      <PageHeader title="Execution History" subtitle="Task run logs" />

      {/* Filters and bulk actions */}
      <div className="flex gap-3 mb-4 flex-wrap items-end">
        <div>
          <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1">Status</span>
          <select
            value={status}
            onChange={(e) => { setStatus(e.target.value); setOffset(0); }}
            className="bg-zinc-900 border border-zinc-700 p-2 text-zinc-300 text-base focus:border-orange-600 focus:outline-none"
          >
            <option value="" className="bg-zinc-900">All</option>
            {STATUSES.filter(Boolean).map((s) => (
              <option key={s} value={s} className="bg-zinc-900">{s}</option>
            ))}
          </select>
        </div>
        <div>
          <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1">Task</span>
          <select
            value={taskId}
            onChange={(e) => { setTaskId(e.target.value); setOffset(0); }}
            className="bg-zinc-900 border border-zinc-700 p-2 text-zinc-300 text-base focus:border-orange-600 focus:outline-none"
          >
            <option value="" className="bg-zinc-900">All tasks</option>
            {tasks?.map((t) => (
              <option key={t.id} value={String(t.id)} className="bg-zinc-900">{t.name}</option>
            ))}
          </select>
        </div>

        {someSelected && (
          <BulkActions
            selected={selected}
            runs={visibleRuns}
            onDelete={handleBulkDelete}
          />
        )}
      </div>

      {error && (
        <Panel className="border-red-500/30 mb-4">
          <p className="text-red-500 text-base">{error}</p>
        </Panel>
      )}

      <Panel>
        {visibleRuns.length === 0 ? (
          <p className="text-base text-zinc-600">No runs found.</p>
        ) : (
          <>
            <table className="w-full text-base">
              <thead>
                <tr className="border-b border-zinc-700">
                  <th className="w-8 pb-2">
                    <Checkbox checked={allSelected} indeterminate={someSelected && !allSelected} onChange={toggleAll} />
                  </th>
                  <Th>Task</Th>
                  <Th>Started</Th>
                  <Th>Duration</Th>
                  <Th>Status</Th>
                  <Th>Model</Th>
                </tr>
              </thead>
              <tbody>
                {visibleRuns.map((run) => (
                  <RunRow
                    key={run.id}
                    run={run}
                    checked={selected.has(run.id)}
                    onCheck={() => toggleOne(run.id)}
                    expanded={expandedRun === run.id}
                    detail={expandedRun === run.id ? runDetail : null}
                    onToggle={() => handleExpand(run.id)}
                    onCancel={() => handleCancel(run.id)}
                    onDelete={() => handleBulkDelete([run.id])}
                  />
                ))}
              </tbody>
            </table>

            {/* Pagination */}
            {totalPages > 1 && (
              <div className="flex items-center justify-between mt-4 pt-3 border-t border-zinc-700">
                <span className="text-sm text-zinc-500">
                  {result!.total} total runs, page {currentPage} of {totalPages}
                </span>
                <div className="flex gap-2">
                  <button
                    onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                    disabled={offset === 0}
                    className="bg-zinc-800 border border-zinc-700 px-3 py-1 text-sm text-zinc-400 hover:bg-zinc-700 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer transition-colors"
                  >
                    Prev
                  </button>
                  <button
                    onClick={() => setOffset(offset + PAGE_SIZE)}
                    disabled={offset + PAGE_SIZE >= result!.total}
                    className="bg-zinc-800 border border-zinc-700 px-3 py-1 text-sm text-zinc-400 hover:bg-zinc-700 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer transition-colors"
                  >
                    Next
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </Panel>
    </div>
  );
}

function BulkActions({ selected, runs, onDelete }: {
  selected: Set<number>; runs: Run[]; onDelete: (ids: number[]) => void;
}) {
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const count = selected.size;
  const selectedRuns = runs.filter((r) => selected.has(r.id));

  return (
    <>
      <div className="flex items-center gap-1">
        <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mr-2">
          {count} selected
        </span>
        <button
          type="button"
          onClick={() => setShowDeleteModal(true)}
          title="Delete selected runs"
          className="h-6 px-2 flex items-center justify-center border border-zinc-700 text-zinc-600 hover:text-red-400 hover:border-red-600/50 text-[12px] uppercase tracking-widest font-medium transition-colors cursor-pointer"
        >
          <Trash2 size={12} className="pointer-events-none" />
        </button>
      </div>

      {showDeleteModal && (
        <DeleteModal
          runs={selectedRuns}
          onCancel={() => setShowDeleteModal(false)}
          onConfirm={() => {
            onDelete([...selected]);
            setShowDeleteModal(false);
          }}
        />
      )}
    </>
  );
}

function DeleteModal({ runs, onCancel, onConfirm }: {
  runs: Run[]; onCancel: () => void; onConfirm: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onCancel}>
      <div
        className="hud-panel bg-zinc-900 max-w-sm w-full mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="text-base font-medium text-zinc-100 mb-3">
          Delete {runs.length === 1 ? 'run' : `${runs.length} runs`}?
        </h3>
        <p className="text-sm text-zinc-500 mb-3">This action cannot be undone.</p>
        <ul className="mb-4 space-y-1 max-h-40 overflow-y-auto">
          {runs.map((r) => (
            <li key={r.id} className="text-sm text-zinc-400 flex items-center gap-2">
              <span className="w-1 h-1 bg-zinc-600 shrink-0" />
              {r.task_name || `Run #${r.id}`} &middot; {formatTime(r.started_at)}
            </li>
          ))}
        </ul>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-4 py-2 text-[12px] uppercase tracking-widest font-medium border border-zinc-700 text-zinc-400 hover:text-zinc-100 hover:border-zinc-500 transition-colors cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            className="px-4 py-2 text-[12px] uppercase tracking-widest font-medium border border-red-600/50 text-red-500 bg-red-900/20 hover:bg-red-900/40 transition-colors cursor-pointer"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}

function Checkbox({ checked, indeterminate, onChange }: {
  checked: boolean; indeterminate?: boolean; onChange: () => void;
}) {
  return (
    <button
      onClick={onChange}
      className={`w-4 h-4 border flex items-center justify-center transition-colors cursor-pointer ${
        checked || indeterminate
          ? 'border-orange-600 bg-orange-600/20 text-orange-500'
          : 'border-zinc-700 text-transparent hover:border-zinc-500'
      }`}
    >
      <span className="text-[12px] leading-none">
        {indeterminate ? '\u2012' : checked ? '\u2713' : ''}
      </span>
    </button>
  );
}

function RunRow({ run, checked, onCheck, expanded, detail, onToggle, onCancel, onDelete }: {
  run: Run;
  checked: boolean;
  onCheck: () => void;
  expanded: boolean;
  detail: Run | null;
  onToggle: () => void;
  onCancel: () => void;
  onDelete: () => void;
}) {
  return (
    <>
      <tr className={`border-b border-zinc-700/50 transition-colors ${
        checked ? 'bg-orange-900/10' : 'hover:bg-zinc-800/30'
      }`}>
        <td className="py-2 w-8" onClick={(e) => e.stopPropagation()}>
          <Checkbox checked={checked} onChange={onCheck} />
        </td>
        <td className="py-2 text-zinc-300 cursor-pointer" onClick={onToggle}>{run.task_name}</td>
        <td className="py-2 text-zinc-500 text-sm cursor-pointer" onClick={onToggle}>{formatTime(run.started_at)}</td>
        <td className="py-2 text-zinc-400 cursor-pointer" onClick={onToggle}>
          <LiveDuration status={run.status} startedAt={run.started_at} serverMs={run.duration_ms} />
        </td>
        <td className="py-2 cursor-pointer" onClick={onToggle}><StatusBadge status={run.status} /></td>
        <td className="py-2 cursor-pointer" onClick={onToggle}>
          <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-400 bg-zinc-800 px-2 py-0.5">
            {run.model_used || 'n/a'}
          </span>
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={6} className="bg-zinc-900/80 p-4 border-b border-zinc-700">
            {!detail ? (
              <p className="text-zinc-500 text-base animate-pulse">Loading detail...</p>
            ) : (
              <div className="space-y-3">
                {detail.status === 'running' && (
                  <div className="flex items-center gap-3">
                    <p className="text-orange-500 text-base">Task is still running...</p>
                    <button
                      onClick={(e) => { e.stopPropagation(); onCancel(); }}
                      className="bg-red-950/50 border border-red-900/50 px-3 py-1 text-sm text-red-400 hover:bg-red-900/40 hover:text-red-300 cursor-pointer transition-colors"
                    >
                      Cancel
                    </button>
                  </div>
                )}
                {detail.error_message && (
                  <div>
                    <span className="text-[12px] uppercase tracking-widest font-medium text-red-500">
                      Error{detail.error_class ? ` (${detail.error_class})` : ''}
                    </span>
                    <pre className="text-sm text-red-400 mt-1 bg-red-950/20 border border-red-900/30 p-3 whitespace-pre-wrap">
                      {detail.error_message}
                    </pre>
                  </div>
                )}
                {detail.result_summary && (
                  <div>
                    <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Result</span>
                    <p className="text-sm text-zinc-300 mt-1">{detail.result_summary}</p>
                  </div>
                )}
                {detail.skills_used && detail.skills_used !== '[]' && (
                  <div>
                    <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Skills</span>
                    <p className="text-sm text-zinc-400 mt-1">{detail.skills_used}</p>
                  </div>
                )}
                {detail.tool_calls && detail.tool_calls.length > 0 && (
                  <div>
                    <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">
                      Tool Calls ({detail.tool_calls.length})
                    </span>
                    <div className="mt-1 border border-zinc-700 overflow-hidden">
                      <table className="w-full text-sm">
                        <thead>
                          <tr className="bg-zinc-800/50">
                            <th className="text-left text-[12px] uppercase tracking-widest font-medium text-zinc-500 px-2 py-1">Tool</th>
                            <th className="text-left text-[12px] uppercase tracking-widest font-medium text-zinc-500 px-2 py-1">Duration</th>
                            <th className="text-left text-[12px] uppercase tracking-widest font-medium text-zinc-500 px-2 py-1">Round</th>
                            <th className="text-left text-[12px] uppercase tracking-widest font-medium text-zinc-500 px-2 py-1">Status</th>
                          </tr>
                        </thead>
                        <tbody>
                          {detail.tool_calls.map((tc: ToolCallRecord, i: number) => (
                            <tr key={i} className="border-t border-zinc-800/50">
                              <td className="px-2 py-1 text-zinc-300">{tc.tool}</td>
                              <td className="px-2 py-1 text-zinc-400">{formatDuration(tc.duration_ms)}</td>
                              <td className="px-2 py-1 text-zinc-500">{tc.round}</td>
                              <td className="px-2 py-1">
                                {tc.error
                                  ? <span className="text-red-400" title={tc.error}>failed</span>
                                  : <span className="text-emerald-400">ok</span>
                                }
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
                {detail.trigger && (
                  <div>
                    <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Trigger</span>
                    <p className="text-sm text-zinc-400 mt-1">{detail.trigger}</p>
                  </div>
                )}
                {detail.session_key && (
                  <div>
                    <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Session</span>
                    <p className="text-sm text-zinc-400 mt-1">{detail.session_key}</p>
                  </div>
                )}
                <div className="flex justify-end pt-2 border-t border-zinc-700">
                  <button
                    onClick={(e) => { e.stopPropagation(); onDelete(); }}
                    className="flex items-center gap-1.5 px-3 py-1 text-[12px] uppercase tracking-widest font-medium border border-zinc-700 text-zinc-600 hover:text-red-400 hover:border-red-600/50 transition-colors cursor-pointer"
                  >
                    <Trash2 size={11} className="pointer-events-none" />
                    Delete
                  </button>
                </div>
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

function LiveDuration({ status, startedAt, serverMs }: {
  status: string; startedAt: string; serverMs: number;
}) {
  const [elapsed, setElapsed] = useState(() =>
    status === 'running' && startedAt
      ? Math.max(0, Date.now() - new Date(startedAt).getTime())
      : serverMs,
  );

  useEffect(() => {
    if (status !== 'running' || !startedAt) {
      setElapsed(serverMs);
      return;
    }
    const origin = new Date(startedAt).getTime();
    setElapsed(Math.max(0, Date.now() - origin));
    const id = setInterval(() => setElapsed(Math.max(0, Date.now() - origin)), 1000);
    return () => clearInterval(id);
  }, [status, startedAt, serverMs]);

  return <>{formatDuration(elapsed)}</>;
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th className="text-left text-[12px] uppercase tracking-widest font-medium text-zinc-500 pb-2">
      {children}
    </th>
  );
}
