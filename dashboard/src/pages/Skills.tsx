import { useState, useMemo, useCallback } from 'react';
import { Search, Download, Star, Package, User, Tag, ClipboardPaste } from 'lucide-react';
import { fetchJSON, postJSON, deleteJSON, usePolling, fetchSkillContent, updateSkill, importSkill } from '../api';
import type { MemoryNode, SkillMeta, SkillSearchResult, SkillDetail } from '../api';
import Panel from '../components/Panel';
import PageHeader from '../components/PageHeader';
import StripedButton from '../components/StripedButton';

export default function Skills() {
  const [query, setQuery] = useState('');
  const [searchResults, setSearchResults] = useState<SkillMeta[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [installingSlug, setInstallingSlug] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [hiddenIds, setHiddenIds] = useState<Set<string>>(new Set());
  const [expandedSlug, setExpandedSlug] = useState<string | null>(null);
  const [skillDetail, setSkillDetail] = useState<SkillDetail | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);

  // Edit mode state
  const [editing, setEditing] = useState(false);
  const [editTitle, setEditTitle] = useState('');
  const [editSummary, setEditSummary] = useState('');
  const [editContent, setEditContent] = useState('');
  const [loadingContent, setLoadingContent] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editError, setEditError] = useState('');
  const [confirmUninstallId, setConfirmUninstallId] = useState<string | null>(null);

  // Import mode state
  const [importing, setImporting] = useState(false);
  const [importContent, setImportContent] = useState('');
  const [importError, setImportError] = useState('');
  const [importSaving, setImportSaving] = useState(false);

  const { data: rawSkills } = usePolling<MemoryNode[]>(
    () => fetchJSON('/api/skills'),
    5000,
  );

  const skills = useMemo(() => {
    if (!rawSkills) return [];
    return rawSkills.filter((s) => !hiddenIds.has(s.id));
  }, [rawSkills, hiddenIds]);

  const installedSlugs = useMemo(() => {
    if (!rawSkills) return new Map<string, string>();
    const map = new Map<string, string>();
    for (const s of rawSkills) {
      if (s.skill_path) {
        const parts = s.skill_path.split('/');
        const idx = parts.indexOf('SKILL.md');
        if (idx > 0) map.set(parts[idx - 1], s.id);
      }
    }
    return map;
  }, [rawSkills]);

  const selected = useMemo(
    () => skills.find((s) => s.id === selectedId) ?? null,
    [skills, selectedId],
  );

  const handleSearch = useCallback(async () => {
    const q = query.trim();
    if (!q) return;
    setSearching(true);
    setSelectedId(null);
    try {
      const result = await fetchJSON<SkillSearchResult>(
        '/api/skills/search?q=' + encodeURIComponent(q),
      );
      setSearchResults(result.results);
    } catch (e) {
      console.error('search failed', e);
      setSearchResults([]);
    }
    setSearching(false);
  }, [query]);

  const handleInstall = useCallback(async (meta: SkillMeta) => {
    setInstallingSlug(meta.slug);
    try {
      await postJSON('/api/skills/install', meta);
      setSearchResults(null);
      setQuery('');
    } catch (e) {
      console.error('install failed', e);
    }
    setInstallingSlug(null);
  }, []);

  const confirmUninstall = useCallback((id: string) => {
    setConfirmUninstallId(null);
    setHiddenIds((prev) => new Set([...prev, id]));
    setSelectedId(null);
    setEditing(false);
    deleteJSON('/api/skills/' + id).catch(() => {
      setHiddenIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    });
  }, []);

  const handleExpandResult = useCallback(async (slug: string) => {
    if (expandedSlug === slug) {
      setExpandedSlug(null);
      setSkillDetail(null);
      return;
    }
    setExpandedSlug(slug);
    setSkillDetail(null);
    setLoadingDetail(true);
    try {
      const detail = await fetchJSON<SkillDetail>(
        `/api/skills/detail/${encodeURIComponent(slug)}`,
      );
      setSkillDetail(detail);
    } catch (e) {
      console.error('detail fetch failed', e);
    }
    setLoadingDetail(false);
  }, [expandedSlug]);

  const clearSearch = () => {
    setSearchResults(null);
    setQuery('');
    setExpandedSlug(null);
    setSkillDetail(null);
  };

  const showingSearch = searchResults !== null;

  const enterImportMode = useCallback(() => {
    setImporting(true);
    setImportContent('');
    setImportError('');
    setSelectedId(null);
    setEditing(false);
    setSearchResults(null);
    setQuery('');
    setExpandedSlug(null);
    setSkillDetail(null);
  }, []);

  const cancelImport = useCallback(() => {
    setImporting(false);
    setImportContent('');
    setImportError('');
  }, []);

  const handleImport = useCallback(async () => {
    const content = importContent.trim();
    if (!content) return;
    setImportSaving(true);
    setImportError('');
    try {
      await importSkill(content);
      setImporting(false);
      setImportContent('');
    } catch (e: any) {
      const msg = e?.message || 'Import failed';
      setImportError(msg);
    }
    setImportSaving(false);
  }, [importContent]);

  const enterEditMode = useCallback(async (node: MemoryNode) => {
    setEditTitle(node.title || '');
    setEditSummary(node.summary || '');
    setEditContent('');
    setEditError('');
    setEditing(true);
    setLoadingContent(true);
    try {
      const { content } = await fetchSkillContent(node.id);
      setEditContent(content);
    } catch (e: any) {
      const msg = e?.message || 'Failed to load skill content';
      setEditError(msg);
    }
    setLoadingContent(false);
  }, []);

  const cancelEdit = useCallback(() => {
    setEditing(false);
    setEditError('');
  }, []);

  const saveEdit = useCallback(async () => {
    if (!selectedId) return;
    setSaving(true);
    setEditError('');
    try {
      const body: { title?: string; summary?: string; content?: string } = {};
      if (editTitle !== (selected?.title || '')) body.title = editTitle;
      if (editSummary !== (selected?.summary || '')) body.summary = editSummary;
      if (editContent) body.content = editContent;
      await updateSkill(selectedId, body);
      setEditing(false);
    } catch (e: any) {
      const msg = e?.message || 'Save failed';
      setEditError(msg);
    }
    setSaving(false);
  }, [selectedId, selected, editTitle, editSummary, editContent]);

  return (
    <div>
      <PageHeader title="Skills" subtitle="Search, install, and manage ClawHub skills" />

      {/* Search bar */}
      <div className="flex gap-2 mb-6">
        <div className="flex-1 relative">
          <Search
            size={14}
            className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-500 pointer-events-none"
          />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
            placeholder="Search ClawHub for skills..."
            className="w-full bg-zinc-900 border border-zinc-700 text-base text-zinc-300 pl-9 pr-3 py-2 placeholder:text-zinc-600 focus:outline-none focus:border-orange-600"
          />
        </div>
        <StripedButton onClick={handleSearch} disabled={searching || !query.trim()}>
          {searching ? 'Searching...' : 'Search'}
        </StripedButton>
        <button
          type="button"
          onClick={enterImportMode}
          className="flex items-center gap-1.5 text-sm text-zinc-500 hover:text-zinc-300 uppercase tracking-widest font-medium px-3 transition-colors cursor-pointer"
          title="Import skill from text"
        >
          <ClipboardPaste size={14} />
          Import
        </button>
        {showingSearch && (
          <button
            type="button"
            onClick={clearSearch}
            className="text-sm text-zinc-500 hover:text-zinc-300 uppercase tracking-widest font-medium px-3 transition-colors cursor-pointer"
          >
            Clear
          </button>
        )}
      </div>

      <div className="grid grid-cols-2 gap-4">
        {/* Left panel: installed skills */}
        <Panel>
          <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
            Installed Skills
          </h3>
          {skills.length === 0 ? (
            <p className="text-base text-zinc-600">
              No skills installed. Search ClawHub to discover skills.
            </p>
          ) : (
            <div className="space-y-1">
              {skills.map((node) => (
                <button
                  key={node.id}
                  onClick={() => {
                    if (showingSearch) clearSearch();
                    if (importing) cancelImport();
                    setEditing(false);
                    setSelectedId(selectedId === node.id ? null : node.id);
                  }}
                  className={`w-full text-left p-3 border transition-colors cursor-pointer ${
                    selectedId === node.id
                      ? 'border-orange-600/50 bg-orange-900/10'
                      : 'border-zinc-700 hover:border-zinc-600 hover:bg-zinc-800/30'
                  }`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-base text-zinc-300 truncate">
                      {node.title || node.id}
                    </span>
                    <div className="flex gap-2 shrink-0">
                      {node.version && (
                        <span className="text-[12px] text-zinc-600">
                          v{node.version}
                        </span>
                      )}
                      {node.origin && (
                        <span className="text-[12px] uppercase tracking-widest font-medium text-orange-600/60">
                          {node.origin === 'learned' ? 'Self' : node.origin}
                        </span>
                      )}
                    </div>
                  </div>
                  {node.summary && (
                    <p className="text-sm text-zinc-500 mt-1 truncate">{node.summary}</p>
                  )}
                </button>
              ))}
            </div>
          )}
        </Panel>

        {/* Right panel: search results or detail */}
        <Panel>
          {importing ? (
            <>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
                Import Skill
              </h3>
              <p className="text-sm text-zinc-600 mb-3">
                Paste a SKILL.md file below. It must include YAML frontmatter with at least a <code className="text-zinc-500">name</code> field.
              </p>
              <textarea
                value={importContent}
                onChange={(e) => setImportContent(e.target.value)}
                placeholder={"---\nname: my-skill\ndescription: What this skill does.\n---\n\n# My Skill\n\nInstructions here..."}
                className="w-full bg-zinc-900 border border-zinc-700 text-sm text-zinc-300 px-3 py-2 font-mono min-h-[300px] resize-y focus:outline-none focus:border-orange-600 placeholder:text-zinc-700"
              />
              {importError && (
                <p className="text-sm text-red-400 mt-2">{importError}</p>
              )}
              <div className="flex items-center gap-3 pt-3 border-t border-zinc-700 mt-3">
                <StripedButton onClick={handleImport} disabled={importSaving || !importContent.trim()}>
                  {importSaving ? 'Importing...' : 'Import'}
                </StripedButton>
                <button
                  type="button"
                  onClick={cancelImport}
                  className="text-sm text-zinc-500 hover:text-zinc-300 uppercase tracking-widest font-medium px-3 transition-colors cursor-pointer"
                >
                  Cancel
                </button>
              </div>
            </>
          ) : showingSearch ? (
            <>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
                Search Results
              </h3>
              {searchResults.length === 0 ? (
                <p className="text-base text-zinc-600">No skills found for "{query}".</p>
              ) : (
                <div className="space-y-2">
                  {searchResults.map((meta) => {
                    const installedNodeId = installedSlugs.get(meta.slug);
                    const installed = !!installedNodeId;
                    const installing = installingSlug === meta.slug;
                    const expanded = expandedSlug === meta.slug;
                    const detail = expanded ? skillDetail : null;
                    return (
                      <div
                        key={meta.slug}
                        className={`border transition-colors ${
                          expanded
                            ? 'border-orange-600/50 bg-orange-900/5'
                            : 'border-zinc-700 hover:border-zinc-600'
                        }`}
                      >
                        <div
                          onClick={() => handleExpandResult(meta.slug)}
                          className="w-full text-left p-3 cursor-pointer"
                        >
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <div className="flex items-center gap-2">
                                <span className="text-base font-medium text-zinc-300">
                                  {meta.displayName}
                                </span>
                                {meta.version && (
                                  <span className="text-[12px] text-zinc-600">
                                    v{meta.version}
                                  </span>
                                )}
                              </div>
                              {meta.summary && (
                                <p className="text-sm text-zinc-500 mt-1">{meta.summary}</p>
                              )}
                              <p className="text-[12px] text-zinc-600 mt-1">
                                {meta.slug}
                              </p>
                            </div>
                            <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
                              {installed ? (
                                confirmUninstallId === installedNodeId ? (
                                  <div className="flex items-center gap-2">
                                    <span className="text-[11px] uppercase tracking-widest font-medium text-red-500">Remove?</span>
                                    <button
                                      type="button"
                                      onClick={() => confirmUninstall(installedNodeId!)}
                                      className="text-[11px] uppercase tracking-widest font-medium text-red-500 hover:text-red-400 cursor-pointer"
                                    >
                                      Yes
                                    </button>
                                    <button
                                      type="button"
                                      onClick={() => setConfirmUninstallId(null)}
                                      className="text-[11px] uppercase tracking-widest font-medium text-zinc-500 hover:text-zinc-300 cursor-pointer"
                                    >
                                      Cancel
                                    </button>
                                  </div>
                                ) : (
                                  <button
                                    type="button"
                                    onClick={() => setConfirmUninstallId(installedNodeId!)}
                                    className="text-[12px] uppercase tracking-widest font-medium text-red-500 px-2 py-1 border border-red-500/30 bg-red-900/10 hover:bg-red-900/20 transition-colors cursor-pointer"
                                  >
                                    Uninstall
                                  </button>
                                )
                              ) : (
                                <StripedButton
                                  onClick={() => handleInstall(meta)}
                                  disabled={installing}
                                >
                                  {installing ? 'Installing...' : 'Install'}
                                </StripedButton>
                              )}
                            </div>
                          </div>
                        </div>

                        {expanded && (
                          <div className="px-3 pb-3 border-t border-zinc-700/50">
                            {loadingDetail ? (
                              <p className="text-sm text-zinc-600 pt-3">Loading detail...</p>
                            ) : detail ? (
                              <div className="pt-3 space-y-3">
                                <div className="flex flex-wrap gap-x-5 gap-y-1 text-sm text-zinc-500">
                                  <span className="flex items-center gap-1">
                                    <Download size={11} />
                                    {detail.skill.stats.downloads.toLocaleString()} downloads
                                  </span>
                                  {detail.skill.stats.stars > 0 && (
                                    <span className="flex items-center gap-1">
                                      <Star size={11} />
                                      {detail.skill.stats.stars}
                                    </span>
                                  )}
                                  <span className="flex items-center gap-1">
                                    <Package size={11} />
                                    v{detail.latestVersion.version}
                                  </span>
                                  <span className="flex items-center gap-1">
                                    <User size={11} />
                                    {detail.owner.handle}
                                  </span>
                                </div>

                                {detail.skill.tags && Object.keys(detail.skill.tags).length > 0 && (
                                  <div className="flex items-center gap-1 flex-wrap">
                                    <Tag size={11} className="text-zinc-600 shrink-0" />
                                    {Object.keys(detail.skill.tags).filter((t) => t !== 'latest').map((tag) => (
                                      <span
                                        key={tag}
                                        className="text-[12px] text-zinc-500 border border-zinc-800 px-1.5 py-0.5"
                                      >
                                        {tag}
                                      </span>
                                    ))}
                                  </div>
                                )}

                                {detail.latestVersion.changelog && (
                                  <div>
                                    <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-600 block mb-1">
                                      Changelog
                                    </span>
                                    <p className="text-sm text-zinc-500">
                                      {detail.latestVersion.changelog}
                                    </p>
                                  </div>
                                )}
                              </div>
                            ) : (
                              <p className="text-sm text-zinc-600 pt-3">
                                Could not load detail.
                              </p>
                            )}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </>
          ) : selected ? (
            <>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
                Detail
              </h3>
              {editing ? (
                <div className="space-y-4">
                  <div>
                    <Label>Title</Label>
                    <input
                      type="text"
                      value={editTitle}
                      onChange={(e) => setEditTitle(e.target.value)}
                      className="w-full bg-zinc-900 border border-zinc-700 text-base text-zinc-300 px-3 py-2 focus:outline-none focus:border-orange-600"
                    />
                  </div>
                  <div>
                    <Label>Summary</Label>
                    <input
                      type="text"
                      value={editSummary}
                      onChange={(e) => setEditSummary(e.target.value)}
                      className="w-full bg-zinc-900 border border-zinc-700 text-base text-zinc-300 px-3 py-2 focus:outline-none focus:border-orange-600"
                    />
                  </div>
                  <div>
                    <Label>Content</Label>
                    {loadingContent ? (
                      <p className="text-sm text-zinc-600">Loading content...</p>
                    ) : (
                      <textarea
                        value={editContent}
                        onChange={(e) => setEditContent(e.target.value)}
                        className="w-full bg-zinc-900 border border-zinc-700 text-sm text-zinc-300 px-3 py-2 font-mono min-h-[300px] resize-y focus:outline-none focus:border-orange-600"
                      />
                    )}
                  </div>
                  {editError && (
                    <p className="text-sm text-red-400 mt-2">{editError}</p>
                  )}
                  <div className="flex items-center gap-3 pt-2 border-t border-zinc-700">
                    <StripedButton onClick={saveEdit} disabled={saving}>
                      {saving ? 'Saving...' : 'Save'}
                    </StripedButton>
                    <button
                      type="button"
                      onClick={cancelEdit}
                      className="text-sm text-zinc-500 hover:text-zinc-300 uppercase tracking-widest font-medium px-3 transition-colors cursor-pointer"
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              ) : (
                <div className="space-y-4">
                  <div>
                    <Label>Title</Label>
                    <p className="text-base text-zinc-300">{selected.title || selected.id}</p>
                  </div>

                  <div>
                    <Label>ID</Label>
                    <p className="text-sm text-zinc-500">{selected.id}</p>
                  </div>

                  {selected.summary && (
                    <div>
                      <Label>Summary</Label>
                      <p className="text-base text-zinc-400">{selected.summary}</p>
                    </div>
                  )}

                  <div className="grid grid-cols-2 gap-3">
                    {selected.version && (
                      <div>
                        <Label>Version</Label>
                        <span className="text-sm text-zinc-400">
                          {selected.version}
                        </span>
                      </div>
                    )}
                    {selected.origin && (
                      <div>
                        <Label>Origin</Label>
                        <span className="text-sm text-zinc-500">{selected.origin === 'learned' ? 'Self' : selected.origin}</span>
                      </div>
                    )}
                  </div>

                  {selected.source_url && (
                    <div>
                      <Label>Source</Label>
                      <a
                        href={selected.source_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-sm text-orange-500 hover:text-orange-400 underline break-all"
                      >
                        {selected.source_url}
                      </a>
                    </div>
                  )}

                  <div className="grid grid-cols-2 gap-3 text-sm text-zinc-600">
                    <div>
                      <Label>Created</Label>
                      {formatDate(selected.created_at)}
                    </div>
                    <div>
                      <Label>Updated</Label>
                      {formatDate(selected.updated_at)}
                    </div>
                  </div>

                  <div className="pt-2 border-t border-zinc-700 flex items-center gap-3">
                    <StripedButton onClick={() => enterEditMode(selected)}>
                      Edit
                    </StripedButton>
                    {confirmUninstallId === selected.id ? (
                      <div className="flex items-center gap-3">
                        <span className="text-[11px] uppercase tracking-widest font-medium text-red-500">Remove?</span>
                        <button
                          type="button"
                          onClick={() => confirmUninstall(selected.id)}
                          className="text-[11px] uppercase tracking-widest font-medium text-red-500 hover:text-red-400 cursor-pointer"
                        >
                          Yes
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirmUninstallId(null)}
                          className="text-[11px] uppercase tracking-widest font-medium text-zinc-500 hover:text-zinc-300 cursor-pointer"
                        >
                          Cancel
                        </button>
                      </div>
                    ) : (
                      <StripedButton onClick={() => setConfirmUninstallId(selected.id)}>
                        Uninstall
                      </StripedButton>
                    )}
                  </div>
                </div>
              )}
            </>
          ) : (
            <>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
                Detail
              </h3>
              <p className="text-base text-zinc-600">
                Select an installed skill or search ClawHub.
              </p>
            </>
          )}
        </Panel>
      </div>
    </div>
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
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    return iso;
  }
}
