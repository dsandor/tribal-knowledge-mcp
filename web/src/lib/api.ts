const BASE = '/api'

// Central fetch wrapper — injects Authorization header and redirects to /login on 401.
async function apiFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const key = localStorage.getItem('tkm_api_key')
  const r = await fetch(url, {
    ...init,
    headers: {
      ...(init.headers as Record<string, string> ?? {}),
      ...(key ? { 'Authorization': `Bearer ${key}` } : {}),
    },
  })
  if (r.status === 401) {
    localStorage.removeItem('tkm_api_key')
    window.location.replace('/login')
    return new Promise<Response>(() => {}) // never resolves; browser navigates away
  }
  return r
}

// Fetch a file through the authenticated apiFetch and trigger a browser download.
async function downloadBlob(path: string, filename: string): Promise<void> {
  const r = await apiFetch(`${BASE}${path}`)
  if (!r.ok) throw new Error(`download failed: ${r.status}`)
  const blob = await r.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

export interface KnowledgeEntry {
  ID: string
  Type: string
  Title: string
  Content: string
  Description: string
  Domain: string
  Tags: string[] | null | undefined
  Author: string
  Team: string
  CreatedAt: string
  UpdatedAt: string
  Version: number
  Rating: number
  UsageCount: number
}

export interface Cluster {
  ID: string
  Domain: string
  Title: string
  Summary: string
  EntryIDs: string[]
  QualityScore: number
  PipelineRunID: string
  CreatedAt: string
}

export interface DatasetSnapshot {
  ID: string
  Version: number
  ClusterCount: number
  EntryCount: number
  Data: string
  PipelineRunID: string
  CreatedAt: string
}

export interface Agent {
  ID: string
  Domain: string
  Version: number
  Status: 'draft' | 'published'
  SystemPrompt: string
  Instructions: string
  AntiPatterns: string
  SourceRefs: string[]
  ClusterID: string
  CreatedAt: string
  UpdatedAt: string
}

export interface AgentVersion {
  ID: string
  AgentID: string
  Version: number
  SystemPrompt: string
  Instructions: string
  AntiPatterns: string
  Changelog: string
  CreatedAt: string
}

export interface Stats {
  knowledge_count: number
  cluster_count: number
  agent_count: number
  pipeline_status: string
  pipeline_last_run: string | null
}

export interface TrendingEntry extends KnowledgeEntry {
  signal_score: number
  usage_count_7d: number
  usage_count_30d: number
  avg_outcome: number
}

export interface ActivityEvent {
  id: string
  event_type: string
  actor_id: string
  entry_id: string
  metadata: Record<string, string>
  created_at: string
}

export interface PipelineStatus {
  ID: string
  Status: string
  Trigger: string
  EntriesProcessed: number
  ClustersFound: number
  Errors: string[]
  StartedAt: string
  CompletedAt: string | null
}

async function get<T>(path: string): Promise<T> {
  const r = await apiFetch(BASE + path)
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
  return r.json()
}

async function put<T>(path: string, body?: unknown): Promise<T> {
  const r = await apiFetch(BASE + path, {
    method: 'PUT',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
  if (r.status === 204 || r.headers.get('content-length') === '0') return undefined as T
  return r.json()
}

async function del(path: string): Promise<void> {
  const r = await apiFetch(BASE + path, { method: 'DELETE' })
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
}

export const api = {
  stats: (): Promise<Stats> => get('/stats'),

  knowledge: {
    list: (params: { limit?: number; offset?: number; domain?: string; type?: string; search?: string; mode?: 'hybrid' | 'semantic' | 'keyword' } = {}): Promise<KnowledgeEntry[]> => {
      const q = new URLSearchParams()
      if (params.limit != null) q.set('limit', String(params.limit))
      if (params.offset != null) q.set('offset', String(params.offset))
      if (params.domain != null) q.set('domain', params.domain)
      if (params.type != null) q.set('type', params.type)
      if (params.search != null) {
        q.set('search', params.search)
        if (params.search !== '') q.set('mode', params.mode ?? 'hybrid')
      }
      return get(`/knowledge?${q}`)
    },
    get: (id: string): Promise<KnowledgeEntry> => get(`/knowledge/${id}`),
    update: (id: string, fields: Partial<KnowledgeEntry>): Promise<KnowledgeEntry> =>
      put(`/knowledge/${id}`, fields),
    delete: (id: string): Promise<void> => del(`/knowledge/${id}`),
    rate: (id: string, rating: number): Promise<{ ok: boolean }> =>
      put(`/knowledge/${id}/rate`, { rating }),
  },

  clusters: {
    list: (): Promise<Cluster[]> => get('/clusters'),
  },

  datasets: {
    list: (): Promise<DatasetSnapshot[]> => get('/datasets'),
    download: (id: string, format: 'json' | 'csv') =>
      downloadBlob(`/datasets/${id}/export?format=${format}`, `dataset-${id}.${format}`),
  },

  agents: {
    list: (): Promise<Agent[]> => get('/agents'),
    get: (id: string): Promise<{ agent: Agent; versions: AgentVersion[] }> => get(`/agents/${id}`),
    publish: (id: string): Promise<{ ok: boolean }> => put(`/agents/${id}/publish`),
    download: (id: string, format: 'md' | 'txt' | 'json') =>
      downloadBlob(`/agents/${id}/export?format=${format}`, `agent-${id}.${format}`),
    bulkDownload: () => downloadBlob('/agents/bulk-export', 'agents-export.zip'),
    refactor: async (id: string, feedback: string): Promise<{ agent: Agent }> => {
      const r = await apiFetch(`${BASE}/agents/${id}/refactor`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ feedback }),
      })
      if (!r.ok) {
        const err = await r.json().catch(() => ({ error: 'refactor failed' }))
        throw new Error(err.message || err.error || 'refactor failed')
      }
      return r.json()
    },
  },

  pipeline: {
    status: (): Promise<PipelineStatus | { status: string }> => get('/pipeline/status'),
  },
}

