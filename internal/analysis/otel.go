package analysis

import (
	"encoding/json"
	"strconv"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// OTel export: map a recorded cassette to OpenTelemetry GenAI spans (OTLP/JSON), so a
// cassette can be imported into Langfuse / Phoenix / SigNoz / any OTLP backend — a
// bridge INTO the observability stack rather than a competitor to it. The GenAI
// semantic conventions are still pre-stable, so this is a best-effort mapping using
// the current attribute names (gen_ai.provider.name — not the deprecated
// gen_ai.system — gen_ai.operation.name, gen_ai.usage.*, plus the MCP convention).
//
// Output is deterministic: span/trace IDs are digests of the recorded content (never
// random) and timestamps are derived from the recorded StreamTiming when present
// (zero otherwise), so the same cassette always exports identical bytes.

// OTLP/JSON structures (the subset the GenAI mapping needs). Per the OTLP/JSON
// encoding, 64-bit ints are strings and trace/span IDs are hex strings.
type otlpExport struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpKV `json:"attributes"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name string `json:"name"`
}

type otlpSpan struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	Name              string     `json:"name"`
	Kind              int        `json:"kind"` // 3 = CLIENT
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []otlpKV   `json:"attributes"`
	Status            otlpStatus `json:"status"`
}

type otlpStatus struct {
	Code int `json:"code"` // 1 = OK, 2 = ERROR
}

type otlpKV struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue *string         `json:"stringValue,omitempty"`
	IntValue    *string         `json:"intValue,omitempty"`
	DoubleValue *float64        `json:"doubleValue,omitempty"`
	ArrayValue  *otlpArrayValue `json:"arrayValue,omitempty"`
}

type otlpArrayValue struct {
	Values []otlpAnyValue `json:"values"`
}

func strVal(s string) otlpAnyValue { return otlpAnyValue{StringValue: &s} }
func intVal(n int) otlpAnyValue {
	s := strconv.FormatInt(int64(n), 10)
	return otlpAnyValue{IntValue: &s}
}
func dblVal(f float64) otlpAnyValue { return otlpAnyValue{DoubleValue: &f} }
func arrStr(ss ...string) otlpAnyValue {
	a := &otlpArrayValue{}
	for _, s := range ss {
		a.Values = append(a.Values, strVal(s))
	}
	return otlpAnyValue{ArrayValue: a}
}

// MarshalOTLP returns the indented OTLP/JSON encoding of f's interactions as GenAI
// spans — ready to write to a file or POST to an OTLP/HTTP traces endpoint.
func MarshalOTLP(f *wirefmt.File) ([]byte, error) {
	return json.MarshalIndent(exportOTLP(f), "", "  ")
}

func exportOTLP(f *wirefmt.File) otlpExport {
	traceID := traceIDFor(f)
	spans := make([]otlpSpan, 0, len(f.Interactions))
	var startNs int64 // advance along the trace so turns render as a waterfall, not stacked at the epoch
	for i, it := range f.Interactions {
		var sp otlpSpan
		sp, startNs = spanFor(traceID, i, it, startNs)
		spans = append(spans, sp)
	}
	return otlpExport{ResourceSpans: []otlpResourceSpans{{
		Resource:   otlpResource{Attributes: []otlpKV{{Key: "service.name", Value: strVal("cassette")}}},
		ScopeSpans: []otlpScopeSpans{{Scope: otlpScope{Name: "cassette"}, Spans: spans}},
	}}}
}

// spanFor builds the span for one interaction starting at startNs, returning the
// next start offset (start + this turn's duration) so spans chain sequentially.
func spanFor(traceID string, idx int, it *wirefmt.Interaction, startNs int64) (otlpSpan, int64) {
	var durNs int64 // recorded inter-frame timing (when captured with --pace) gives a real duration
	if d := it.Response.StreamTiming; len(d) > 0 {
		var total int64
		for _, ms := range d {
			total += ms
		}
		durNs = total * 1_000_000
	}
	sp := otlpSpan{
		TraceID:           traceID,
		SpanID:            spanIDFor(idx, it),
		Kind:              3, // CLIENT
		StartTimeUnixNano: strconv.FormatInt(startNs, 10),
		EndTimeUnixNano:   strconv.FormatInt(startNs+durNs, 10),
		Status:            otlpStatus{Code: 1},
	}
	if it.Kind == "mcp" {
		sp.Attributes, sp.Name = mcpAttrs(it)
		if mcpIsError(it.Response.Body.Bytes()) {
			sp.Status.Code = 2
		}
	} else {
		sp.Attributes, sp.Name = httpAttrs(it)
		if it.Response.Status >= 400 || it.Response.Error != "" {
			sp.Status.Code = 2
		}
	}
	if d := it.Response.StreamTiming; len(d) > 0 {
		// Non-standard key (the GenAI conventions define no streaming-chunk metric as a
		// span attribute); namespaced under gen_ai.client.operation.* and emitted only
		// when --pace timing was captured.
		sp.Attributes = append(sp.Attributes, otlpKV{
			Key: "gen_ai.client.operation.time_to_first_chunk", Value: dblVal(float64(d[0]) / 1000.0)})
	}
	return sp, startNs + durNs
}

func httpAttrs(it *wirefmt.Interaction) ([]otlpKV, string) {
	provider := ProviderName(wireenc.ProviderForURL(it.Request.URL))
	attrs := []otlpKV{
		{Key: "gen_ai.provider.name", Value: strVal(provider)},
		{Key: "gen_ai.operation.name", Value: strVal("chat")},
	}
	model := RequestModel(it) // body "model", or the Gemini URL-path model
	if model != "" {
		attrs = append(attrs, otlpKV{Key: "gen_ai.request.model", Value: strVal(model)})
	}
	// Gate on Known (not on a >0 count): the recording either carries usage or it
	// doesn't — emitting nothing for "unknown" is correct, but a real recorded 0 is
	// distinct from missing.
	if u := DecodeUsage(it); u.Known {
		attrs = append(attrs,
			otlpKV{Key: "gen_ai.usage.input_tokens", Value: intVal(u.InputTokens)},
			otlpKV{Key: "gen_ai.usage.output_tokens", Value: intVal(u.OutputTokens)})
	}
	if tr, _, _ := DecodeInteraction(it); tr.FinishReason != "" {
		attrs = append(attrs, otlpKV{Key: "gen_ai.response.finish_reasons", Value: arrStr(tr.FinishReason)})
	}
	name := "chat"
	if model != "" {
		name = "chat " + model
	}
	return attrs, name
}

func mcpAttrs(it *wirefmt.Interaction) ([]otlpKV, string) {
	op := "execute_tool"
	if it.Request.MCPMethod == "tools/list" {
		op = "list_tools"
	}
	attrs := []otlpKV{
		{Key: "gen_ai.provider.name", Value: strVal("mcp")},
		{Key: "gen_ai.operation.name", Value: strVal(op)},
		{Key: "mcp.method.name", Value: strVal(it.Request.MCPMethod)},
	}
	name := op
	if it.Request.MCPTool != "" {
		attrs = append(attrs, otlpKV{Key: "mcp.tool.name", Value: strVal(it.Request.MCPTool)})
		name = op + " " + it.Request.MCPTool
	}
	return attrs, name
}

// traceIDFor derives a deterministic 16-byte (32 hex) trace id for the whole
// cassette — one session, one trace.
func traceIDFor(f *wirefmt.File) string {
	var keys []byte
	for _, it := range f.Interactions {
		keys = append(keys, it.Request.MatchKey...)
		keys = append(keys, '\n')
	}
	return canon.SumHex(keys)[:32]
}

// spanIDFor derives a deterministic 8-byte (16 hex) span id per interaction.
func spanIDFor(idx int, it *wirefmt.Interaction) string {
	seed := strconv.Itoa(idx) + ":" + it.Request.MatchKey + it.Request.MCPTool + it.Request.URL
	return canon.SumHex([]byte(seed))[:16]
}
