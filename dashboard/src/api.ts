// Thin typed client over the gg serve JSON API. Field names mirror the Go
// structs (mostly capitalized; SemanticScore is json:"semantic_score").

export type Decision = {
  ID?: string
  Text: string
  Reason?: string
  Evidence?: string
  Pinned?: boolean
  Tags?: string[]
  CreatedAt?: string
  Approach?: string
  Title?: string
  semantic_score?: number
}
export type Task = { ID: string; Title: string; Status: string; Priority?: string; Owner?: string }
export type Bug = { ID: string; Title: string; Status: string; Severity?: string; RootCause?: string }
export type CanonEntry = { area: string; text: string }

export type Overview = {
  project: string
  counts: Record<string, number>
  canon: CanonEntry[]
  recentDecisions: Decision[]
}

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

export type GraphNode = { id: string; label: string; properties: Record<string, any> }
export type GraphEdge = { src: string; dst: string; type: string; properties?: Record<string, any> }
export type GraphData = { nodes: GraphNode[]; edges: GraphEdge[]; error?: string }

export type FileInfo = { name: string; records: number; bytes: number }
export type FileDump = { name: string; records: any[] }
export type Telemetry = { weekly?: any; sessions?: any }

async function get<T>(url: string): Promise<T> {
  const r = await fetch(url)
  return (await r.json()) as T
}

export const api = {
  overview: () => get<Overview>('/api/overview'),
  search: (q: string) => get<SearchResult>('/api/search?q=' + encodeURIComponent(q)),
  decisions: () => get<Decision[]>('/api/decisions'),
  tasks: () => get<Task[]>('/api/tasks'),
  bugs: () => get<Bug[]>('/api/bugs'),
  graph: () => get<GraphData>('/api/graph'),
  files: () => get<FileInfo[]>('/api/files'),
  file: (name: string, tail = 20) => get<FileDump>(`/api/file?name=${encodeURIComponent(name)}&tail=${tail}`),
  telemetry: () => get<Telemetry>('/api/telemetry'),
}
