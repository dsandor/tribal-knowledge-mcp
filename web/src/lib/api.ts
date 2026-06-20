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
  AutoTags: string[] | null | undefined
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

export interface ActorRef {
  id: string
  display: string
}

export interface ActivityEvent {
  id: string
  type: string
  actor: ActorRef
  fragment?: string
  entry_id?: string
  title?: string
  meta?: Record<string, string>
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
    list: (params: { limit?: number; offset?: number; domain?: string; type?: string; search?: string; mode?: 'hybrid' | 'semantic' | 'keyword'; tag?: string } = {}): Promise<KnowledgeEntry[]> => {
      const q = new URLSearchParams()
      if (params.limit != null) q.set('limit', String(params.limit))
      if (params.offset != null) q.set('offset', String(params.offset))
      if (params.domain != null) q.set('domain', params.domain)
      if (params.type != null) q.set('type', params.type)
      if (params.search != null) {
        q.set('search', params.search)
        if (params.search !== '') q.set('mode', params.mode ?? 'hybrid')
      }
      if (params.tag) q.set('tag', params.tag)
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
    rename: async (id: string, newDomain: string): Promise<{
      ok: boolean
      old_domain: string
      new_domain: string
      updated: { entries: number; clusters: number; agents: number }
    }> => {
      const r = await apiFetch(`${BASE}/agents/${id}/rename`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ new_domain: newDomain }),
      })
      if (!r.ok) {
        const err = await r.json().catch(() => ({ error: 'rename failed' }))
        throw new Error(err.message || err.error || 'rename failed')
      }
      return r.json()
    },
  },

  pipeline: {
    status: (): Promise<PipelineStatus | { status: string }> => get('/pipeline/status'),
  },

  admin: {
    downloadBackup: () => {
      const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')
      return downloadBlob('/admin/backup', `tribal-backup-${stamp}.tar.gz`)
    },
    restore: async (file: File, force: boolean): Promise<{ tables_restored: Record<string, number>; embeddings_restored: number }> => {
      const fd = new FormData()
      fd.append('archive', file)
      const r = await apiFetch(`${BASE}/admin/restore?force=${force}`, { method: 'POST', body: fd })
      const body = await r.json().catch(() => ({}))
      if (!r.ok) throw new Error(body?.message || `restore failed: ${r.status}`)
      return body
    },
  },
}

// --- Auth ---
export interface AuthInfo {
  provider: string;       // "local" | "oidc"
  oidc_enabled: boolean;
}

// Public: which auth provider the server is configured for. No auth required.
export async function fetchAuthInfo(): Promise<AuthInfo> {
  const r = await fetch('/auth/info');
  if (!r.ok) throw new Error('fetch auth info failed');
  return r.json();
}

// Verify the current session cookie or Bearer key. Resolves true when authed.
export async function checkAuth(): Promise<boolean> {
  const key = localStorage.getItem('tkm_api_key');
  const r = await fetch('/api/me', {
    headers: key ? { Authorization: `Bearer ${key}` } : {},
  });
  return r.ok;
}

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

// --- AI / Effective settings ---
export interface AIFieldValue {
  effective: string;
  saved: string;
  env: string;
  source: 'saved' | 'env' | 'none';
}

export interface AITouchpoint {
  provider: string;
  model: string;
}

export interface AISettings {
  anthropic_api_key: AIFieldValue;
  anthropic_model: AIFieldValue;
  agent_model: AIFieldValue;
  ollama_url: AIFieldValue;
  ollama_model: AIFieldValue;
  llm_provider: AIFieldValue;
  ollama_llm_model: AIFieldValue;
  ai_touchpoints?: Record<string, AITouchpoint>;
}

export interface ModelOption {
  id: string;
  label: string;
}

export interface ModelOptions {
  anthropic: ModelOption[];
  ollama: ModelOption[];
  anthropic_source: 'api' | 'fallback';
  ollama_source: 'api' | 'unavailable';
}

