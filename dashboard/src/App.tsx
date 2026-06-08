import { useEffect, useState } from 'react'
import ReactFlow, { Background, Controls, type Node, type Edge } from 'reactflow'
import 'reactflow/dist/style.css'
import dagre from '@dagrejs/dagre'
import { api, type Decision, type Task, type Bug, type FileInfo, type Overview, type SearchResult, type Telemetry, type GraphData } from './api'

const TABS = ['Overview', 'Live Search', 'Decisions', 'Work', 'Bugs', 'Graph', 'Files', 'Context'] as const
type Tab = (typeof TABS)[number]

const date = (s?: string) => (s ? s.slice(0, 10) : '')
const tok = (n?: number) => (n == null ? '0' : Math.abs(n) >= 1000 ? (n / 1000).toFixed(1) + 'K' : String(n))

function Card({ n, label, sub }: { n: React.ReactNode; label: string; sub?: string }) {
  return (
    <div className="bg-panel border border-border rounded-xl p-4">
      <div className="text-3xl font-bold">{n}</div>
      <div className="text-dim text-xs uppercase tracking-wide mt-1">{label}</div>
      {sub && <div className="text-dim text-[11px] mt-1">{sub}</div>}
    </div>
  )
}

function Tags({ tags }: { tags?: string[] }) {
  if (!tags?.length) return null
  return (
    <>
      {tags.map((t) => (
        <span key={t} className="text-[10.5px] text-purple border border-border rounded-full px-2 py-px">#{t}</span>
      ))}
    </>
  )
}

function DecisionRow({ d }: { d: Decision }) {
  const unverified = !d.Evidence && d.Text && d.Reason
  return (
    <div className="bg-panel border border-border rounded-lg p-3 mb-2">
      <div className="flex gap-2.5 flex-wrap items-center text-dim text-[11px] mb-1">
        <span>{date(d.CreatedAt)}</span>
        {d.Pinned && <span className="text-warn text-[10.5px] border border-border rounded-full px-2">📌 pinned</span>}
        <Tags tags={d.Tags} />
        {d.ID && <span>{d.ID}</span>}
      </div>
      <div className="text-[13.5px]">{d.Text}</div>
      {d.Reason && <div className="text-dim text-[12.5px] mt-1">— {d.Reason}</div>}
      {d.Evidence && <div className="text-good text-[12.5px] mt-1">✓ {d.Evidence}</div>}
      {unverified && <div className="text-warn text-[12.5px] mt-1">[unverified]</div>}
    </div>
  )
}

function ScoredRow({ d, max }: { d: Decision; max: number }) {
  const sc = d.semantic_score || 0
  const pct = max > 0 ? Math.round((sc / max) * 100) : 0
  return (
    <div className="bg-panel border border-border rounded-lg p-3 mb-2">
      <div className="flex gap-2.5 flex-wrap items-center text-dim text-[11px] mb-1">
        <span>{date(d.CreatedAt)}</span>
        <span className="text-accent">score {sc.toFixed(3)}</span>
        <Tags tags={d.Tags} />
      </div>
      <div className="text-[13.5px]">{d.Text || d.Approach || d.Title}</div>
      {d.Reason && <div className="text-dim text-[12.5px] mt-1">— {d.Reason}</div>}
      <div className="h-[5px] bg-panel2 rounded mt-1.5 overflow-hidden">
        <div className="h-full" style={{ width: pct + '%', background: 'linear-gradient(90deg,#58a6ff,#3fb950)' }} />
      </div>
    </div>
  )
}

const H2 = ({ children }: { children: React.ReactNode }) => (
  <h2 className="text-xs uppercase tracking-wider text-dim font-semibold mt-7 mb-3">{children}</h2>
)
const Empty = ({ children }: { children: React.ReactNode }) => <div className="text-dim text-center py-8">{children}</div>

