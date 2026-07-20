package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	graphstore "github.com/gurkangul/gg-cli/internal/graph"
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Work with the local code graph",
}

var graphExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a self-contained offline graph visualization",
	Args:  cobra.NoArgs,
	RunE:  runGraphExport,
}

var graphExportFormat string
var graphExportSymbolCap int
var graphExportView string

type graphExportPayload struct {
	GeneratedAt string                 `json:"generated_at"`
	Nodes       []graphstore.BrainNode `json:"nodes"`
	Edges       []graphstore.BrainEdge `json:"edges"`
	Symbols     []graphstore.BrainNode `json:"symbols,omitempty"`
	SymbolCap   int                    `json:"symbol_cap"`
	SymbolView  bool                   `json:"symbol_view"`
	MemoryNodes int                    `json:"memory_nodes"`
}

func init() {
	graphExportCmd.Flags().StringVar(&graphExportFormat, "format", "html", "export format: html")
	graphExportCmd.Flags().IntVar(&graphExportSymbolCap, "symbol-cap", 500, "include symbol view only when symbol count is at or below this cap")
	graphExportCmd.Flags().StringVar(&graphExportView, "view", graphViewCode, "what to render: code (files/symbols), memory (decisions/tasks/bugs), or all")
	graphCmd.AddCommand(graphExportCmd)
	rootCmd.AddCommand(graphCmd)
}

func runGraphExport(cmd *cobra.Command, _ []string) error {
	if graphExportFormat != "html" {
		return fmt.Errorf("--format must be html")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return err
	}
	if !validGraphExportView(graphExportView) {
		return fmt.Errorf("--view must be one of code, memory, all")
	}
	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	var nodes graphExportNodeSets
	var edges []graphstore.BrainEdge

	// The code half needs the Memgraph-backed code graph; the memory half is
	// derived from JSONL. Skip the graph client entirely for --view memory so a
	// project with no code index can still render its decision web.
	if graphExportView != graphViewMemory {
		gc, gcErr := graphstore.New(cfg.DataDir, cfg.ProjectID)
		if gcErr != nil {
			return fmt.Errorf("graph client init: %w", gcErr)
		}
		defer func() { _ = gc.Close(ctx) }()
		nodes, edges, gcErr = exportGraphData(ctx, gc, graphExportSymbolCap)
		if gcErr != nil {
			return gcErr
		}
	}

	memoryNodes := 0
	if graphExportView != graphViewCode {
		mn, me, memErr := memoryGraphExport()
		if memErr != nil {
			return memErr
		}
		nodes.files = append(nodes.files, mn...)
		edges = append(edges, me...)
		memoryNodes = len(mn)
	}

	payload := graphExportPayload{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Nodes:       nodes.files,
		Edges:       edges,
		Symbols:     nodes.symbols,
		SymbolCap:   graphExportSymbolCap,
		SymbolView:  len(nodes.symbols) > 0,
		MemoryNodes: memoryNodes,
	}
	html, err := renderGraphExportHTML(payload)
	if err != nil {
		return err
	}
	outDir := filepath.Join(runtimeDir, "graph")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return fmt.Errorf("create graph export dir: %w", err)
	}
	outPath := filepath.Join(outDir, "graph-"+time.Now().UTC().Format("20060102-150405")+".html")
	if err := os.WriteFile(outPath, []byte(html), 0o600); err != nil {
		return fmt.Errorf("write graph export: %w", err)
	}
	return printJSON(map[string]any{"output": outPath, "format": "html", "view": graphExportView, "nodes": len(payload.Nodes), "edges": len(payload.Edges), "memory_nodes": payload.MemoryNodes, "symbol_view": payload.SymbolView}, func() {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Graph export written: %s\n", outPath)
	})
}

type graphExportNodeSets struct {
	files   []graphstore.BrainNode
	symbols []graphstore.BrainNode
}

func exportGraphData(ctx context.Context, gc interface {
	ExportNodes(context.Context) ([]graphstore.BrainNode, error)
	ExportEdges(context.Context) ([]graphstore.BrainEdge, error)
}, symbolCap int) (graphExportNodeSets, []graphstore.BrainEdge, error) {
	allNodes, err := gc.ExportNodes(ctx)
	if err != nil {
		return graphExportNodeSets{}, nil, fmt.Errorf("export graph nodes: %w", err)
	}
	allEdges, err := gc.ExportEdges(ctx)
	if err != nil {
		return graphExportNodeSets{}, nil, fmt.Errorf("export graph edges: %w", err)
	}
	var files, symbols []graphstore.BrainNode
	for _, n := range allNodes {
		switch n.Label {
		case graphstore.LabelFile:
			files = append(files, n)
		case graphstore.LabelSymbol:
			symbols = append(symbols, n)
		}
	}
	symbolIDs := map[string]bool{}
	if symbolCap >= 0 && len(symbols) <= symbolCap {
		for _, n := range symbols {
			symbolIDs[n.ID] = true
		}
	} else {
		symbols = nil
	}
	fileIDs := map[string]bool{}
	for _, n := range files {
		fileIDs[n.ID] = true
	}
	var edges []graphstore.BrainEdge
	for _, e := range allEdges {
		if fileIDs[e.Src] && fileIDs[e.Dst] {
			edges = append(edges, e)
			continue
		}
		if symbolIDs[e.Src] && symbolIDs[e.Dst] {
			edges = append(edges, e)
		}
	}
	return graphExportNodeSets{files: files, symbols: symbols}, edges, nil
}