export async function fetchModelOptions(): Promise<ModelOptions> {
  const r = await apiFetch('/api/settings/models');
  if (!r.ok) throw new Error(`fetch model options failed: ${r.status}`);
  return r.json();
}

export async function importEnvSettings(fields: string[]): Promise<{ ai: AISettings }> {
  const r = await apiFetch('/api/settings/import-env', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ fields }),
  });
  if (!r.ok) throw new Error(`import env settings failed: ${r.status}`);
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

export interface TeamDataCounts { users: number; api_keys: number; entries: number; clusters: number; agents: number; rules: number }
export interface TeamDeleteResult { ok?: boolean; needsMigration?: boolean; counts?: TeamDataCounts; summary?: Record<string, number> }

export async function deleteTeam(id: string, migrateTo?: string): Promise<TeamDeleteResult> {
  const q = migrateTo ? `?migrate_to=${encodeURIComponent(migrateTo)}` : ''
  const r = await apiFetch(`/api/admin/teams/${id}${q}`, { method: 'DELETE' })
  if (r.status === 409) {
    const body = await r.json()
    return { needsMigration: true, counts: body.counts }
  }
  if (!r.ok) {
    const err = await r.json().catch(() => ({}))
    throw new Error((err as { message?: string }).message ?? 'delete team failed')
  }
  return { ok: true, summary: await r.json().catch(() => undefined) }
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
export interface APIKey {
  id: string;
  team_id: string;
  user_id: string;
  key_type: string;   // "team" | "user"
  name: string;
  role: string;
  raw_key?: string;   // present for keys created after raw-key retention was enabled
  created_at: string;
}

export async function listAPIKeys(): Promise<APIKey[]> {
  const r = await apiFetch('/api/api-keys');
  if (!r.ok) throw new Error('list api keys failed');
  return r.json();
}

export async function createAPIKey(
  name: string,
  role: string,
  keyType: string,
  userId?: string,
): Promise<{ id: string; raw_key: string; name: string; role: string; key_type: string; created_at: string }> {
  const r = await apiFetch('/api/api-keys', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, role, key_type: keyType, ...(userId ? { user_id: userId } : {}) }),
  });
  if (!r.ok) throw new Error('create api key failed');
  return r.json();
}

export async function revokeAPIKey(id: string): Promise<void> {
  const r = await apiFetch(`/api/api-keys/${id}`, { method: 'DELETE' });
  if (!r.ok) throw new Error('revoke api key failed');
}

export interface TeamUser {
  id: string;
  team_id: string;
  email: string;
  name: string;
  role: string;
}

export interface AdminUser {
  id: string;
  team_id: string;
  email: string;
  name: string;
  role: string;
  manually_assigned: boolean;
}

export async function listAllUsers(): Promise<AdminUser[]> {
  const r = await apiFetch('/api/admin/users');
  if (!r.ok) throw new Error('list all users failed');
  return r.json();
}

export async function assignUserTeam(id: string, teamId: string, role: string): Promise<void> {
  const r = await apiFetch(`/api/admin/users/${id}/team`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ team_id: teamId, role }),
  });
  if (!r.ok) {
    const err = await r.json().catch(() => ({}));
    throw new Error((err as { message?: string }).message ?? 'assign team failed');
  }
}

export async function listUsers(): Promise<TeamUser[]> {
  const r = await apiFetch('/api/users');
  if (!r.ok) throw new Error('list users failed');
  return r.json();
}

export async function addUser(email: string, role: string): Promise<{ id: string }> {
  const r = await apiFetch('/api/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, role }),
  });
  if (!r.ok) {
    const err = await r.json().catch(() => ({}));
    throw new Error((err as { message?: string }).message ?? 'add user failed');
  }
  return r.json();
}

export async function setUserRole(id: string, role: string): Promise<void> {
  const r = await apiFetch(`/api/users/${id}/role`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ role }),
  });
  if (!r.ok) {
    const err = await r.json().catch(() => ({}));
    throw new Error((err as { message?: string }).message ?? 'set role failed');
  }
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