function OverviewTab() {
  const [o, setO] = useState<Overview | null>(null)
  useEffect(() => { api.overview().then(setO) }, [])
  if (!o) return <Empty>loading…</Empty>
  const c = o.counts || {}
  return (
    <>
      <div className="grid gap-3 mb-2" style={{ gridTemplateColumns: 'repeat(auto-fit,minmax(150px,1fr))' }}>
        <Card n={c.decisions ?? 0} label="active decisions" sub="memory, never deleted" />
        <Card n={`${c.tasksDone ?? 0}/${c.tasks ?? 0}`} label="work done" sub="tasks completed" />
        <Card n={c.bugsFixed ?? 0} label="bugs fixed" sub={`${c.bugsOpen ?? 0} open`} />
        <Card n={c.rejections ?? 0} label="rejections" sub="what not to do" />
      </div>
      <H2>Canon — distilled institutional memory (auto-derived)</H2>
      {(o.canon || []).map((e) => (
        <div key={e.area} className="mb-3">
          <h3 className="text-accent text-xs uppercase tracking-wide mb-2">{e.area}</h3>
          {e.text.split('\n').map((l, i) => (
            <div key={i} className="text-[13px] py-0.5 border-b border-panel2">{l}</div>
          ))}
        </div>
      ))}
      <H2>Recent decisions</H2>
      {(o.recentDecisions || []).slice(0, 15).map((d, i) => <DecisionRow key={i} d={d} />)}
    </>
  )
}

function SearchTab() {
  const [q, setQ] = useState('')
  const [r, setR] = useState<SearchResult | null>(null)
  const [loading, setLoading] = useState(false)
  const run = async () => {
    if (!q.trim()) return
    setLoading(true)
    setR(await api.search(q))
    setLoading(false)
  }
  const all = [...(r?.decisions || []), ...(r?.rejections || []), ...(r?.bugs || [])]
  const max = Math.max(0.0001, ...all.map((d) => d.semantic_score || 0))
  return (
    <>
      <div className="text-dim text-xs mb-3.5">
        Type a question and watch how gg answers it: your text is embedded into a 768-dim vector (Ollama), then Qdrant
        returns the nearest records by cosine similarity — the same path an agent's <code>gg search</code> takes.
      </div>
      <div className="flex gap-2.5 mb-2">
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && run()}
          placeholder="e.g. why is JSONL the source of truth?"
          className="flex-1 bg-panel border border-border rounded-lg text-fg px-3.5 py-3 outline-none focus:border-accent"
        />
        <button onClick={run} className="bg-accent text-[#04121f] font-semibold rounded-lg px-5">Search</button>
      </div>
      {loading && <Empty>searching…</Empty>}
      {r?.error && <Empty>{r.error}</Empty>}
      {r && !r.error && (
        <>
          <div className="flex items-center gap-2 flex-wrap my-3.5 text-dim text-xs">
            <span className="bg-panel border border-border rounded-lg px-3 py-2">“{r.query}”</span>
            <span className="text-accent">→</span>
            <span className="bg-panel border border-border rounded-lg px-3 py-2">embed → <b className="text-fg">{r.vectorDim}-dim</b> in <b className="text-fg">{r.embedMs}ms</b></span>
            <span className="text-accent">→</span>
            <span className="bg-panel border border-border rounded-lg px-3 py-2">Qdrant cosine <b className="text-fg">{r.searchMs}ms</b></span>
            <span className="text-accent">→</span>
            <span className="bg-panel border border-border rounded-lg px-3 py-2"><b className="text-fg">{all.length}</b> hits</span>
          </div>
          {!!r.decisions?.length && <H2>Decisions</H2>}
          {r.decisions?.map((d, i) => <ScoredRow key={i} d={d} max={max} />)}
          {!!r.rejections?.length && <H2>Rejected approaches</H2>}
          {r.rejections?.map((d, i) => <ScoredRow key={i} d={d} max={max} />)}
          {!!r.bugs?.length && <H2>Bugs</H2>}
          {r.bugs?.map((d, i) => <ScoredRow key={i} d={d} max={max} />)}
          {all.length === 0 && <Empty>no matches</Empty>}
        </>
      )}
    </>
  )
}

function DecisionsTab() {
  const [d, setD] = useState<Decision[]>([])
  useEffect(() => { api.decisions().then((x) => setD(x || [])) }, [])
  return (
    <>
      <H2>{d.length} active decisions (noise-filtered)</H2>
      {d.map((x, i) => <DecisionRow key={i} d={x} />)}
    </>
  )
}