// --- Auth ---
export async function login(email: string, password: string) {
  const r = await fetch('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });
  if (!r.ok) throw new Error((await r.json()).error ?? 'login failed');
  return r.json();
}

export async function logout() {
  await apiFetch('/auth/logout', { method: 'POST' });
}

// --- Analytics ---
export async function fetchUsage() {
  const r = await apiFetch('/api/analytics/usage');
  if (!r.ok) throw new Error('fetch usage failed');
  return r.json();
}

export async function fetchGaps() {
  const r = await apiFetch('/api/analytics/gaps');
  if (!r.ok) throw new Error('fetch gaps failed');
  return r.json();
}

export async function fetchContributions() {
  const r = await apiFetch('/api/analytics/contributions');
  if (!r.ok) throw new Error('fetch contributions failed');
  return r.json();
}

// --- Settings ---
export async function fetchSettings() {
  const r = await apiFetch('/api/settings');
  if (!r.ok) throw new Error('fetch settings failed');
  return r.json();
}

export async function putSettings(settings: object) {
  const r = await apiFetch('/api/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  });
  if (!r.ok) throw new Error('put settings failed');
  return r.json();
}

// --- Pending queue ---
export async function fetchPending() {
  const r = await apiFetch('/api/knowledge?status=pending');
  if (!r.ok) throw new Error('fetch pending failed');
  return r.json();
}

export async function approveEntry(id: string) {
  const r = await apiFetch(`/api/knowledge/${id}/approve`, { method: 'PUT' });
  if (!r.ok) throw new Error('approve failed');
  return r.json();
}

export async function rejectEntry(id: string) {
  const r = await apiFetch(`/api/knowledge/${id}/reject`, { method: 'PUT' });
  if (!r.ok) throw new Error('reject failed');
  return r.json();
}

export async function batchApprove(ids: string[]): Promise<{ approved: number; errors: string[] }> {
  const r = await apiFetch('/api/knowledge/batch-approve', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });
  if (!r.ok) throw new Error('batch approve failed');
  return r.json();
}

export async function batchReject(ids: string[]): Promise<{ rejected: number; errors: string[] }> {
  const r = await apiFetch('/api/knowledge/batch-reject', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });
  if (!r.ok) throw new Error('batch reject failed');
  return r.json();
}

// --- Admin: teams ---
export async function fetchTeams() {
  const r = await apiFetch('/api/admin/teams');
  if (!r.ok) throw new Error('fetch teams failed');
  return r.json();
}

