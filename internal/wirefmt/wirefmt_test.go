package wirefmt

import (
	"bytes"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
)

func sampleFile() *File {
	return &File{
		SchemaVersion: SchemaVersion,
		Notice:        "test fixture",
		Interactions: []*Interaction{
			{
				Kind: "http",
				Request: Request{
					Method:     "POST",
					URL:        "/v1/messages",
					Headers:    HeadersFromHTTP(http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json"}}),
					Body:       NewBody([]byte(`{"max_tokens":5,"model":"claude"}`)),
					BodySHA256: "deadbeef",
				},
				Response: Response{
					Status:    200,
					Headers:   HeadersFromHTTP(http.Header{"Content-Type": {"text/event-stream"}}),
					Streaming: true,
					Body:      NewBody([]byte("event: message_start\ndata: {\"x\":1}\n\nevent: message_stop\ndata: {}\n\n")),
				},
			},
		},
	}
}

func TestDeterministicSerialization(t *testing.T) {
	f := sampleFile()
	a, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("marshal not deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	// Re-marshalling a re-parsed file must also be byte-identical (record twice).
	parsed, err := Unmarshal(a)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, c) {
		t.Fatalf("re-marshal after round-trip differs:\n--- a ---\n%s\n--- c ---\n%s", a, c)
	}
}

func TestRoundTripDeepEqual(t *testing.T) {
	f := sampleFile()
	data, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f, got) {
		t.Fatalf("round-trip not deep-equal:\n want=%#v\n got=%#v", f, got)
	}
}

func TestBodyRoundTrip_BinaryAndNonUTF8(t *testing.T) {
	cases := map[string][]byte{
		"json":           []byte(`{"a":1,"b":"héllo"}`),
		"sse":            []byte("event: x\ndata: 1\n\n"),
		"binary":         {0x00, 0x01, 0x02, 0xff, 0xfe, '\n', 0x7f},
		"invalid-utf8":   {0xc3, 0x28}, // invalid 2-byte sequence
		"trailing-space": []byte("line with trailing space \nnext"),
		"crlf":           []byte("a\r\nb"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			f := &File{
				SchemaVersion: SchemaVersion,
				Interactions: []*Interaction{{
					Kind:     "http",
					Request:  Request{Method: "POST", URL: "/x"},
					Response: Response{Status: 200, Body: NewBody(raw)},
				}},
			}
			data, err := Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Unmarshal(data)
			if err != nil {
				t.Fatal(err)
			}
			gotBody := got.Interactions[0].Response.Body.Bytes()
			if !bytes.Equal(gotBody, raw) {
				t.Fatalf("body not byte-exact after round-trip:\n want=%v\n got=%v", raw, gotBody)
			}
		})
	}
}

func TestSaveLoadAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "c.yaml")
	f := sampleFile()
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f, got) {
		t.Fatalf("save/load not deep-equal")
	}
}

func TestUnmarshalRejectsBadSchemaVersion(t *testing.T) {
	if _, err := Unmarshal([]byte("schema_version: 2\ninteractions: []\n")); err == nil {
		t.Fatal("expected error for schema_version 2")
	}
}

func TestHeadersDeterministicOrder(t *testing.T) {
	h := HeadersFromHTTP(http.Header{"Zeta": {"1"}, "Alpha": {"2"}, "Mike": {"3"}})
	// Marshal whole and check Alpha precedes Mike precedes Zeta.
	data, err := Marshal(&File{SchemaVersion: SchemaVersion, Interactions: []*Interaction{{
		Kind: "http", Request: Request{Headers: h}, Response: Response{Status: 200},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	ia, im, iz := bytes.Index(data, []byte("Alpha")), bytes.Index(data, []byte("Mike")), bytes.Index(data, []byte("Zeta"))
	if !(ia >= 0 && ia < im && im < iz) {
		t.Fatalf("headers not sorted in output:\n%s", s)
	}
}

func TestHeadersGetAndToHTTP(t *testing.T) {
	h := HeadersFromHTTP(http.Header{"X-Api-Key": {"v"}, "Multi": {"a", "b"}})
	if h.Get("x-api-key") != "v" {
		t.Fatalf("Get case-insensitive failed: %q", h.Get("x-api-key"))
	}
	if h.Get("absent") != "" {
		t.Fatal("absent header should return empty")
	}
	hh := h.ToHTTP()
	if len(hh["Multi"]) != 2 {
		t.Fatalf("ToHTTP lost values: %v", hh)
	}
}

func TestNilBodyBytes(t *testing.T) {
	var b *Body
	if b.Bytes() != nil {
		t.Fatal("nil body should return nil bytes")
	}
	if NewBody(nil) != nil || NewBody([]byte{}) != nil {
		t.Fatal("empty body should be nil *Body")
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load("/no/such/cassette.yaml"); err == nil {
		t.Fatal("expected error loading missing file")
	}
	if _, err := Unmarshal([]byte(": not yaml :")); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestEmptyHeadersFromHTTP(t *testing.T) {
	if HeadersFromHTTP(nil) != nil {
		t.Fatal("nil header should map to nil Headers")
	}
}