const COLS: [string, string][] = [
  ['pending', 'Pending'], ['in_progress', 'In Progress'], ['ready_for_live', 'Ready for Live'], ['blocked', 'Blocked'], ['done', 'Done'],
]
function WorkTab() {
  const [t, setT] = useState<Task[]>([])
  useEffect(() => { api.tasks().then((x) => setT(x || [])) }, [])
  const by: Record<string, Task[]> = {}
  COLS.forEach(([k]) => (by[k] = []))
  t.forEach((x) => (by[x.Status] = by[x.Status] || []).push(x))
  return (
    <>
      <div className="text-dim text-xs mb-3.5">{t.length} tasks across the lifecycle (same data as <code>gg task list</code>).</div>
      <div className="flex gap-3 overflow-x-auto pb-2.5">
        {COLS.map(([k, label]) => (
          <div key={k} className="flex-1 min-w-[200px] bg-panel border border-border rounded-xl p-2.5">
            <div className="flex justify-between text-xs uppercase tracking-wide text-dim mb-2.5">
              <span>{label}</span>
              <span className="bg-panel2 rounded-full px-2 text-fg">{(by[k] || []).length}</span>
            </div>
            {(by[k] || []).map((x) => (
              <div key={x.ID} className="bg-panel2 border border-border rounded-lg px-2.5 py-2 mb-1.5">
                <div className="flex gap-2 flex-wrap items-center text-dim text-[11px] mb-1">
                  <span>{x.ID}</span>
                  {x.Priority && <span className="border border-border rounded-full px-1.5">{x.Priority}</span>}
                  {x.Owner && <span>@{x.Owner}</span>}
                </div>
                <div className="text-[13px]">{x.Title}</div>
              </div>
            ))}
            {!(by[k] || []).length && <div className="text-border text-center py-3.5 text-xs">—</div>}
          </div>
        ))}
      </div>
    </>
  )
}

function BugsTab() {
  const [b, setB] = useState<Bug[]>([])
  useEffect(() => { api.bugs().then((x) => setB(x || [])) }, [])
  const sev = (s?: string) => (s === 'high' || s === 'critical' ? 'text-bad' : s === 'medium' ? 'text-warn' : 'text-dim')
  return (
    <>
      <H2>{b.length} bugs</H2>
      {b.map((x) => (
        <div key={x.ID} className="bg-panel border border-border rounded-lg p-3 mb-2">
          <div className="flex gap-2.5 flex-wrap items-center text-[11px] mb-1">
            <span className="text-dim">{x.ID}</span>
            <span className={x.Status === 'fixed' ? 'text-good' : 'text-bad'}>{x.Status}</span>
            <span className={sev(x.Severity)}>{x.Severity}</span>
          </div>
          <div className="text-[13.5px]">{x.Title}</div>
          {x.RootCause && <div className="text-dim text-[12.5px] mt-1">root cause: {x.RootCause}</div>}
        </div>
      ))}
    </>
  )
}

function FilesTab() {
  const [f, setF] = useState<FileInfo[]>([])
  const [sel, setSel] = useState<string | null>(null)
  const [recs, setRecs] = useState<any[]>([])
  useEffect(() => { api.files().then((x) => setF(x || [])) }, [])
  const open = async (name: string) => {
    setSel(name)
    const d = await api.file(name, 20)
    setRecs((d.records || []).slice().reverse())
  }
  const kb = (b: number) => (b >= 1048576 ? (b / 1048576).toFixed(1) + 'MB' : (b / 1024).toFixed(0) + 'KB')
  return (
    <>
      <div className="text-dim text-xs mb-3.5">
        The brain is plain JSONL on disk — the source of truth (Qdrant/Memgraph are derived indexes rebuilt from these).
        Click a file to see its most recent records.
      </div>
      {f.map((x) => (
        <div key={x.name} onClick={() => open(x.name)} className="bg-panel border border-border rounded-lg p-3 mb-2 cursor-pointer hover:border-accent">
          <div className="text-dim text-[11px] mb-1">.gg/{x.name === 'canon' ? '' : 'brain/'}{x.name}.jsonl</div>
          <div className="text-[13.5px]">{x.records} records · {kb(x.bytes)}</div>
        </div>
      ))}
      {sel && (
        <>
          <H2>{sel} — last {recs.length} records (raw, newest first)</H2>
          {recs.map((x, i) => (
            <pre key={i} className="bg-panel border border-border rounded-lg p-3.5 overflow-auto text-xs text-dim mb-2">
              {JSON.stringify(x.payload || x, null, 2)}
            </pre>
          ))}
        </>
      )}
    </>
  )
}

