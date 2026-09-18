package analysis

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// SchemaViolation is one emitted tool call whose arguments don't conform to the
// schema the request advertised for that tool.
type SchemaViolation struct {
	Turn     int      `json:"turn"`
	Tool     string   `json:"tool"`
	Problems []string `json:"problems"`
}

// SchemaReport is the whole-file tool-arg contract result.
type SchemaReport struct {
	Checked    int               `json:"checked"`    // tool calls validated against a known schema
	Unschemaed int               `json:"unschemaed"` // tool calls with no advertised schema (skipped)
	Violations []SchemaViolation `json:"violations"`
}

// OK reports whether every validated tool call conformed.
func (r SchemaReport) OK() bool { return len(r.Violations) == 0 }

// SchemaCheck validates that every emitted tool call's arguments conform to the
// JSON Schema the model was given for that tool — proving the model honored its own
// tool contract. The advertised schemas come from each HTTP request's tools[]
// (Anthropic input_schema / OpenAI function.parameters / Gemini functionDeclarations)
// and, for MCP, from the recorded tools/list result. Validation is a hand-rolled
// structural check of the common JSON Schema subset (type, required, properties,
// additionalProperties:false, enum) — no schema-library dependency. Pure, offline.
func SchemaCheck(f *wirefmt.File) SchemaReport {
	var rep SchemaReport
	mcpSchemas := mcpToolSchemas(f)
	turn := -1
	for _, it := range f.Interactions {
		switch it.Kind {
		case "http":
			turn++
			schemas := httpToolSchemas(it)
			tr, _, _ := DecodeInteraction(it)
			for _, call := range tr.ToolCalls {
				validateCall(&rep, turn, call.Name, call.Args, schemas)
			}
		case "mcp":
			turn++
			if it.Request.MCPMethod != "tools/call" {
				continue
			}
			validateCall(&rep, turn, it.Request.MCPTool, string(it.Request.Body.Bytes()), mcpSchemas)
		}
	}
	return rep
}

func validateCall(rep *SchemaReport, turn int, tool, argsJSON string, schemas map[string]gjson.Result) {
	schema, ok := schemas[tool]
	if !ok {
		rep.Unschemaed++
		return
	}
	rep.Checked++
	var args any
	if argsJSON == "" {
		args = map[string]any{}
	} else if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		rep.Violations = append(rep.Violations, SchemaViolation{Turn: turn, Tool: tool, Problems: []string{"arguments are not valid JSON"}})
		return
	}
	var schemaObj any
	if err := json.Unmarshal([]byte(schema.Raw), &schemaObj); err != nil {
		return // unparseable advertised schema — nothing to validate against
	}
	if probs := validateValue(args, schemaObj, ""); len(probs) > 0 {
		sort.Strings(probs)
		rep.Violations = append(rep.Violations, SchemaViolation{Turn: turn, Tool: tool, Problems: probs})
	}
}

// validateValue checks v against a JSON Schema subset, prefixing problems with path.
func validateValue(v, schemaAny any, path string) []string {
	schema, ok := schemaAny.(map[string]any)
	if !ok {
		return nil
	}
	var probs []string
	at := func(s string) string {
		if path == "" {
			return s
		}
		return path + ": " + s
	}

	if enum, ok := schema["enum"].([]any); ok && !enumContains(enum, v) {
		probs = append(probs, at(fmt.Sprintf("value %v not in enum", v)))
	}
	switch typeOf(schema["type"]) {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return append(probs, at("expected object"))
		}
		props, _ := schema["properties"].(map[string]any)
		for _, req := range toStrings(schema["required"]) {
			if _, present := obj[req]; !present {
				probs = append(probs, at("missing required key "+quote(req)))
			}
		}
		additional, hasAdd := schema["additionalProperties"].(bool)
		for k, val := range obj {
			ps, known := props[k]
			if !known {
				if hasAdd && !additional {
					probs = append(probs, at("unknown key "+quote(k)))
				}
				continue
			}
			probs = append(probs, validateValue(val, ps, joinPath(path, k))...)
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return append(probs, at("expected array"))
		}
		if items, ok := schema["items"]; ok {
			for i, e := range arr {
				probs = append(probs, validateValue(e, items, joinPath(path, fmt.Sprintf("[%d]", i)))...)
			}
		}
	case "string":
		if _, ok := v.(string); !ok {
			probs = append(probs, at("expected string"))
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			probs = append(probs, at("expected boolean"))
		}
	case "integer":
		if n, ok := v.(float64); !ok || n != float64(int64(n)) {
			probs = append(probs, at("expected integer"))
		}
	case "number":
		if _, ok := v.(float64); !ok {
			probs = append(probs, at("expected number"))
		}
	}
	return probs
}

// httpToolSchemas maps tool name -> advertised parameter schema from a request's
// tools[], across the Anthropic / OpenAI / Gemini shapes.
func httpToolSchemas(it *wirefmt.Interaction) map[string]gjson.Result {
	out := map[string]gjson.Result{}
	body := it.Request.Body.Bytes()
	gjson.GetBytes(body, "tools").ForEach(func(_, tool gjson.Result) bool {
		// Gemini: a tool wraps functionDeclarations[].
		if fd := tool.Get("functionDeclarations"); fd.Exists() {
			fd.ForEach(func(_, d gjson.Result) bool {
				if n := d.Get("name").String(); n != "" {
					out[n] = d.Get("parameters")
				}
				return true
			})
			return true
		}
		name := firstNonEmpty(tool.Get("name").String(), tool.Get("function.name").String())
		schema := firstExisting(tool.Get("input_schema"), tool.Get("function.parameters"), tool.Get("parameters"))
		if name != "" && schema.Exists() {
			out[name] = schema
		}
		return true
	})
	return out
}

// mcpToolSchemas maps tool name -> inputSchema from any recorded MCP tools/list result.
func mcpToolSchemas(f *wirefmt.File) map[string]gjson.Result {
	out := map[string]gjson.Result{}
	for _, it := range f.Interactions {
		if it.Kind != "mcp" || it.Request.MCPMethod != "tools/list" {
			continue
		}
		gjson.GetBytes(it.Response.Body.Bytes(), "tools").ForEach(func(_, tool gjson.Result) bool {
			if n := tool.Get("name").String(); n != "" {
				out[n] = firstExisting(tool.Get("inputSchema"), tool.Get("input_schema"))
			}
			return true
		})
	}
	return out
}

func typeOf(v any) string { s, _ := v.(string); return s }

func enumContains(enum []any, v any) bool {
	want, _ := json.Marshal(v)
	for _, e := range enum {
		if got, _ := json.Marshal(e); string(got) == string(want) {
			return true
		}
	}
	return false
}

func toStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func firstExisting(rs ...gjson.Result) gjson.Result {
	for _, r := range rs {
		if r.Exists() {
			return r
		}
	}
	return gjson.Result{}
}

func joinPath(a, b string) string {
	if a == "" {
		return b
	}
	if strings.HasPrefix(b, "[") {
		return a + b
	}
	return a + "." + b
}

func quote(s string) string { return "\"" + s + "\"" }
