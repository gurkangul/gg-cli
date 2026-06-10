// Thin typed client over the gg serve JSON API. Field names mirror the Go
// structs (mostly capitalized; SemanticScore is json:"semantic_score").

export type Decision = {
  ID?: string
  Text: string
  Reason?: string
  Evidence?: string
  Pinned?: boolean
  Tags?: string[]
  TaskID?: string
  CreatedAt?: string
  Approach?: string
  Title?: string
  semantic_score?: number
}
export type Task = {
  ID: string
  Title: string
  Status: string
  Priority?: string
  Owner?: string
  Detail?: string
  DependsOn?: string[]
  Blocks?: string[]
  ReviewStatus?: string
  CreatedAt?: string
}
export type Bug = { ID: string; Title: string; Status: string; Severity?: string; RootCause?: string }
export type CanonEntry = { area: string; text: string }

export type Overview = {
  project: string
  counts: Record<string, number>
  canon: CanonEntry[]
  recentDecisions: Decision[]
  writable?: boolean
}

export type WriteResult = { ok?: boolean; output?: string; error?: string }

export type SearchResult = {
  query: string
  vectorDim: number
  embedMs: number
  searchMs: number
  decisions?: Decision[]
  rejections?: Decision[]
  bugs?: Decision[]
  error?: string
}

export type Message = { ID: string; FromRole: string; ToRole: string; Content: string; Audience: string; Read: boolean; TaskID?: string; CreatedAt: string }

export type GraphNode = { id: string; label: string; properties: Record<string, any> }
export type GraphEdge = { src: string; dst: string; type: string; properties?: Record<string, any> }
export type GraphData = { nodes: GraphNode[]; edges: GraphEdge[]; error?: string }

export type FileInfo = { name: string; records: number; bytes: number }
export type FileDump = { name: string; records: any[] }
export type Telemetry = { weekly?: any; sessions?: any }
export type ProjectItem = { id: string; name: string; root: string; default?: boolean }

// The selected project is threaded into every request as ?project=<id> so one
// path-independent server can serve every registered project's (isolated) brain.
let currentProject = ''
export function setProject(id: string) {
  currentProject = id
}
export function projectQuery(url: string): string {
  if (!currentProject) return url
  return url + (url.includes('?') ? '&' : '?') + 'project=' + encodeURIComponent(currentProject)
}

async function get<T>(url: string): Promise<T> {
  const r = await fetch(projectQuery(url))
  return (await r.json()) as T
}

async function post<T>(url: string, body: unknown): Promise<T> {
  const r = await fetch(projectQuery(url), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  return (await r.json()) as T
}

export const api = {
  projects: () => get<ProjectItem[]>('/api/projects'),
  overview: () => get<Overview>('/api/overview'),
  search: (q: string) => get<SearchResult>('/api/search?q=' + encodeURIComponent(q)),
  decisions: () => get<Decision[]>('/api/decisions'),
  tasks: () => get<Task[]>('/api/tasks'),
  bugs: () => get<Bug[]>('/api/bugs'),
  messages: () => get<Message[]>('/api/messages'),
  graph: () => get<GraphData>('/api/graph'),
  files: () => get<FileInfo[]>('/api/files'),
  file: (name: string, tail = 20) => get<FileDump>(`/api/file?name=${encodeURIComponent(name)}&tail=${tail}`),
  telemetry: () => get<Telemetry>('/api/telemetry'),
  recordDecision: (text: string, reason: string) => post<WriteResult>('/api/write/decision', { Text: text, Reason: reason }),
  createTask: (title: string, detail: string) => post<WriteResult>('/api/write/task', { Title: title, Detail: detail }),
}