const NODE_COLOR: Record<string, string> = { Decision: '#58a6ff', Task: '#3fb950', Bug: '#f85149', Rejection: '#bc8cff' }
const EDGE_COLOR: Record<string, string> = { DECIDES: '#58a6ff', DEPENDS_ON: '#3fb950', REJECTS: '#bc8cff', BLOCKS: '#f85149', IMPLEMENTS: '#d29922' }

function GraphTab() {
  const [data, setData] = useState<GraphData | null>(null)
  useEffect(() => { api.graph().then(setData) }, [])
  if (!data) return <Empty>loading…</Empty>
  if (data.error) return <Empty>{data.error}</Empty>
  if (!data.nodes?.length) return <Empty>no brain relationships yet — link tasks/decisions and they'll appear here</Empty>

  const W = 220
  const H = 46
  const ids = new Set(data.nodes.map((n) => n.id))
  const rawEdges = data.edges.filter((e) => ids.has(e.src) && ids.has(e.dst))

  // dagre lays the relationships out as a left-to-right DAG (decision → task →
  // dependency) instead of naive columns, so the web is actually readable.
  const dg = new dagre.graphlib.Graph()
  dg.setGraph({ rankdir: 'LR', nodesep: 24, ranksep: 90 })
  dg.setDefaultEdgeLabel(() => ({}))
  data.nodes.forEach((n) => dg.setNode(n.id, { width: W, height: H }))
  rawEdges.forEach((e) => dg.setEdge(e.src, e.dst))
  dagre.layout(dg)

  const nodes: Node[] = data.nodes.map((n) => {
    const p = dg.node(n.id)
    const title = n.properties?.title || n.properties?.text || n.id
    const color = NODE_COLOR[n.label] || '#8b949e'
    return {
      id: n.id,
      position: { x: (p?.x ?? 0) - W / 2, y: (p?.y ?? 0) - H / 2 },
      data: { label: `${n.label}: ${String(title).slice(0, 38)}` },
      style: { background: '#161b22', color: '#e6edf3', border: `1px solid ${color}`, borderRadius: 8, fontSize: 11, width: W, padding: 6 },
    }
  })
  const edges: Edge[] = rawEdges.map((e, i) => ({
    id: `${e.src}-${e.dst}-${i}`,
    source: e.src,
    target: e.dst,
    label: e.type,
    animated: e.type === 'DEPENDS_ON',
    style: { stroke: EDGE_COLOR[e.type] || '#8b949e' },
    labelStyle: { fill: '#8b949e', fontSize: 10 },
    labelBgStyle: { fill: '#0d1117' },
  }))
  return (
    <>
      <div className="text-dim text-xs mb-3">
        {nodes.length} connected records · {edges.length} relationships — decision→task (DECIDES), task→task (DEPENDS_ON),
        decision→rejected (REJECTS). The 37k-symbol code graph is intentionally excluded to keep this legible.
      </div>
      <div style={{ height: '72vh' }} className="bg-panel border border-border rounded-xl overflow-hidden">
        <ReactFlow nodes={nodes} edges={edges} fitView minZoom={0.1} proOptions={{ hideAttribution: true }}>
          <Background color="#2a3340" gap={22} />
          <Controls />
        </ReactFlow>
      </div>
    </>
  )
}

function Bar({ label, val, max, fmt, color }: { label: string; val: number; max: number; fmt?: (n: number) => string; color?: string }) {
  return (
    <div className="flex items-center gap-2.5 my-1.5">
      <div className="w-[170px] text-dim text-xs text-right truncate">{label}</div>
      <div className="flex-1 bg-panel2 rounded h-4 overflow-hidden">
        <div className="h-full" style={{ width: Math.max(2, Math.round((val / max) * 100)) + '%', background: color || '#58a6ff' }} />
      </div>
      <div className="w-16 text-xs">{fmt ? fmt(val) : val}</div>
    </div>
  )
}