export async function updateTeam(id: string, name: string, domainPatterns: string[]) {
  const r = await apiFetch(`/api/admin/teams/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, domain_patterns: domainPatterns }),
  });
  if (!r.ok) throw new Error('update team failed');
  return r.json();
}

export async function createTeam(name: string, domainPatterns: string[]) {
  const r = await apiFetch('/api/admin/teams', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, domain_patterns: domainPatterns }),
  });
  if (!r.ok) throw new Error('create team failed');
  return r.json();
}

export async function setTeamEnabled(id: string, enabled: boolean) {
  const r = await apiFetch(`/api/admin/teams/${id}/enabled`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  if (!r.ok) throw new Error('set team enabled failed');
  return r.json();
}

// --- Admin: auth config ---
export async function fetchAuthConfig() {
  const r = await apiFetch('/api/admin/auth-config');
  if (!r.ok) throw new Error('fetch auth config failed');
  return r.json();
}

export async function putAuthConfig(config: object) {
  const r = await apiFetch('/api/admin/auth-config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  if (!r.ok) throw new Error('put auth config failed');
  return r.json();
}

// --- Pipeline ---
export async function fetchPipelineRuns(limit = 20): Promise<PipelineStatus[]> {
  const r = await apiFetch(`/api/pipeline/runs?limit=${limit}`)
  if (!r.ok) throw new Error('fetch pipeline runs failed')
  return r.json()
}

export async function triggerPipeline() {
  const r = await apiFetch('/api/pipeline/trigger', { method: 'POST' });
  if (!r.ok) throw new Error('trigger failed');
  return r.json();
}

// --- API Keys ---
export async function createAPIKey(name: string, role: string, keyType: string) {
  const r = await apiFetch('/api/api-keys', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, role, key_type: keyType }),
  });
  if (!r.ok) throw new Error('create api key failed');
  return r.json();
}

// --- Knowledge (standalone helper) ---
export async function fetchKnowledge(params?: Record<string, string>) {
  const q = new URLSearchParams(params ?? {});
  const r = await apiFetch(`/api/knowledge?${q}`);
  if (!r.ok) throw new Error('fetch knowledge failed');
  return r.json() as Promise<KnowledgeEntry[]>;
}

export async function searchSimilar(entryId: string, query: string, limit = 3): Promise<KnowledgeEntry[]> {
  const q = new URLSearchParams({ search: query, mode: 'hybrid', limit: String(limit + 1) })
  const r = await apiFetch(`/api/knowledge?${q}`)
  if (!r.ok) throw new Error('search similar failed')
  const entries: KnowledgeEntry[] = await r.json()
  return entries.filter(e => e.ID !== entryId).slice(0, limit)
}

export async function storeKnowledge(entry: {
  title: string;
  content: string;
  type: string;
  domain: string;
  tags: string[];
}) {
  const r = await apiFetch('/api/knowledge', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(entry),
  });
  if (!r.ok) throw new Error('store knowledge failed');
  return r.json();
}

// --- Knowledge Import ---
export interface ImportResult {
  imported: number
  skipped: number
  errors: string[]
}

export async function importKnowledge(entries: Partial<KnowledgeEntry>[]): Promise<ImportResult> {
  const r = await apiFetch('/api/knowledge/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(entries),
  })
  if (!r.ok) throw new Error(`import failed: ${r.status} ${r.statusText}`)
  return r.json()
}

export async function importKnowledgeCSV(file: File): Promise<ImportResult> {
  const form = new FormData()
  form.append('file', file)
  const r = await apiFetch('/api/knowledge/import', {
    method: 'POST',
    body: form,
  })
  if (!r.ok) throw new Error(`import failed: ${r.status} ${r.statusText}`)
  return r.json()
}

// --- Trending & Activity ---
export async function fetchTrending(days = 7, limit = 10): Promise<TrendingEntry[]> {
  const r = await apiFetch(`/api/knowledge/trending?days=${days}&limit=${limit}`);
  if (!r.ok) throw new Error('fetch trending failed');
  return r.json();
}

export async function fetchActivity(limit = 20, offset = 0): Promise<ActivityEvent[]> {
  const r = await apiFetch(`/api/activity?limit=${limit}&offset=${offset}`);
  if (!r.ok) throw new Error('fetch activity failed');
  return r.json();
}
