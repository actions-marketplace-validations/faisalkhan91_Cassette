package analysis

import (
	"sort"

	"github.com/tidwall/gjson"

	"github.com/faisalkhan91/cassette/internal/canon"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// MCPChange is one detected difference between two MCP recordings' tool contracts.
// Breaking changes are the ones that can break an existing client: a tool that
// disappeared, an input schema that got stricter (a property removed, a type
// changed, or a new required property), or a call that used to succeed and now
// errors. Additive changes (a new tool, a new optional property) are reported but
// not breaking.
type MCPChange struct {
	Tool     string `json:"tool"`
	Kind     string `json:"kind"`
	Detail   string `json:"detail"`
	Breaking bool   `json:"breaking"`
}

// MCPDiffReport is the result of comparing two MCP recordings' tool contracts.
type MCPDiffReport struct {
	Changes  []MCPChange `json:"changes"`
	Breaking int         `json:"breaking"`
}

// OK reports whether there were zero breaking changes.
func (r MCPDiffReport) OK() bool { return r.Breaking == 0 }

func (r *MCPDiffReport) add(c MCPChange) {
	if c.Breaking {
		r.Breaking++
	}
	r.Changes = append(r.Changes, c)
}

// MCPDiff compares the tool contracts of two MCP recordings (old vs new) and reports
// the changes, flagging the breaking ones. It is the basis of the `cassette mcp-diff
// --fail-on-breaking` CI gate: record a golden MCP session, re-record against the
// updated server, and fail the build if the server broke an advertised tool. Schema
// comparison covers the TOP-LEVEL input properties (name/type/required/
// additionalProperties); nested object schemas and $ref are not yet walked.
func MCPDiff(old, newer *wirefmt.File) MCPDiffReport {
	rep := MCPDiffReport{Changes: []MCPChange{}} // empty serializes as [] not null
	oldTools, newTools := mcpToolSchemas(old), mcpToolSchemas(newer)

	for _, name := range sortedUnionKeys(oldTools, newTools) {
		oldSchema, inOld := oldTools[name]
		newSchema, inNew := newTools[name]
		switch {
		case inOld && !inNew:
			rep.add(MCPChange{name, "tool_removed", "tool is no longer advertised", true})
		case !inOld && inNew:
			rep.add(MCPChange{name, "tool_added", "new tool advertised", false})
		default:
			for _, c := range schemaChanges(name, oldSchema, newSchema) {
				rep.add(c)
			}
		}
	}
	for _, c := range callRegressions(old, newer) {
		rep.add(c)
	}
	return rep
}

// schemaChanges compares two JSON-Schema-ish tool input schemas and reports
// backward-incompatible changes (and additive ones, non-breaking).
func schemaChanges(tool string, oldS, newS gjson.Result) []MCPChange {
	var out []MCPChange
	oldProps, newProps := oldS.Get("properties"), newS.Get("properties")
	oldReq, newReq := requiredSet(oldS), requiredSet(newS)

	// Properties removed (breaking) or with a changed type (breaking).
	for _, name := range sortedResultKeys(oldProps) {
		np := newProps.Get(escapeKey(name))
		if !np.Exists() {
			out = append(out, MCPChange{tool, "property_removed", "input property " + name + " was removed", true})
			continue
		}
		// Compare types as sets so a nullable union (["string","null"]) vs "string" is
		// judged by narrowing, not raw inequality: dropping a previously-allowed type is
		// breaking; widening (adding one) is not.
		oldT := typeSet(oldProps.Get(escapeKey(name)).Get("type"))
		newT := typeSet(np.Get("type"))
		if len(oldT) > 0 && len(newT) > 0 {
			for t := range oldT {
				if !newT[t] {
					out = append(out, MCPChange{tool, "type_changed", "input property " + name + " no longer accepts type " + t, true})
					break
				}
			}
		}
	}
	// additionalProperties tightened from allowed to false is breaking (clients that
	// send extra fields start failing).
	if additionalAllowed(oldS) && !additionalAllowed(newS) {
		out = append(out, MCPChange{tool, "additional_properties_forbidden", "additionalProperties tightened to false", true})
	}
	// Properties added: breaking only if newly required.
	for _, name := range sortedResultKeys(newProps) {
		if !oldProps.Get(escapeKey(name)).Exists() {
			breaking := newReq[name] && !oldReq[name]
			detail := "new optional input property " + name
			kind := "property_added"
			if breaking {
				detail = "new REQUIRED input property " + name + " (existing clients omit it)"
				kind = "required_added"
			}
			out = append(out, MCPChange{tool, kind, detail, breaking})
		}
	}
	// A previously-optional property that became required is breaking.
	for name := range newReq {
		if newProps.Get(escapeKey(name)).Exists() && oldProps.Get(escapeKey(name)).Exists() && !oldReq[name] {
			out = append(out, MCPChange{tool, "required_added", "input property " + name + " is now required", true})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

// callRegressions flags a tools/call that succeeded in old but errors in new (same
// tool + canonical arguments) — a behavioral break even when the schema is unchanged.
func callRegressions(old, newer *wirefmt.File) []MCPChange {
	newErr := map[string]bool{}
	for _, it := range newer.Interactions {
		if it.Kind == "mcp" && it.Request.MCPMethod == "tools/call" {
			newErr[callKey(it)] = mcpIsError(it.Response.Body.Bytes())
		}
	}
	var out []MCPChange
	seen := map[string]bool{}
	for _, it := range old.Interactions {
		if it.Kind != "mcp" || it.Request.MCPMethod != "tools/call" {
			continue
		}
		k := callKey(it)
		if seen[k] || mcpIsError(it.Response.Body.Bytes()) {
			continue // only old successes can regress; dedup identical calls
		}
		seen[k] = true
		if errored, ok := newErr[k]; ok && errored {
			out = append(out, MCPChange{it.Request.MCPTool, "now_errors", "a call that succeeded now returns an error", true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}

func callKey(it *wirefmt.Interaction) string {
	// Canonicalize the arguments so two recordings with the same call but different
	// JSON key order / whitespace still match (else a real success→error regression
	// is missed). Mirrors rekey.CallKey's digest of canonical args.
	return it.Request.MCPTool + "|" + canon.Digest(it.Request.Body.Bytes())
}

// typeSet returns a JSON-Schema "type" as a set, accepting a string ("string") or an
// array (["string","null"]).
func typeSet(t gjson.Result) map[string]bool {
	out := map[string]bool{}
	if t.IsArray() {
		t.ForEach(func(_, v gjson.Result) bool {
			if s := v.String(); s != "" {
				out[s] = true
			}
			return true
		})
	} else if s := t.String(); s != "" {
		out[s] = true
	}
	return out
}

// additionalAllowed reports whether a schema accepts properties beyond those declared
// (the JSON-Schema default is true; only an explicit additionalProperties:false forbids).
func additionalAllowed(schema gjson.Result) bool {
	ap := schema.Get("additionalProperties")
	return !(ap.Exists() && ap.Type == gjson.False)
}

func requiredSet(schema gjson.Result) map[string]bool {
	out := map[string]bool{}
	schema.Get("required").ForEach(func(_, v gjson.Result) bool {
		out[v.String()] = true
		return true
	})
	return out
}

func sortedResultKeys(obj gjson.Result) []string {
	var keys []string
	obj.ForEach(func(k, _ gjson.Result) bool {
		keys = append(keys, k.String())
		return true
	})
	sort.Strings(keys)
	return keys
}

func sortedUnionKeys(a, b map[string]gjson.Result) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// escapeKey quotes a gjson path element so a property name containing '.' or '*'
// is treated literally.
func escapeKey(k string) string {
	var b []byte
	for i := 0; i < len(k); i++ {
		if k[i] == '.' || k[i] == '*' || k[i] == '?' || k[i] == '\\' {
			b = append(b, '\\')
		}
		b = append(b, k[i])
	}
	return string(b)
}
