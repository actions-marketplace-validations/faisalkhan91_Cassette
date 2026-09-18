package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// doctorRequest describes a live request that failed (or might fail) to match.
type doctorRequest struct {
	Method      string          `json:"method"`
	URL         string          `json:"url"`
	ContentType string          `json:"content_type"`
	Body        json.RawMessage `json:"body"`
}

// cmdDoctor diagnoses a replay MISS: it recomputes the match key for a live
// request, and if nothing matches, finds the nearest recorded interaction (same
// method+path, fewest differing request axes) and names the remedy. Fully offline.
func cmdDoctor(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("request"), args, doctorUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	reqPath := in.str("request")
	if path == "" || in.nargs() > 1 || reqPath == "" {
		return doctorUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	raw, err := os.ReadFile(reqPath)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	var dr doctorRequest
	if err := json.Unmarshal(raw, &dr); err != nil {
		fmt.Fprintf(stderr, "cassette: parse %s: %v\n", reqPath, err)
		return exitFail
	}
	st := newStyle(stdout)
	// diagnoseMiss applies the POST / application/json defaults for empty fields.
	matched, summary, detail, err := diagnoseMiss(f, dr.Method, dr.URL, dr.ContentType, dr.Body)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if matched {
		fmt.Fprintf(stdout, "%s %s\n", st.check(), summary)
		return exitOK
	}
	fmt.Fprintf(stdout, "%s %s\n", st.cross(), summary)
	for _, line := range detail {
		if rest, isFix := strings.CutPrefix(line, "fix "); isFix {
			fmt.Fprintf(stdout, "  %s %s\n", st.yellow("fix"), rest)
		} else {
			fmt.Fprintf(stdout, "  %s\n", line)
		}
	}
	return exitFail
}

// diagnoseMiss recomputes the match key for a live request against f and returns a
// human diagnosis: whether it would match, a one-line summary, and (on a miss) the
// nearest recorded turn plus the per-axis remedy. Plain text (no styling) so both
// `cassette doctor` and the live `cassette up` miss-handler render it consistently.
// "fix " is a marker prefix the doctor command turns into a styled "fix" label.
func diagnoseMiss(f *wirefmt.File, method, url, ct string, body []byte) (matched bool, summary string, detail []string, err error) {
	if method == "" {
		method = "POST"
	}
	if ct == "" {
		ct = "application/json"
	}
	liveKey, kerr := match.Key(match.Input{
		Method: method, Path: url, Body: body, ContentType: ct,
		Header: http.Header{"Content-Type": {ct}},
	}, match.Config{})
	if kerr != nil {
		return false, "", nil, kerr
	}
	for i, it := range f.Interactions {
		if it.Kind == "http" && it.Request.MatchKey == liveKey {
			return true, fmt.Sprintf("request matches recorded turn %d — replay would succeed", i), nil, nil
		}
	}
	// Miss: rank same-path candidates by fewest differing axes.
	type cand struct {
		idx  int
		axes []string
	}
	var cands []cand
	for i, it := range f.Interactions {
		if it.Kind != "http" || it.Request.Method != method || it.Request.URL != url {
			continue
		}
		cands = append(cands, cand{i, analysis.AxisDiff(body, it.Request.Body.Bytes())})
	}
	summary = fmt.Sprintf("no recorded interaction matches %s %s", method, url)
	if len(cands) == 0 {
		return false, summary, []string{"no recording on this method+path at all — record it (`cassette up` records on the first live call), or check the URL"}, nil
	}
	sort.Slice(cands, func(i, j int) bool { return len(cands[i].axes) < len(cands[j].axes) })
	near := cands[0]
	detail = append(detail, fmt.Sprintf("nearest: turn %d, differs on: %s", near.idx, axisList(near.axes)))
	for _, ax := range near.axes {
		detail = append(detail, fmt.Sprintf("fix %s — %s", ax, remedyFor(ax)))
	}
	if len(near.axes) == 0 {
		detail = append(detail, "bodies differ only in a volatile/unkeyed field — try `cassette redact-field` or Match.VolatileJSONPaths, then `cassette rekey`")
	}
	return false, summary, detail, nil
}

func axisList(a []string) string {
	if len(a) == 0 {
		return "(only volatile/unkeyed fields)"
	}
	return strings.Join(a, ", ")
}

func remedyFor(axis string) string {
	switch axis {
	case "model":
		return "the model changed — re-record, or collapse it via a Match transform if intended"
	case "messages":
		return "the conversation/prompt differs — this is a genuinely different request; re-record it"
	case "system":
		return "the system prompt changed — re-record"
	case "tools":
		return "the offered tool set changed — re-record"
	case "sampling":
		return "sampling params differ — add Match.VolatileJSONPaths to ignore them, then `cassette rekey`"
	default:
		return "re-record this interaction"
	}
}

const doctorUsageText = "usage: cassette doctor <cassette.yaml> --request req.json"

func doctorUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, doctorUsageText)
	return exitUsage
}
