package cassette

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Fault is one declarative fault applied at response-reconstruction time without
// ever mutating the recorded cassette. A fault with Status>0 is SYNTHETIC: it
// injects a failure response (e.g. 429) and does NOT consume a recorded
// interaction, so the agent's retry can still reach the real recording on a
// later attempt. A fault with Status==0 MODIFIES the consumed recorded response
// (truncate frames / drop the terminal event) to simulate a corrupted stream.
type Fault struct {
	// Method and Path select which requests this fault applies to ("" = any;
	// Path matches by suffix so "/v1/messages" matches a full URL path).
	Method string `yaml:"method,omitempty"`
	Path   string `yaml:"path,omitempty"`
	// Attempt is the 1-based attempt ordinal for the matched key (0 = every attempt).
	Attempt int `yaml:"attempt,omitempty"`
	// Synthetic failure (Status>0):
	Status     int    `yaml:"status,omitempty"`
	RetryAfter string `yaml:"retry_after,omitempty"`
	Body       string `yaml:"body,omitempty"`
	// Modify the real recorded response (Status==0):
	TruncateAfterFrame *int `yaml:"truncate_after_frame,omitempty"`
	DropTerminal       bool `yaml:"drop_terminal,omitempty"`
}

// FaultSet is an ordered list of faults (a *.chaos.yaml overlay).
type FaultSet []Fault

// LoadFaults reads a chaos overlay YAML file (`faults: [...]`).
func LoadFaults(path string) (FaultSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Faults FaultSet `yaml:"faults"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("cassette: parse chaos overlay: %w", err)
	}
	return doc.Faults, nil
}

func (f Fault) synthetic() bool { return f.Status > 0 }

func (f Fault) matches(method, path string, attempt int) bool {
	if f.Method != "" && !strings.EqualFold(f.Method, method) {
		return false
	}
	if f.Path != "" && !strings.HasSuffix(path, f.Path) {
		return false
	}
	return f.Attempt == 0 || f.Attempt == attempt
}

// shadowsEveryAttempt reports whether an unbounded synthetic fault (Status>0,
// Attempt==0) covers method+path. Such a fault injects a failure on every attempt
// and never consumes the recording, so that interaction is intentionally never
// replayed and must not be flagged as unconsumed by Verify.
func (fs FaultSet) shadowsEveryAttempt(method, path string) bool {
	for i := range fs {
		f := &fs[i]
		if f.synthetic() && f.Attempt == 0 && f.matches(method, path, 0) {
			return true
		}
	}
	return false
}

// lookup returns the first matching fault for (method, path, attempt), or nil.
func (fs FaultSet) lookup(method, path string, attempt int) *Fault {
	for i := range fs {
		if fs[i].matches(method, path, attempt) {
			return &fs[i]
		}
	}
	return nil
}

// synthResponse builds the synthetic failure response for a Status>0 fault.
func (f Fault) synthResponse() wirefmt.Response {
	h := http.Header{"Content-Type": {"application/json"}}
	if f.RetryAfter != "" {
		h.Set("Retry-After", f.RetryAfter)
	}
	body := f.Body
	if body == "" {
		body = fmt.Sprintf(`{"error":{"type":"injected_fault","message":"synthetic %d"}}`, f.Status)
	}
	return wirefmt.Response{
		Status:  f.Status,
		Headers: wirefmt.HeadersFromHTTP(h),
		Body:    wirefmt.NewBody([]byte(body)),
	}
}

// applyModify returns a COPY of stored with the modify fault applied (the stored
// response is never mutated).
func applyModify(stored wirefmt.Response, f Fault) wirefmt.Response {
	if f.Status > 0 || (f.TruncateAfterFrame == nil && !f.DropTerminal) {
		return stored
	}
	frames := splitSSEFrames(stored.Body.Bytes())
	if f.TruncateAfterFrame != nil && *f.TruncateAfterFrame >= 0 && *f.TruncateAfterFrame < len(frames) {
		frames = frames[:*f.TruncateAfterFrame]
	}
	if f.DropTerminal && len(frames) > 0 {
		frames = frames[:len(frames)-1]
	}
	var b []byte
	for _, fr := range frames {
		b = append(b, fr...)
	}
	out := stored
	out.Body = wirefmt.NewBody(b)
	out.StreamTiming = nil
	return out
}
