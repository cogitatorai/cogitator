import { useState, useCallback, useMemo } from 'react';
import { Trash2 } from 'lucide-react';
import { fetchJSON, postJSON, putJSON, deleteJSON, usePolling } from '../api';
import type { Task } from '../api';
import { useAuth } from '../auth';
import Panel from '../components/Panel';
import PageHeader from '../components/PageHeader';
import StripedButton from '../components/StripedButton';
import SchedulePicker from '../components/SchedulePicker';

export default function Tasks() {
  const { isAdmin } = useAuth();
  const { data: tasks, error } = usePolling<Task[]>(
    () => fetchJSON('/api/tasks'),
    5000,
  );
  const [showCreate, setShowCreate] = useState(false);
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [hiddenIds, setHiddenIds] = useState<Set<number>>(new Set());
  const [ownerFilter, setOwnerFilter] = useState<string>('');

  // Unique owner names for the filter dropdown (admin only).
  const owners = useMemo(() => {
    if (!isAdmin || !tasks) return [];
    const names = new Set(tasks.map((t) => t.owner_name || '').filter(Boolean));
    return [...names].sort();
  }, [isAdmin, tasks]);

  const visibleTasks = useMemo(
    () => (tasks || []).filter((t) => {
      if (hiddenIds.has(t.id)) return false;
      if (ownerFilter && (t.owner_name || '') !== ownerFilter) return false;
      return true;
    }),
    [tasks, hiddenIds, ownerFilter],
  );
  const allIds = useMemo(() => visibleTasks.map((t) => t.id), [visibleTasks]);
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

  return (
    <div>
      <PageHeader title="Tasks" subtitle="Scheduled task management" />

      <div className="flex items-center gap-3 mb-4">
        <StripedButton onClick={() => setShowCreate(!showCreate)}>
          {showCreate ? 'Cancel' : '+ Create Task'}
        </StripedButton>

        {isAdmin && owners.length > 1 && (
          <select
            value={ownerFilter}
            onChange={(e) => setOwnerFilter(e.target.value)}
            className="h-[38px] bg-zinc-900 border border-zinc-700 text-zinc-300 text-[12px] uppercase tracking-widest font-medium px-3 focus:border-orange-600 focus:outline-none cursor-pointer"
          >
            <option value="">All owners</option>
            {owners.map((name) => (
              <option key={name} value={name}>{name}</option>
            ))}
          </select>
        )}

        {someSelected && (
          <BulkActions
            selected={selected}
            tasks={visibleTasks}
            onDone={() => setSelected(new Set())}
            onDelete={(ids) => {
              setHiddenIds((prev) => new Set([...prev, ...ids]));
              setSelected(new Set());
              ids.forEach((id) => deleteJSON(`/api/tasks/${id}`).catch(() => {
                setHiddenIds((prev) => {
                  const next = new Set(prev);
                  next.delete(id);
                  return next;
                });
              }));
            }}
          />
        )}
      </div>

      {showCreate && <CreateForm onDone={() => setShowCreate(false)} />}

      {error && (
        <Panel className="border-red-500/30 mb-4">
          <p className="text-red-500 text-base">{error}</p>
        </Panel>
      )}

      <Panel>
        {visibleTasks.length === 0 ? (
          <p className="text-base text-zinc-600">No tasks configured.</p>
        ) : (
          <table className="w-full text-base">
            <thead>
              <tr className="border-b border-zinc-700">
                <th className="w-8 pb-2">
                  <Checkbox checked={allSelected} indeterminate={someSelected && !allSelected} onChange={toggleAll} />
                </th>
                <Th>Name</Th>
                {isAdmin && <Th>Owner</Th>}
                <Th>Schedule</Th>
                <Th>Model</Th>
                <Th>Runs</Th>
                <Th>Last</Th>
                <Th>Enabled</Th>
              </tr>
            </thead>
            <tbody>
              {visibleTasks.map((task) => (
                <TaskRow
                  key={task.id}
                  task={task}
                  checked={selected.has(task.id)}
                  onCheck={() => toggleOne(task.id)}
                  expanded={expandedId === task.id}
                  onToggleExpand={() => setExpandedId(expandedId === task.id ? null : task.id)}
                  showOwner={isAdmin}
                />
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </div>
  );
}

function BulkActions({ selected, tasks, onDone, onDelete }: {
  selected: Set<number>; tasks: Task[]; onDone: () => void; onDelete: (ids: number[]) => void;
}) {
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const count = selected.size;

  const handleBulkRun = useCallback(async () => {
    const ids = [...selected];
    await Promise.allSettled(ids.map((id) => postJSON(`/api/tasks/${id}/trigger`, {})));
    onDone();
  }, [selected, onDone]);

  const handleBulkEnable = useCallback(async (enabled: boolean) => {
    const ids = [...selected];
    await Promise.allSettled(ids.map((id) => putJSON(`/api/tasks/${id}`, { enabled })));
    onDone();
  }, [selected, onDone]);

  const selectedTasks = tasks.filter((t) => selected.has(t.id));
  const hasManual = selectedTasks.some((t) => t.allow_manual);

  return (
    <>
      <div className="flex items-center gap-1">
        <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mr-2">
          {count} selected
        </span>

        {hasManual && (
          <BulkButton onClick={handleBulkRun} title="Run selected tasks">
            {'\u25B6'}
          </BulkButton>
        )}
        <BulkButton onClick={() => handleBulkEnable(true)} title="Enable selected">
          ON
        </BulkButton>
        <BulkButton onClick={() => handleBulkEnable(false)} title="Disable selected">
          OFF
        </BulkButton>
        <BulkButton
          onClick={() => setShowDeleteModal(true)}
          title="Delete selected tasks"
          className="border-zinc-700 text-zinc-600 hover:text-red-400 hover:border-red-600/50"
        >
          <Trash2 size={12} className="pointer-events-none" />
        </BulkButton>
      </div>

      {showDeleteModal && (
        <DeleteModal
          tasks={selectedTasks}
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

function DeleteModal({ tasks, onCancel, onConfirm }: {
  tasks: Task[]; onCancel: () => void; onConfirm: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onCancel}>
      <div
        className="hud-panel max-w-sm w-full mx-4"
        style={{ background: 'var(--color-zinc-900, #18181b)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="text-base font-medium text-zinc-100 mb-3">
          Delete {tasks.length === 1 ? 'task' : `${tasks.length} tasks`}?
        </h3>
        <p className="text-sm text-zinc-500 mb-3">This action cannot be undone.</p>
        <ul className="mb-4 space-y-1">
          {tasks.map((t) => (
            <li key={t.id} className="text-sm text-zinc-400 flex items-center gap-2">
              <span className="w-1 h-1 bg-zinc-600 shrink-0" />
              {t.name}
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

function BulkButton({ onClick, title, className, children }: {
  onClick: () => void; title: string; className?: string; children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      className={`h-6 px-2 flex items-center justify-center border text-[12px] uppercase tracking-widest font-medium transition-colors cursor-pointer ${
        className || 'border-zinc-700 text-zinc-500 hover:text-orange-400 hover:border-orange-600/50'
      }`}
    >
      {children}
    </button>
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

function TaskRow({ task, checked, onCheck, expanded, onToggleExpand, showOwner }: {
  task: Task; checked: boolean; onCheck: () => void;
  expanded: boolean; onToggleExpand: () => void; showOwner?: boolean;
}) {
  const [toggling, setToggling] = useState(false);
  const [triggerState, setTriggerState] = useState<'idle' | 'running' | 'done' | 'error'>('idle');
  const [editPrompt, setEditPrompt] = useState(task.prompt);
  const [editCron, setEditCron] = useState(task.cron_expr || '');
  const [editModel, setEditModel] = useState(task.model_tier || 'auto');
  const [saving, setSaving] = useState(false);
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saved' | 'error'>('idle');
  const dirty = editPrompt !== task.prompt
    || editCron !== (task.cron_expr || '')
    || editModel !== (task.model_tier || 'auto');

  const handleSave = async () => {
    if (!dirty) return;
    setSaving(true);
    setSaveStatus('idle');
    const patch: Record<string, string> = {};
    if (editPrompt !== task.prompt) patch.prompt = editPrompt;
    if (editCron !== (task.cron_expr || '')) patch.cron_expr = editCron;
    if (editModel !== (task.model_tier || 'auto')) patch.model_tier = editModel;
    try {
      await putJSON(`/api/tasks/${task.id}`, patch);
      setSaveStatus('saved');
      setTimeout(() => setSaveStatus('idle'), 2000);
    } catch {
      setSaveStatus('error');
      setTimeout(() => setSaveStatus('idle'), 3000);
    }
    setSaving(false);
  };

  const handleToggle = async () => {
    setToggling(true);
    try {
      await putJSON(`/api/tasks/${task.id}`, { enabled: !task.enabled });
    } catch (e) {
      console.error('toggle failed', e);
    }
    setToggling(false);
  };

  const handleTrigger = useCallback(async () => {
    setTriggerState('running');
    try {
      await postJSON(`/api/tasks/${task.id}/trigger`, {});
      setTriggerState('done');
      setTimeout(() => setTriggerState('idle'), 2000);
    } catch (e) {
      console.error('trigger failed', e);
      setTriggerState('error');
      setTimeout(() => setTriggerState('idle'), 3000);
    }
  }, [task.id]);

  const triggerLabel = { idle: '\u25B6', running: '\u2022\u2022\u2022', done: '\u2713', error: '!' }[triggerState];
  const triggerColor = {
    idle: 'text-zinc-500 hover:text-orange-400 hover:border-orange-600/50',
    running: 'text-orange-500 border-orange-600/30',
    done: 'text-green-500 border-green-600/30',
    error: 'text-red-500 border-red-600/30',
  }[triggerState];

  return (
    <>
      <tr className={`border-b border-zinc-700/50 transition-colors ${
        checked ? 'bg-orange-900/10' : 'hover:bg-zinc-800/30'
      }`}>
        <td className="py-2 w-8">
          <Checkbox checked={checked} onChange={onCheck} />
        </td>
        <td className="py-2">
          <div className="flex items-center gap-2">
            <button
              onClick={onToggleExpand}
              className="text-zinc-300 hover:text-orange-400 transition-colors cursor-pointer text-left"
            >
              {task.name}
            </button>
            {task.allow_manual && (
              <button
                onClick={handleTrigger}
                disabled={triggerState === 'running'}
                title="Run now"
                className={`w-7 h-5 flex items-center justify-center border border-zinc-700 text-sm transition-colors cursor-pointer ${triggerColor}`}
              >
                {triggerLabel}
              </button>
            )}
          </div>
        </td>
        {showOwner && (
          <td className="py-2 text-zinc-500 text-sm">{task.owner_name || ''}</td>
        )}
        <td className="py-2 text-zinc-500 text-sm" title={task.cron_expr || undefined}>
          <ScheduleCell task={task} />
        </td>
        <td className="py-2">
          <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-400 bg-zinc-800 px-2 py-0.5">
            {task.model_tier || 'auto'}
          </span>
        </td>
        <td className="py-2 text-zinc-500">{task.total_runs}</td>
        <td className="py-2">
          <RunStatusBadge status={task.last_status} />
        </td>
        <td className="py-2">
          <button
            onClick={handleToggle}
            disabled={toggling}
            className={`w-10 h-5 rounded-full relative transition-colors cursor-pointer ${
              task.enabled ? 'bg-orange-600' : 'bg-zinc-700'
            }`}
          >
            <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-zinc-100 transition-transform ${
              task.enabled ? 'left-5' : 'left-0.5'
            }`} />
          </button>
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={showOwner ? 8 : 7} className="bg-zinc-900/80 p-4 border-b border-zinc-700">
            <div className="space-y-3">
              <div>
                <div className="flex items-center justify-between mb-1">
                  <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Details</span>
                  <div className="flex items-center gap-2">
                    {saveStatus === 'saved' && (
                      <span className="text-[12px] uppercase tracking-widest font-medium text-green-500">Saved</span>
                    )}
                    {saveStatus === 'error' && (
                      <span className="text-[12px] uppercase tracking-widest font-medium text-red-500">Error</span>
                    )}
                    {dirty && (
                      <button
                        onClick={handleSave}
                        disabled={saving}
                        className="px-3 py-1 text-[12px] uppercase tracking-widest font-medium border border-orange-600/50 text-orange-500 bg-orange-900/20 hover:bg-orange-900/40 disabled:opacity-50 transition-colors cursor-pointer"
                      >
                        {saving ? 'Saving...' : 'Save'}
                      </button>
                    )}
                  </div>
                </div>
                <textarea
                  value={editPrompt}
                  onChange={(e) => setEditPrompt(e.target.value)}
                  rows={4}
                  className="w-full text-sm text-zinc-300 bg-zinc-950 p-3 border border-zinc-800 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none resize-y"
                />
              </div>
              <div className="flex gap-4">
                <div className="flex-1">
                  <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1 block">Schedule</span>
                  <SchedulePicker value={editCron} onChange={setEditCron} />
                </div>
                <div>
                  <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1 block">Model Tier</span>
                  <div className="flex gap-0">
                    {(['auto', 'cheap', 'standard'] as const).map((t) => (
                      <button
                        key={t}
                        type="button"
                        onClick={() => setEditModel(t)}
                        className={`px-3 py-1.5 text-[12px] uppercase tracking-widest font-medium border transition-colors cursor-pointer ${
                          editModel === t
                            ? 'bg-orange-900/30 border-orange-600 text-orange-500'
                            : 'bg-zinc-900 border-zinc-700 text-zinc-500 hover:text-zinc-300'
                        } ${t === 'auto' ? '' : '-ml-px'}`}
                      >
                        {t}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
              {task.working_dir && (
                <div>
                  <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Working Dir</span>
                  <p className="text-sm text-zinc-400 mt-1">{task.working_dir}</p>
                </div>
              )}
              {task.notify && (
                <div>
                  <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Notify</span>
                  <span className="text-sm text-zinc-400 ml-2">{task.notify}</span>
                </div>
              )}
              <NotifyChatToggle taskId={task.id} initial={task.notify_chat} />
              <BroadcastToggle taskId={task.id} initial={task.broadcast} />
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function NotifyChatToggle({ taskId, initial }: { taskId: number; initial: boolean }) {
  const [enabled, setEnabled] = useState(initial);
  const [toggling, setToggling] = useState(false);

  const handleToggle = async () => {
    setToggling(true);
    try {
      await putJSON(`/api/tasks/${taskId}`, { notify_chat: !enabled });
      setEnabled(!enabled);
    } catch (e) {
      console.error('toggle notify_chat failed', e);
    }
    setToggling(false);
  };

  return (
    <div className="flex items-center gap-2">
      <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Send output to chat</span>
      <button
        onClick={handleToggle}
        disabled={toggling}
        className={`w-10 h-5 rounded-full relative transition-colors cursor-pointer ${
          enabled ? 'bg-orange-600' : 'bg-zinc-700'
        }`}
      >
        <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-zinc-100 transition-transform ${
          enabled ? 'left-5' : 'left-0.5'
        }`} />
      </button>
    </div>
  );
}

function BroadcastToggle({ taskId, initial }: { taskId: number; initial: boolean }) {
  const [enabled, setEnabled] = useState(initial);
  const [toggling, setToggling] = useState(false);

  const handleToggle = async () => {
    setToggling(true);
    try {
      await putJSON(`/api/tasks/${taskId}`, { broadcast: !enabled });
      setEnabled(!enabled);
    } catch (e) {
      console.error('toggle broadcast failed', e);
    }
    setToggling(false);
  };

  return (
    <div className="flex items-center gap-2">
      <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">Broadcast to all users</span>
      <button
        onClick={handleToggle}
        disabled={toggling}
        className={`w-10 h-5 rounded-full relative transition-colors cursor-pointer ${
          enabled ? 'bg-orange-500' : 'bg-zinc-700'
        }`}
      >
        <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-zinc-100 transition-transform ${
          enabled ? 'left-5' : 'left-0.5'
        }`} />
      </button>
    </div>
  );
}

function CreateForm({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState('');
  const [cronExpr, setCronExpr] = useState('');
  const [prompt, setPrompt] = useState('');
  const [modelTier, setModelTier] = useState('auto');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setErr('');
    try {
      await postJSON('/api/tasks', {
        name,
        cron_expr: cronExpr,
        prompt,
        model_tier: modelTier,
        allow_manual: true,
      });
      onDone();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : 'Failed');
    }
    setSubmitting(false);
  };

  const tiers = ['auto', 'cheap', 'standard'];

  return (
    <Panel className="mb-4">
      <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">New Task</h3>
      <form onSubmit={handleSubmit} className="space-y-3">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Task name"
          className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none"
        />
        <div>
          <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-2 block">Schedule</span>
          <SchedulePicker value={cronExpr} onChange={setCronExpr} />
        </div>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="Task prompt"
          rows={3}
          className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none resize-none"
        />
        <div>
          <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-2 block">Model Tier</span>
          <div className="flex gap-0">
            {tiers.map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => setModelTier(t)}
                className={`px-3 py-1.5 text-[12px] uppercase tracking-widest font-medium border transition-colors cursor-pointer ${
                  modelTier === t
                    ? 'bg-orange-900/30 border-orange-600 text-orange-500'
                    : 'bg-zinc-900 border-zinc-700 text-zinc-500 hover:text-zinc-300'
                } ${t === tiers[0] ? '' : '-ml-px'}`}
              >
                {t}
              </button>
            ))}
          </div>
        </div>
        {err && <p className="text-red-500 text-sm">{err}</p>}
        <div className="flex gap-2">
          <StripedButton type="submit" disabled={submitting}>
            {submitting ? 'Creating...' : 'Create'}
          </StripedButton>
          <button
            type="button"
            onClick={onDone}
            className="bg-zinc-800 border border-zinc-700 hover:bg-zinc-700 text-zinc-300 text-sm uppercase tracking-widest font-medium px-4 py-2.5 transition-colors cursor-pointer"
          >
            Cancel
          </button>
        </div>
      </form>
    </Panel>
  );
}

function RunStatusBadge({ status }: { status?: string }) {
  if (!status) return <span className="text-zinc-700 text-sm">n/a</span>;

  const colors: Record<string, string> = {
    completed: 'text-green-500',
    failed: 'text-red-500',
    running: 'text-orange-500',
    cancelled: 'text-zinc-500',
    retrying: 'text-yellow-500',
  };

  return (
    <span className={`text-[12px] uppercase tracking-widest font-medium ${colors[status] || 'text-zinc-500'}`}>
      {status}
    </span>
  );
}

function ScheduleCell({ task }: { task: Task }) {
  if (!task.cron_expr) return <span>manual</span>;
  return <span>{task.cron_description || 'custom schedule'}</span>;
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th className="text-left text-[12px] uppercase tracking-widest font-medium text-zinc-500 pb-2">
      {children}
    </th>
  );
}
