package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func mcpList(toolsJSON string) *wirefmt.Interaction {
	return &wirefmt.Interaction{
		Kind:     "mcp",
		Request:  wirefmt.Request{MCPMethod: "tools/list", Body: wirefmt.NewBody([]byte(`{}`))},
		Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(toolsJSON))},
	}
}

func mcpCall(tool, args, result string) *wirefmt.Interaction {
	return &wirefmt.Interaction{
		Kind:     "mcp",
		Request:  wirefmt.Request{MCPMethod: "tools/call", MCPTool: tool, Body: wirefmt.NewBody([]byte(args))},
		Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(result))},
	}
}

func fileOf(its ...*wirefmt.Interaction) *wirefmt.File {
	return &wirefmt.File{SchemaVersion: 1, Interactions: its}
}

// addSchema advertises tool "add" with int properties a,b (a required).
const addSchema = `{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"required":["a"]}}]}`

func hasKind(rep MCPDiffReport, tool, kind string) bool {
	for _, c := range rep.Changes {
		if c.Tool == tool && c.Kind == kind {
			return true
		}
	}
	return false
}

func TestMCPDiff_Breaking(t *testing.T) {
	old := fileOf(mcpList(addSchema), mcpCall("add", `{"a":1}`, `{"content":[{"type":"text","text":"3"}]}`))

	t.Run("tool_removed", func(t *testing.T) {
		rep := MCPDiff(old, fileOf(mcpList(`{"tools":[]}`)))
		if rep.OK() || !hasKind(rep, "add", "tool_removed") {
			t.Fatalf("expected breaking tool_removed: %+v", rep)
		}
	})

	t.Run("property_removed", func(t *testing.T) {
		newer := fileOf(mcpList(`{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"integer"}},"required":["a"]}}]}`))
		rep := MCPDiff(old, newer)
		if rep.OK() || !hasKind(rep, "add", "property_removed") {
			t.Fatalf("expected breaking property_removed: %+v", rep)
		}
	})

	t.Run("type_changed", func(t *testing.T) {
		newer := fileOf(mcpList(`{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"integer"}},"required":["a"]}}]}`))
		rep := MCPDiff(old, newer)
		if rep.OK() || !hasKind(rep, "add", "type_changed") {
			t.Fatalf("expected breaking type_changed: %+v", rep)
		}
	})

	t.Run("required_added", func(t *testing.T) {
		// b becomes required (existing clients that omit it break).
		newer := fileOf(mcpList(`{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"required":["a","b"]}}]}`))
		rep := MCPDiff(old, newer)
		if rep.OK() || !hasKind(rep, "add", "required_added") {
			t.Fatalf("expected breaking required_added: %+v", rep)
		}
	})

	t.Run("now_errors", func(t *testing.T) {
		newer := fileOf(mcpList(addSchema), mcpCall("add", `{"a":1}`, `{"isError":true,"content":[{"type":"text","text":"boom"}]}`))
		rep := MCPDiff(old, newer)
		if rep.OK() || !hasKind(rep, "add", "now_errors") {
			t.Fatalf("expected breaking now_errors: %+v", rep)
		}
	})
}

// Type comparison is set-based: widening a type to a nullable union is NOT breaking,
// but narrowing (dropping an accepted type) is.
func TestMCPDiff_UnionTypes(t *testing.T) {
	nullable := `{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":["integer","null"]}},"required":["a"]}}]}`
	strict := `{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"integer"}},"required":["a"]}}]}`

	// integer -> [integer,null] widens: not breaking.
	if rep := MCPDiff(fileOf(mcpList(strict)), fileOf(mcpList(nullable))); !rep.OK() {
		t.Fatalf("widening to a nullable union must not be breaking: %+v", rep.Changes)
	}
	// [integer,null] -> integer narrows (drops null): breaking.
	if rep := MCPDiff(fileOf(mcpList(nullable)), fileOf(mcpList(strict))); rep.OK() || !hasKind(rep, "add", "type_changed") {
		t.Fatalf("narrowing away null must be breaking: %+v", rep.Changes)
	}
}

// Tightening additionalProperties from allowed to false is breaking.
func TestMCPDiff_AdditionalProperties(t *testing.T) {
	open := `{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"integer"}}}}]}`
	closed := `{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"integer"}},"additionalProperties":false}}]}`
	if rep := MCPDiff(fileOf(mcpList(open)), fileOf(mcpList(closed))); rep.OK() || !hasKind(rep, "add", "additional_properties_forbidden") {
		t.Fatalf("tightening additionalProperties to false must be breaking: %+v", rep.Changes)
	}
}

// A success→error regression must be caught even when the new recording's call args
// are in a different JSON key order (callKey canonicalizes).
func TestMCPDiff_NowErrors_CanonicalArgs(t *testing.T) {
	old := fileOf(mcpList(addSchema), mcpCall("add", `{"a":1,"b":2}`, `{"content":[{"type":"text","text":"3"}]}`))
	newer := fileOf(mcpList(addSchema), mcpCall("add", `{"b":2,"a":1}`, `{"isError":true,"content":[{"type":"text","text":"boom"}]}`))
	rep := MCPDiff(old, newer)
	if rep.OK() || !hasKind(rep, "add", "now_errors") {
		t.Fatalf("reordered-arg success→error regression must be detected: %+v", rep.Changes)
	}
}

func TestMCPDiff_NonBreaking(t *testing.T) {
	old := fileOf(mcpList(addSchema))

	// A brand-new tool and a new optional property are additive, not breaking.
	newer := fileOf(mcpList(`{"tools":[
		{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"},"c":{"type":"integer"}},"required":["a"]}},
		{"name":"sub","inputSchema":{"type":"object","properties":{"a":{"type":"integer"}}}}
	]}`))
	rep := MCPDiff(old, newer)
	if !rep.OK() {
		t.Fatalf("additive changes must not be breaking: %+v", rep)
	}
	if !hasKind(rep, "sub", "tool_added") || !hasKind(rep, "add", "property_added") {
		t.Fatalf("expected tool_added + property_added reported: %+v", rep)
	}
}

// Identical contracts produce no changes.
func TestMCPDiff_Identical(t *testing.T) {
	f := fileOf(mcpList(addSchema), mcpCall("add", `{"a":1}`, `{"content":[]}`))
	if rep := MCPDiff(f, f); !rep.OK() || len(rep.Changes) != 0 {
		t.Fatalf("identical recordings should diff clean: %+v", rep)
	}
}