function ContextTab() {
  const [t, setT] = useState<Telemetry | null>(null)
  useEffect(() => { api.telemetry().then(setT) }, [])
  if (!t) return <Empty>loading…</Empty>
  const w = t.weekly || {}
  const s = t.sessions || {}
  const agentPct = w.total ? Math.round((w.agent_calls / w.total) * 100) : 0
  const refetch = w.compact_calls ? Math.round((w.hydration_calls / w.compact_calls) * 100) : 0
  const verbs = Object.entries((w.verb_counts || {}) as Record<string, number>).sort((a, b) => b[1] - a[1]).slice(0, 12)
  const vmax = Math.max(1, ...verbs.map((v) => v[1]))
  const saved = Object.entries((w.compact_by_verb_bytes_saved || {}) as Record<string, number>).sort((a, b) => b[1] - a[1]).slice(0, 8)
  const smax = Math.max(1, ...saved.map((v) => v[1]))
  const fix = (x: any) => (typeof x === 'number' ? x.toFixed(0) : x || 0)
  return (
    <>
      <div className="text-dim text-xs mb-3.5">All recorded locally — nothing leaves your machine. gg's context economy and exactly what it's doing.</div>
      <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fit,minmax(150px,1fr))' }}>
        <Card n={tok(w.net_tokens_saved)} label="net tokens saved" sub="compact − hydration" />
        <Card n={tok(w.compact_tokens_saved)} label="compact saved" sub="display truncation" />
        <Card n={w.total || 0} label="gg calls (7d)" sub={agentPct + '% agent-initiated'} />
        <Card n={refetch + '%'} label="hydration re-fetch" sub={refetch > 50 ? '⚠ drop-list aggressive' : 'healthy'} />
      </div>
      <H2>What gg is actually doing (top commands, 7d)</H2>
      {verbs.map(([k, v]) => <Bar key={k} label={k} val={v} max={vmax} />)}
      {!verbs.length && <Empty>no activity yet</Empty>}
      <H2>Where compaction saves the most</H2>
      {saved.map(([k, v]) => <Bar key={k} label={k} val={v} max={smax} fmt={(b) => tok(Math.round(b / 4)) + ' tok'} color="linear-gradient(90deg,#58a6ff,#3fb950)" />)}
      <H2>Session context pressure (7d)</H2>
      <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fit,minmax(150px,1fr))' }}>
        <Card n={s.ActiveSessions || 0} label="active sessions" />
        <Card n={fix(s.P50CumulativeKB) + 'KB'} label="P50 / session" sub="median compact output" />
        <Card n={fix(s.P95CumulativeKB) + 'KB'} label="P95 / session" />
        <Card n={s.OverThresholdCount || 0} label="over 100KB" sub="high-pressure sessions" />
      </div>
      <H2>How gg feeds context — 3 tiers (the full project is never loaded)</H2>
      <div className="text-[13px] space-y-1">
        <div>🟢 <b>session-start</b> — orientation: compact canon + status — <b>~2.1K tok</b> (once/session, optional)</div>
        <div>🔵 <b>gg context --for-task TASK-X</b> — task + deps + related decisions — <b>~300 tok</b></div>
        <div>🟣 <b>gg search "q"</b> — semantic top-K over the whole archive — <b>~150 tok</b></div>
        <div className="text-dim">The full ledger stays on disk/Qdrant — pulled in slices on demand, never dumped into the context window.</div>
      </div>
    </>
  )
}

export default function App() {
  const [tab, setTab] = useState<Tab>('Overview')
  const [project, setProject] = useState('')
  useEffect(() => { api.overview().then((o) => setProject(o.project || '')) }, [])
  return (
    <div className="min-h-full">
      <header className="flex items-center gap-3.5 px-6 py-3.5 border-b border-border bg-panel">
        <h1 className="text-base font-semibold m-0"><span className="text-accent font-bold">gg</span> · project brain</h1>
        <span className="text-dim text-xs">{project}</span>
        <span className="ml-auto text-[11px] text-good border border-border rounded-full px-2.5 py-0.5">● localhost · read-only</span>
      </header>
      <nav className="flex gap-1 px-6 py-2.5 border-b border-border bg-panel overflow-x-auto">
        {TABS.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={
              'px-3.5 py-2 rounded-md text-[13px] whitespace-nowrap ' +
              (tab === t ? 'text-fg bg-panel2 shadow-[inset_0_-2px_0_#58a6ff]' : 'text-dim hover:text-fg hover:bg-panel2')
            }
          >
            {t}
          </button>
        ))}
      </nav>
      <main className="p-6 max-w-[1100px] mx-auto">
        {tab === 'Overview' && <OverviewTab />}
        {tab === 'Live Search' && <SearchTab />}
        {tab === 'Decisions' && <DecisionsTab />}
        {tab === 'Work' && <WorkTab />}
        {tab === 'Bugs' && <BugsTab />}
        {tab === 'Graph' && <GraphTab />}
        {tab === 'Files' && <FilesTab />}
        {tab === 'Context' && <ContextTab />}
      </main>
    </div>
  )
}
