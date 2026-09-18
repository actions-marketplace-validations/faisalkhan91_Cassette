package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// toolTurn builds an Anthropic streaming turn whose request advertises a tool
// schema and whose response emits a tool call with the given args.
func toolTurn(schema, callArgs string) *wirefmt.Interaction {
	req := `{"model":"claude","tools":[{"name":"get_weather","input_schema":` + schema + `}]}`
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{
		ToolCalls: []semequal.ToolCall{{Name: "get_weather", Args: callArgs}}, FinishReason: "tool_use"}, wireenc.Envelope{})
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(req))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sse)}}
}

const weatherSchema = `{"type":"object","required":["city"],"additionalProperties":false,` +
	`"properties":{"city":{"type":"string"},"days":{"type":"integer"},"unit":{"enum":["c","f"]}}}`

func TestSchemaCheck(t *testing.T) {
	cases := []struct {
		name          string
		args          string
		wantOK        bool
		wantInProblem string
	}{
		{"conforms", `{"city":"Paris","days":3,"unit":"c"}`, true, ""},
		{"missing required", `{"days":3}`, false, "missing required key \"city\""},
		{"wrong type", `{"city":"Paris","days":"three"}`, false, "expected integer"},
		{"unknown key", `{"city":"Paris","country":"FR"}`, false, "unknown key \"country\""},
		{"bad enum", `{"city":"Paris","unit":"kelvin"}`, false, "not in enum"},
	}
	for _, c := range cases {
		f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{toolTurn(weatherSchema, c.args)}}
		rep := SchemaCheck(f)
		if rep.OK() != c.wantOK {
			t.Fatalf("%s: OK=%v want %v (%+v)", c.name, rep.OK(), c.wantOK, rep.Violations)
		}
		if rep.Checked != 1 {
			t.Fatalf("%s: checked=%d want 1", c.name, rep.Checked)
		}
		if !c.wantOK {
			found := false
			for _, p := range rep.Violations[0].Problems {
				if containsStr2(p, c.wantInProblem) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s: problems %v missing %q", c.name, rep.Violations[0].Problems, c.wantInProblem)
			}
		}
	}
}

func TestSchemaCheck_MCP(t *testing.T) {
	list := &wirefmt.Interaction{Kind: "mcp",
		Request:  wirefmt.Request{MCPMethod: "tools/list"},
		Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(`{"tools":[{"name":"add","inputSchema":{"type":"object","required":["a","b"],"properties":{"a":{"type":"integer"},"b":{"type":"integer"}}}}]}`))}}
	good := &wirefmt.Interaction{Kind: "mcp", Request: wirefmt.Request{MCPMethod: "tools/call", MCPTool: "add", Body: wirefmt.NewBody([]byte(`{"a":1,"b":2}`))}}
	bad := &wirefmt.Interaction{Kind: "mcp", Request: wirefmt.Request{MCPMethod: "tools/call", MCPTool: "add", Body: wirefmt.NewBody([]byte(`{"a":1}`))}}

	if rep := SchemaCheck(&wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{list, good}}); !rep.OK() || rep.Checked != 1 {
		t.Fatalf("mcp good: %+v", rep)
	}
	if rep := SchemaCheck(&wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{list, bad}}); rep.OK() {
		t.Fatal("mcp bad: expected a missing-required violation")
	}
}

func TestSchemaCheck_NoSchemaSkipped(t *testing.T) {
	// A tool call with no advertised schema is counted unschemaed, not a violation.
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{
		ToolCalls: []semequal.ToolCall{{Name: "mystery", Args: `{"x":1}`}}, FinishReason: "tool_use"}, wireenc.Envelope{})
	it := &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(`{"model":"m"}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sse)}}
	rep := SchemaCheck(&wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{it}})
	if !rep.OK() || rep.Checked != 0 || rep.Unschemaed != 1 {
		t.Fatalf("unschemaed handling wrong: %+v", rep)
	}
}

func containsStr2(s, sub string) bool { return sub == "" || (len(sub) > 0 && indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
