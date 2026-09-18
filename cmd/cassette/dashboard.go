package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// cmdDashboard renders a corpus to a single self-contained, offline HTML report:
// the behavioral coverage matrix, token totals, refusal verdicts, tool usage, and
// a per-cassette transcript. No server, no external assets, no timestamps — the
// output is byte-stable for a given corpus and can be committed or attached to CI.
func cmdDashboard(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("out").alias("o", "out"), args, dashboardUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	if src == "" {
		src = "."
	}
	out := in.str("out")
	if out == "" || in.nargs() > 1 {
		return dashboardUsage(stderr)
	}
	paths, err := lintTargets(src)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	files, err := loadAll(paths)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	data := buildDashboard(paths, files)
	var buf []byte
	if buf, err = renderDashboard(data); err != nil {
		fmt.Fprintf(stderr, "cassette: render: %v\n", err)
		return exitFail
	}
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		fmt.Fprintf(stderr, "cassette: write: %v\n", err)
		return exitFail
	}
	fmt.Fprintf(stdout, "wrote %s — %d cassette(s), %d turn(s)\n", out, data.Coverage.Cassettes, data.Coverage.Turns)
	return exitOK
}

// dashboardData is the fully-resolved, deterministic model the template renders.
type dashboardData struct {
	Coverage    analysis.CoverageReport
	Providers   []cell
	Tools       []cell
	Finish      []cell
	Refusal     []cell
	Models      []modelCost
	TotalInput  int
	TotalOutput int
	Cassettes   []cassetteRow
}

type cell struct {
	Name  string
	Count int
}

type modelCost struct {
	Model        string
	InputTokens  int
	OutputTokens int
}

type cassetteRow struct {
	Name       string
	Anchor     string
	Turns      int
	Providers  string
	Tokens     int
	Refusal    string
	Transcript string
}

func buildDashboard(paths []string, files []*wirefmt.File) dashboardData {
	d := dashboardData{Coverage: analysis.Coverage(files)}
	d.Providers = toCells(d.Coverage.Providers)
	d.Tools = toCells(d.Coverage.Tools)
	d.Finish = toCells(d.Coverage.Finish)
	d.Refusal = toCells(d.Coverage.Refusal)

	models := map[string]*modelCost{}
	for i, f := range files {
		name := filepath.Base(paths[i])
		row := cassetteRow{
			Name:       name,
			Anchor:     anchorFor(name, i),
			Transcript: renderDoc(paths[i], f),
		}
		provSet := map[string]bool{}
		for _, it := range f.Interactions {
			if it.Kind != "http" {
				continue
			}
			provSet[analysis.ProviderName(wireenc.ProviderForURL(it.Request.URL))] = true
			u := analysis.DecodeUsage(it)
			row.Tokens += u.Total()
			d.TotalInput += u.InputTokens
			d.TotalOutput += u.OutputTokens
			if m := analysis.RequestModel(it); m != "" {
				mc := models[m]
				if mc == nil {
					mc = &modelCost{Model: m}
					models[m] = mc
				}
				mc.InputTokens += u.InputTokens
				mc.OutputTokens += u.OutputTokens
			}
		}
		turns := analysis.CollectTurns(f)
		row.Turns = len(turns)
		row.Refusal = string(analysis.FinalVerdict(turns))
		row.Providers = joinSortedSet(provSet)
		d.Cassettes = append(d.Cassettes, row)
	}
	for _, mc := range models {
		d.Models = append(d.Models, *mc)
	}
	sort.Slice(d.Models, func(i, j int) bool { return d.Models[i].Model < d.Models[j].Model })
	return d
}