func renderGraphExportHTML(payload graphExportPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	tmpl, err := template.New("graph").Parse(graphExportHTMLTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"Data": encoded}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const graphExportHTMLTemplate = `<!doctype html>
<html><head><meta charset="utf-8"><title>gg graph export</title>
<style>
body{font-family:system-ui,sans-serif;margin:0;color:#17202a;background:#f8fafc}header{padding:14px 18px;background:#111827;color:white}main{display:grid;grid-template-columns:1fr 320px;height:calc(100vh - 56px)}#graph{width:100%;height:100%;background:white}.panel{border-left:1px solid #d8dee9;padding:12px;overflow:auto}input{width:100%;box-sizing:border-box;padding:8px;border:1px solid #cbd5e1;border-radius:4px}.node{cursor:pointer}.node circle{fill:#2563eb}.node.file circle{fill:#0f766e}.node.decision circle{fill:#7c3aed}.node.task circle{fill:#2563eb}.node.bug circle{fill:#dc2626}.node.note circle{fill:#64748b}.node.rejection circle{fill:#b45309}.node.message circle{fill:#0891b2}.node.hit circle{fill:#f59e0b}.node.neighbor circle{fill:#ef4444}.edge{stroke:#94a3b8;stroke-width:1.2}.label{font-size:11px;fill:#111827}pre{white-space:pre-wrap;font-size:12px}
</style></head><body><header><strong>gg graph export</strong> <span id="meta"></span></header>
<main><svg id="graph"></svg><aside class="panel"><input id="search" placeholder="Search nodes"><h3>Node details</h3><pre id="details">Click a node.</pre></aside></main>
<script>const DATA=JSON.parse(atob('{{.Data}}'));const svg=document.getElementById('graph'),details=document.getElementById('details'),search=document.getElementById('search');document.getElementById('meta').textContent=DATA.nodes.length+' nodes ('+(DATA.memory_nodes||0)+' memory), '+DATA.edges.length+' edges, symbol view '+(DATA.symbol_view?'on':'off');let nodes=[...DATA.nodes,...(DATA.symbols||[])],edges=DATA.edges,w=1200,h=800;function label(n){return (n.properties.path||n.properties.name||n.id).replace(/^file:/,'')}function draw(){svg.setAttribute('viewBox','0 0 '+w+' '+h);svg.innerHTML='';const pos=new Map();nodes.forEach((n,i)=>{let a=2*Math.PI*i/Math.max(nodes.length,1);pos.set(n.id,{x:w/2+Math.cos(a)*Math.min(w,h)*0.38,y:h/2+Math.sin(a)*Math.min(w,h)*0.38})});edges.forEach(e=>{let a=pos.get(e.src),b=pos.get(e.dst);if(!a||!b)return;let l=document.createElementNS('http://www.w3.org/2000/svg','line');l.setAttribute('class','edge');l.setAttribute('x1',a.x);l.setAttribute('y1',a.y);l.setAttribute('x2',b.x);l.setAttribute('y2',b.y);svg.appendChild(l)});nodes.forEach(n=>{let p=pos.get(n.id),g=document.createElementNS('http://www.w3.org/2000/svg','g');g.setAttribute('class','node '+String(n.label).toLowerCase());g.dataset.id=n.id;g.innerHTML='<circle r="7" cx="'+p.x+'" cy="'+p.y+'"></circle><text class="label" x="'+(p.x+10)+'" y="'+(p.y+4)+'">'+escapeHtml(label(n)).slice(0,48)+'</text>';g.onclick=()=>select(n);svg.appendChild(g)})}function select(n){details.textContent=JSON.stringify(n,null,2);let nbr=new Set(edges.filter(e=>e.src===n.id||e.dst===n.id).flatMap(e=>[e.src,e.dst]));document.querySelectorAll('.node').forEach(el=>{el.classList.toggle('hit',el.dataset.id===n.id);el.classList.toggle('neighbor',el.dataset.id!==n.id&&nbr.has(el.dataset.id))})}function escapeHtml(s){return String(s).replace(/[&<>]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]))}search.oninput=()=>{let q=search.value.toLowerCase();document.querySelectorAll('.node').forEach(el=>{let n=nodes.find(x=>x.id===el.dataset.id);el.style.display=!q||JSON.stringify(n).toLowerCase().includes(q)?'':'none'})};draw();</script></body></html>`
