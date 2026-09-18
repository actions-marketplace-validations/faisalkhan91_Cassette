package analysis_test

import (
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func toolUseSSE() []byte {
	return []byte(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t","name":"get_weather"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Paris\"}"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}

`)
}

func fileWith(bodies ...[]byte) *wirefmt.File {
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion}
	for _, b := range bodies {
		f.Interactions = append(f.Interactions, &wirefmt.Interaction{Kind: "http",
			Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(`{"model":"m"}`))},
			Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(b)}})
	}
	return f
}

func TestBuildAttestManifest(t *testing.T) {
	f := fileWith(sseAnthropic("hi"), toolUseSSE())
	m := analysis.BuildAttestManifest(f)
	if m.Combined == "" || len(m.TurnDigests) != 2 || len(m.WireShapes) != 2 {
		t.Fatalf("manifest incomplete: %+v", m)
	}
	if len(m.Tools) != 1 || m.Tools[0] != "get_weather" {
		t.Fatalf("tools = %v", m.Tools)
	}
	if m.CassetteSHA == "" {
		t.Fatal("cassette sha missing")
	}
}

func TestAttestManifest_SemanticEqual(t *testing.T) {
	a := analysis.BuildAttestManifest(fileWith(sseAnthropic("hi")))
	b := analysis.BuildAttestManifest(fileWith(sseAnthropic("hi")))
	if !a.SemanticEqual(b) {
		t.Fatal("identical behavior should be semantically equal")
	}
	c := analysis.BuildAttestManifest(fileWith(sseAnthropic("different")))
	if a.SemanticEqual(c) {
		t.Fatal("different text should not be semantically equal")
	}
	// Expect differences are part of the semantic identity.
	fa := fileWith(sseAnthropic("hi"))
	fa.Expect = &wirefmt.Expect{FinishesClean: true}
	if analysis.BuildAttestManifest(fa).SemanticEqual(a) {
		t.Fatal("differing Expect should break semantic equality")
	}
}