func toCells(m map[string]int) []cell {
	out := make([]cell, 0, len(m))
	for k, v := range m {
		out = append(out, cell{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func joinSortedSet(set map[string]bool) string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}

func anchorFor(name string, i int) string {
	return fmt.Sprintf("c%d-%s", i, name)
}

func renderDashboard(d dashboardData) ([]byte, error) {
	t, err := template.New("dashboard").Parse(dashboardTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const dashboardUsageText = "usage: cassette dashboard [dir] -o <report.html>"

func dashboardUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, dashboardUsageText)
	return exitUsage
}

// dashboardTemplate is a self-contained HTML document: inline CSS, no external
// assets, no scripts, no timestamps. html/template auto-escapes all interpolated
// values (including each transcript shown verbatim in a <pre>).
const dashboardTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>cassette — corpus report</title>
<style>
:root { color-scheme: light dark; }
body { font: 15px/1.5 -apple-system, system-ui, sans-serif; margin: 0; padding: 2rem;
  max-width: 64rem; margin: 0 auto; color: #1a1a1a; background: #fff; }
@media (prefers-color-scheme: dark) { body { color: #e6e6e6; background: #161616; } }
h1 { font-size: 1.6rem; margin: 0 0 .25rem; }
h2 { font-size: 1.1rem; margin: 2rem 0 .5rem; border-bottom: 1px solid #8884; padding-bottom: .25rem; }
.sub { color: #8a8a8a; margin: 0 0 1rem; }
.cards { display: flex; flex-wrap: wrap; gap: .5rem; }
.card { border: 1px solid #8884; border-radius: 8px; padding: .5rem .75rem; }
.card .n { font-size: 1.4rem; font-weight: 600; }
.card .l { color: #8a8a8a; font-size: .85rem; }
table { border-collapse: collapse; width: 100%; margin: .5rem 0; }
th, td { text-align: left; padding: .35rem .6rem; border-bottom: 1px solid #8883; }
th { color: #8a8a8a; font-weight: 600; font-size: .85rem; }
code, pre { font-family: ui-monospace, Menlo, monospace; }
.pill { display: inline-block; background: #6663; border-radius: 99px; padding: 0 .5rem; margin: 0 .2rem .2rem 0; font-size: .85rem; }
.refused { color: #c0392b; } .complied { color: #1e8449; } .unknown { color: #8a8a8a; }
details { border: 1px solid #8884; border-radius: 8px; margin: .4rem 0; }
summary { cursor: pointer; padding: .5rem .75rem; font-weight: 600; }
details pre { white-space: pre-wrap; word-break: break-word; margin: 0; padding: 0 .75rem .75rem; font-size: .8rem; }
footer { margin-top: 3rem; color: #8a8a8a; font-size: .8rem; }
</style>
</head>
<body>
<h1>cassette — corpus report</h1>
<p class="sub">{{.Coverage.Cassettes}} cassette(s) · {{.Coverage.Turns}} turn(s) · {{.Coverage.Streaming}} streaming, {{.Coverage.Unary}} unary</p>

<div class="cards">
  <div class="card"><div class="n">{{.Coverage.Cassettes}}</div><div class="l">cassettes</div></div>
  <div class="card"><div class="n">{{.Coverage.Turns}}</div><div class="l">turns</div></div>
  <div class="card"><div class="n">{{.TotalInput}}</div><div class="l">input tokens</div></div>
  <div class="card"><div class="n">{{.TotalOutput}}</div><div class="l">output tokens</div></div>
</div>

<h2>Coverage</h2>
<table>
<tr><th>Providers</th><td>{{range .Providers}}<span class="pill">{{.Name}} ×{{.Count}}</span>{{else}}<span class="l">(none)</span>{{end}}</td></tr>
<tr><th>Tools</th><td>{{range .Tools}}<span class="pill">{{.Name}} ×{{.Count}}</span>{{else}}<span class="l">(none)</span>{{end}}</td></tr>
<tr><th>Finish</th><td>{{range .Finish}}<span class="pill">{{.Name}} ×{{.Count}}</span>{{else}}<span class="l">(none)</span>{{end}}</td></tr>
<tr><th>Refusal</th><td>{{range .Refusal}}<span class="pill">{{.Name}} ×{{.Count}}</span>{{else}}<span class="l">(none)</span>{{end}}</td></tr>
</table>

{{if .Models}}
<h2>Tokens by model</h2>
<table>
<tr><th>Model</th><th>Input</th><th>Output</th></tr>
{{range .Models}}<tr><td><code>{{.Model}}</code></td><td>{{.InputTokens}}</td><td>{{.OutputTokens}}</td></tr>
{{end}}</table>
{{end}}

<h2>Cassettes</h2>
<table>
<tr><th>Cassette</th><th>Turns</th><th>Providers</th><th>Tokens</th><th>Verdict</th></tr>
{{range .Cassettes}}<tr><td><a href="#{{.Anchor}}">{{.Name}}</a></td><td>{{.Turns}}</td><td>{{.Providers}}</td><td>{{.Tokens}}</td><td class="{{.Refusal}}">{{.Refusal}}</td></tr>
{{end}}</table>

<h2>Transcripts</h2>
{{range .Cassettes}}<details id="{{.Anchor}}"><summary>{{.Name}}</summary><pre>{{.Transcript}}</pre></details>
{{end}}

<footer>Generated by <code>cassette dashboard</code> — offline, deterministic, no external assets.</footer>
</body>
</html>
`
