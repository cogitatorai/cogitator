import { useState, useCallback, useEffect } from 'react';
import { fetchJSON, postJSON, deleteJSON, pinMemoryNode, usePolling, fetchUsers } from '../api';
import type { MemoryNode, MemoryStats, MemoryGraph as MemoryGraphData } from '../api';
import Panel from '../components/Panel';
import PageHeader from '../components/PageHeader';
import StripedButton from '../components/StripedButton';
import MemoryGraph from '../components/MemoryGraph';

const NODE_TYPES = ['fact', 'preference', 'pattern', 'skill', 'episode', 'task_knowledge'];
const VISIBLE_TYPES = ['fact', 'preference', 'pattern', 'skill'];
const FILTER_OPTIONS = [...VISIBLE_TYPES, 'pinned'];

type ViewMode = 'graph' | 'list';

export default function Memory() {
  const [view, setView] = useState<ViewMode>('graph');
  const [nodeType, setNodeType] = useState('fact');
  const [selectedNode, setSelectedNode] = useState<MemoryNode | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [userNames, setUserNames] = useState<Record<string, string>>({});

  useEffect(() => {
    fetchUsers()
      .then(({ users }) => {
        const map: Record<string, string> = {};
        for (const u of users) map[u.id] = u.name;
        setUserNames(map);
      })
      .catch(() => {});
  }, []);

  const { data: stats } = usePolling<MemoryStats>(
    () => fetchJSON('/api/memory/stats'),
    10000,
  );

  const { data: graphData } = usePolling<MemoryGraphData>(
    () => fetchJSON('/api/memory/graph'),
    30000,
  );

  const isPinnedFilter = nodeType === 'pinned';
  const { data: rawNodes, error: nodesError, refresh: refreshNodes } = usePolling<MemoryNode[]>(
    () => isPinnedFilter
      ? fetchJSON('/api/memory/nodes?limit=200')
      : fetchJSON(`/api/memory/nodes?type=${nodeType}&limit=100`),
    5000,
    nodeType,
  );
  const nodes = isPinnedFilter && rawNodes ? rawNodes.filter((n) => n.pinned) : rawNodes;

  const handlePin = useCallback(async (node: MemoryNode, e?: React.MouseEvent) => {
    if (e) { e.stopPropagation(); }
    try {
      const updated = await pinMemoryNode(node.id, !node.pinned);
      if (selectedNode?.id === node.id) setSelectedNode(updated);
      refreshNodes();
    } catch (err) {
      console.error('pin toggle failed', err);
    }
  }, [selectedNode, refreshNodes]);

  const handleSelectNode = useCallback(async (node: MemoryNode) => {
    if (selectedNode?.id === node.id) {
      setSelectedNode(null);
      return;
    }
    setLoadingDetail(true);
    try {
      const detail = await fetchJSON<MemoryNode>(`/api/memory/nodes/${node.id}`);
      setSelectedNode(detail);
    } catch {
      setSelectedNode(node);
    }
    setLoadingDetail(false);
  }, [selectedNode]);

  const handleGraphSelectNode = useCallback(async (id: string) => {
    if (selectedNode?.id === id) {
      setSelectedNode(null);
      return;
    }
    setLoadingDetail(true);
    try {
      const detail = await fetchJSON<MemoryNode>(`/api/memory/nodes/${id}`);
      setSelectedNode(detail);
    } catch {
      setSelectedNode(null);
    }
    setLoadingDetail(false);
  }, [selectedNode]);

  const handleDelete = useCallback(async (id: string) => {
    try {
      await deleteJSON(`/api/memory/nodes/${id}`);
      if (selectedNode?.id === id) setSelectedNode(null);
      refreshNodes();
    } catch (e) {
      console.error('delete failed', e);
    }
  }, [selectedNode, refreshNodes]);

  return (
    <div className="flex flex-col h-full">
      <PageHeader title="Memory" subtitle="Knowledge graph nodes" />

      {/* Stats */}
      {stats && (
        <div className="grid grid-cols-3 gap-4 mb-6">
          <StatCard label="Total Nodes" value={stats.total_nodes} />
          <StatCard label="Total Relations" value={stats.total_edges ?? 0} />
          <EnrichmentCard count={stats.pending_enrichment ?? 0} enriching={!!stats.enriching} />
        </div>
      )}

      {/* View toggle */}
      <div className="flex gap-0 mb-4">
        {(['graph', 'list'] as ViewMode[]).map((v) => (
          <button
            key={v}
            onClick={() => { setView(v); setSelectedNode(null); }}
            className={`px-3 py-1.5 text-[12px] uppercase tracking-widest font-medium border transition-colors cursor-pointer ${
              view === v
                ? 'bg-orange-900/30 border-orange-600 text-orange-500'
                : 'bg-zinc-900 border-zinc-700 text-zinc-500 hover:text-zinc-300'
            } ${v === 'graph' ? '' : '-ml-px'}`}
          >
            {v}
          </button>
        ))}
      </div>

      {view === 'graph' ? (
        <div className="grid grid-cols-3 gap-4 flex-1 min-h-0">
          {/* Graph panel */}
          <Panel className="col-span-2 flex flex-col !p-0 overflow-hidden">
            <div className="flex-1 min-h-[400px]">
              {graphData && (
                <MemoryGraph
                  nodes={graphData.nodes}
                  edges={graphData.edges}
                  selectedNodeId={selectedNode?.id ?? null}
                  onSelectNode={handleGraphSelectNode}
                />
              )}
            </div>
            {/* Legend */}
            <div className="flex flex-wrap gap-4 px-4 py-2 border-t border-zinc-700">
              {NODE_TYPES.map(t => (
                <div key={t} className="flex items-center gap-1.5">
                  <div className="w-3 h-3 rounded-full border border-zinc-700" style={{
                    backgroundColor: (nodeColorMap[t] || '#52525b') + '20',
                    boxShadow: `inset 0 0 0 1px ${nodeColorMap[t] || '#52525b'}40`,
                  }}>
                    <div className="w-1.5 h-1.5 rounded-full mx-auto mt-[3px]" style={{
                      backgroundColor: nodeColorMap[t] || '#52525b',
                      opacity: 0.7,
                    }} />
                  </div>
                  <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">
                    {t.replace(/_/g, ' ')}
                  </span>
                </div>
              ))}
            </div>
          </Panel>

          {/* Detail sidebar */}
          <Panel className="overflow-y-auto">
            <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
              Detail
            </h3>
            <NodeDetail
              node={selectedNode}
              loading={loadingDetail}
              onDelete={handleDelete}
              userNames={userNames}
              onPin={handlePin}
            />
          </Panel>
        </div>
      ) : (
        <>
          {/* Type filter */}
          <div className="flex gap-0 mb-4 flex-wrap">
            {FILTER_OPTIONS.map((t, i) => (
              <button
                key={t}
                onClick={() => { setNodeType(t); setSelectedNode(null); }}
                className={`px-3 py-1.5 text-[12px] uppercase tracking-widest font-medium border transition-colors cursor-pointer ${
                  nodeType === t
                    ? 'bg-orange-900/30 border-orange-600 text-orange-500'
                    : 'bg-zinc-900 border-zinc-700 text-zinc-500 hover:text-zinc-300'
                } ${i === 0 ? '' : '-ml-px'}`}
              >
                {t.replace(/_/g, ' ')}
              </button>
            ))}
          </div>

          {nodesError && (
            <Panel className="border-red-500/30 mb-4">
              <p className="text-red-500 text-base">{nodesError}</p>
            </Panel>
          )}

          <div className="grid grid-cols-2 gap-4">
            {/* Node list */}
            <Panel>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
                {nodeType.replace(/_/g, ' ')} nodes
              </h3>
              {(!nodes || nodes.length === 0) ? (
                <p className="text-base text-zinc-600">No {nodeType} nodes found.</p>
              ) : (
                <div className="space-y-1">
                  {nodes.map((node) => (
                    <div
                      key={node.id}
                      onClick={() => handleSelectNode(node)}
                      className={`relative w-full text-left p-3 border transition-colors cursor-pointer ${
                        selectedNode?.id === node.id
                          ? 'border-orange-600/50 bg-orange-900/10'
                          : 'border-zinc-700 hover:border-zinc-600 hover:bg-zinc-800/30'
                      }`}
                    >
                      <button
                        onClick={(e) => handlePin(node, e)}
                        title={node.pinned ? 'Unpin node' : 'Pin node'}
                        className="absolute top-2 right-2 w-6 h-6 flex items-center justify-center text-[14px] transition-colors cursor-pointer hover:scale-110"
                        style={{ color: node.pinned ? '#f97316' : '#4a4a52' }}
                      >
                        <PinIcon filled={node.pinned} />
                      </button>
                      <div className="flex items-center justify-between pr-7">
                        <span className="text-base text-zinc-300 truncate">{node.title}</span>
                        <span className="text-[12px] text-zinc-600 ml-2 shrink-0">
                          {(node.confidence * 100).toFixed(0)}%
                        </span>
                      </div>
                      {node.summary && (
                        <p className="text-sm text-zinc-500 mt-1 truncate">{node.summary}</p>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </Panel>

            {/* Node detail */}
            <Panel>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
                Detail
              </h3>
              <NodeDetail
                node={selectedNode}
                loading={loadingDetail}
                onDelete={handleDelete}
                userNames={userNames}
                onPin={handlePin}
              />
            </Panel>
          </div>
        </>
      )}
    </div>
  );
}

const nodeColorMap: Record<string, string> = {
  fact: '#60a5fa',
  preference: '#a78bfa',
  pattern: '#2dd4bf',
  skill: '#f97316',
  episode: '#4ade80',
  task_knowledge: '#fbbf24',
};

function NodeDetail({
  node,
  loading,
  onDelete,
  onPin,
  userNames,
}: {
  node: MemoryNode | null;
  loading: boolean;
  onDelete: (id: string) => void;
  onPin: (node: MemoryNode) => void;
  userNames: Record<string, string>;
}) {
  if (loading) {
    return <p className="text-base text-zinc-600 animate-pulse">Loading...</p>;
  }
  if (!node) {
    return <p className="text-base text-zinc-600">Select a node to view details.</p>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between">
        <div className="flex-1 min-w-0">
          <Label>Title</Label>
          <p className="text-base text-zinc-300">{node.title}</p>
        </div>
        <button
          onClick={() => onPin(node)}
          title={node.pinned ? 'Unpin node' : 'Pin node'}
          className="ml-2 mt-1 w-7 h-7 flex items-center justify-center transition-colors cursor-pointer hover:scale-110"
          style={{ color: node.pinned ? '#f97316' : '#4a4a52' }}
        >
          <PinIcon filled={node.pinned} />
        </button>
      </div>

      <div>
        <Label>ID</Label>
        <p className="text-sm text-zinc-500">{node.id}</p>
      </div>

      {(node.subject_id || node.user_id) && (
        <div className="grid grid-cols-2 gap-3">
          {node.subject_id && (
            <div>
              <Label>About</Label>
              <p className="text-sm text-zinc-400">{userNames[node.subject_id] || node.subject_id}</p>
            </div>
          )}
          {node.user_id && (
            <div>
              <Label>Owner</Label>
              <p className="text-sm text-zinc-400">{userNames[node.user_id] || node.user_id}</p>
            </div>
          )}
        </div>
      )}

      {node.summary && (
        <div>
          <Label>Summary</Label>
          <p className="text-base text-zinc-400">{node.summary}</p>
        </div>
      )}

      <div className="grid grid-cols-2 gap-3">
        <div>
          <Label>Confidence</Label>
          <div className="flex items-center gap-2">
            <div className="flex-1 h-1.5 bg-zinc-800">
              <div
                className="h-full bg-orange-600 transition-all"
                style={{ width: `${node.confidence * 100}%` }}
              />
            </div>
            <span className="text-sm text-zinc-400">
              {(node.confidence * 100).toFixed(0)}%
            </span>
          </div>
        </div>
        <div>
          <Label>Enrichment</Label>
          <span className={`text-sm font-medium uppercase tracking-widest ${
            node.enrichment_status === 'done'
              ? 'text-green-500'
              : node.enrichment_status === 'pending'
              ? 'text-orange-500'
              : 'text-zinc-500'
          }`}>
            {node.enrichment_status || 'none'}
          </span>
        </div>
      </div>

      {node.tags && node.tags.length > 0 && (
        <div>
          <Label>Tags</Label>
          <div className="flex flex-wrap gap-1">
            {node.tags.map((tag) => (
              <span key={tag} className="text-[12px] uppercase tracking-widest font-medium text-zinc-400 bg-zinc-800 px-2 py-0.5">
                {tag}
              </span>
            ))}
          </div>
        </div>
      )}

      {node.origin && (
        <div>
          <Label>Origin</Label>
          <p className="text-sm text-zinc-500">{node.origin === 'learned' ? 'Self' : node.origin}</p>
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 text-sm text-zinc-600">
        <div>
          <Label>Created</Label>
          {formatDate(node.created_at)}
        </div>
        <div>
          <Label>Updated</Label>
          {formatDate(node.updated_at)}
        </div>
      </div>

      {/* Delete action */}
      <div className="pt-2 border-t border-zinc-700">
        <StripedButton onClick={() => onDelete(node.id)}>
          Delete
        </StripedButton>
      </div>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <Panel className="hud-panel-orange">
      <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500 block mb-2">{label}</span>
      <span className="text-3xl font-semibold text-orange-500">{value}</span>
    </Panel>
  );
}

function EnrichmentCard({ count, enriching }: { count: number; enriching: boolean }) {
  const handleTrigger = useCallback(async () => {
    try {
      await postJSON('/api/memory/enrich', {});
    } catch (e) {
      console.error('enrich trigger failed', e);
    }
  }, []);

  return (
    <Panel className="hud-panel-orange">
      <div className="flex items-center justify-between">
        <div>
          <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500 block mb-2">
            Pending Enrichment
          </span>
          <span className="text-3xl font-semibold text-orange-500">{count}</span>
        </div>
        {count > 0 && (
          enriching ? (
            <span className="px-2.5 py-1 text-[11px] uppercase tracking-widest font-medium text-orange-500/70">
              Processing...
            </span>
          ) : (
            <button
              onClick={handleTrigger}
              className="px-2.5 py-1 text-[11px] uppercase tracking-widest font-medium border border-orange-600/50 bg-orange-900/20 text-orange-500 hover:bg-orange-900/40 hover:border-orange-500 hover:text-orange-400 transition-colors cursor-pointer"
            >
              Enrich
            </button>
          )
        )}
      </div>
    </Panel>
  );
}

function PinIcon({ filled }: { filled: boolean }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path
        d="M10.5 1.5L14.5 5.5L11 9L11.5 13.5L9 11L5.5 14.5L5 10.5L2 7L6 6.5L10.5 1.5Z"
        fill={filled ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function Label({ children }: { children: React.ReactNode }) {
  return (
    <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1">
      {children}
    </span>
  );
}

function formatDate(iso: string): string {
  if (!iso) return 'n/a';
  try {
    return new Date(iso).toLocaleString([], {
      month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit',
    });
  } catch {
    return iso;
  }
}
