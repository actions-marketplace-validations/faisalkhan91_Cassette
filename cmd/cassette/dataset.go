package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/tidwall/gjson"
)

// datasetRow is one interaction projected into a flat, queryable record. The
// rows ARE the byte-exact replay fixtures, with semantic fields attached.
type datasetRow struct {
	Cassette     string   `json:"cassette"`
	Index        int      `json:"index"`
	Model        string   `json:"model"`
	SystemDigest string   `json:"system_digest,omitempty"`
	UserText     string   `json:"user_text,omitempty"`
	ToolsOffered []string `json:"tools_offered,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
	ToolCalls    []string `json:"tool_calls,omitempty"`
}

// cmdDataset indexes every decodable http interaction under a directory into a
// flat dataset, filterable by --where field=value (flat equality), emitted as
// jsonl (default), csv, or a table. Derived only; never re-records.
func cmdDataset(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("format", "where"), args, datasetUsageText, stderr)
	if !ok {
		return exitUsage
	}
	dir := "."
	if in.nargs() > 0 {
		dir = in.arg(in.nargs() - 1) // last positional wins, mirroring the prior loop
	}
	format := "jsonl"
	if in.has("format") {
		format = in.str("format")
	}
	where := map[string]string{}
	for _, spec := range in.list("where") {
		kv := strings.SplitN(spec, "=", 2)
		if len(kv) != 2 {
			return datasetUsage(stderr)
		}
		where[kv[0]] = kv[1]
	}
	if format != "jsonl" && format != "csv" && format != "table" {
		fmt.Fprintln(stderr, "cassette: --format must be jsonl, csv, or table")
		return exitUsage
	}

	files := walkYAML(dir)

	var rows []datasetRow
	for _, p := range files {
		f, err := wirefmt.Load(p)
		if err != nil {
			continue // skip non-cassette yaml
		}
		for i, it := range f.Interactions {
			if it.Kind != "http" {
				continue
			}
			tr, _, ok := analysis.DecodeInteraction(it)
			if !ok {
				continue // skip unary/undecodable, mirroring doc
			}
			row := datasetRow{
				Cassette:     filepath.Base(p),
				Index:        i,
				Model:        analysis.RequestModel(it),
				SystemDigest: systemDigest(it.Request.Body.Bytes()),
				UserText:     lastUserText(it.Request.Body.Bytes()),
				ToolsOffered: toolsOffered(it.Request.Body.Bytes()),
				FinishReason: tr.FinishReason,
			}
			for _, tc := range tr.ToolCalls {
				row.ToolCalls = append(row.ToolCalls, tc.Name)
			}
			if matchWhere(row, where) {
				rows = append(rows, row)
			}
		}
	}

	switch format {
	case "csv":
		return emitCSV(stdout, rows)
	case "table":
		return emitTable(stdout, rows)
	default:
		return emitJSONL(stdout, rows)
	}
}

func systemDigest(body []byte) string {
	v := gjson.GetBytes(body, "system")
	if !v.Exists() {
		return ""
	}
	c, _ := canon.Canonicalize([]byte(v.Raw))
	return canon.SumHex(c)[:12]
}

func lastUserText(body []byte) string {
	msgs := gjson.GetBytes(body, "messages")
	var last string
	msgs.ForEach(func(_, m gjson.Result) bool {
		if m.Get("role").String() != "user" {
			return true
		}
		content := m.Get("content")
		if content.Type == gjson.String {
			last = content.String()
		} else { // array of blocks (Anthropic / multimodal)
			var parts []string
			content.ForEach(func(_, b gjson.Result) bool {
				if t := b.Get("text").String(); t != "" {
					parts = append(parts, t)
				}
				return true
			})
			last = strings.Join(parts, " ")
		}
		return true
	})
	return truncate(last, 200)
}

func toolsOffered(body []byte) []string {
	var names []string
	gjson.GetBytes(body, "tools").ForEach(func(_, t gjson.Result) bool {
		n := t.Get("name").String()
		if n == "" {
			n = t.Get("function.name").String()
		}
		if n != "" {
			names = append(names, n)
		}
		return true
	})
	return names
}

func matchWhere(r datasetRow, where map[string]string) bool {
	for k, v := range where {
		switch k {
		case "model":
			if r.Model != v {
				return false
			}
		case "finish", "finish_reason":
			if r.FinishReason != v {
				return false
			}
		case "cassette":
			if r.Cassette != v {
				return false
			}
		case "tool":
			if !slices.Contains(r.ToolCalls, v) {
				return false
			}
		default:
			return false // unknown field never matches
		}
	}
	return true
}

func emitJSONL(w io.Writer, rows []datasetRow) int {
	enc := json.NewEncoder(w)
	for _, r := range rows {
		enc.Encode(r)
	}
	return exitOK
}

func emitCSV(w io.Writer, rows []datasetRow) int {
	cw := csv.NewWriter(w)
	cw.Write([]string{"cassette", "index", "model", "system_digest", "user_text", "tools_offered", "finish_reason", "tool_calls"})
	for _, r := range rows {
		cw.Write([]string{r.Cassette, fmt.Sprint(r.Index), r.Model, r.SystemDigest, r.UserText,
			strings.Join(r.ToolsOffered, "|"), r.FinishReason, strings.Join(r.ToolCalls, "|")})
	}
	cw.Flush()
	return exitOK
}

func emitTable(w io.Writer, rows []datasetRow) int {
	fmt.Fprintf(w, "%-22s %3s  %-20s %-10s %s\n", "cassette", "#", "model", "finish", "tools→calls")
	for _, r := range rows {
		fmt.Fprintf(w, "%-22s %3d  %-20s %-10s %s → %s\n", truncate(r.Cassette, 22), r.Index,
			truncate(r.Model, 20), r.FinishReason, strings.Join(r.ToolsOffered, ","), strings.Join(r.ToolCalls, ","))
	}
	fmt.Fprintf(w, "(%d rows)\n", len(rows))
	return exitOK
}

const datasetUsageText = "usage: cassette dataset [dir] [--where field=value]... [--format jsonl|csv|table]"

func datasetUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, datasetUsageText)
	return exitUsage
}

// walkYAML returns every .yaml file under dir, sorted. The shared corpus-collection
// helper for the directory-walking commands (dataset, redteam, orphans), so the walk
// rule lives in one place.
func walkYAML(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".yaml") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files
}
