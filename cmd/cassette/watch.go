package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// cmdWatch is the deliberate INVERSE of zero-dial replay: it re-issues each
// recorded request against a LIVE provider and compares the live response to the
// golden recording under a drift Policy. It DIALS THE NETWORK (opt-in via
// --base-url) and never writes the cassette.
func cmdWatch(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("base-url", "policy").boolFlag("json"), args, watchUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() > 1 {
		return exitUsage
	}
	golden := in.arg(0)
	baseURL := in.str("base-url")
	policySpec := in.str("policy")
	asJSON := in.boolv("json")
	if golden == "" || baseURL == "" {
		fmt.Fprintln(stderr, "cassette: watch requires <golden.yaml> and --base-url (it makes LIVE network calls)")
		return watchUsage(stderr)
	}
	f, ok := loadOrErr(golden, stderr)
	if !ok {
		return exitFail
	}
	policy := parsePolicy(policySpec)

	st := newStyle(stderr)
	fmt.Fprintf(stderr, "%s watch DIALS the live provider at %s — this makes real network calls\n", st.yellow("⚠"), baseURL)

	client := &http.Client{Timeout: 60 * time.Second}
	drift := false
	turn := -1
	for _, it := range f.Interactions {
		if it.Kind != "http" {
			continue
		}
		turn++
		want, _, ok := analysis.DecodeInteraction(it)
		if !ok {
			continue
		}
		got, err := liveTranscript(client, baseURL, it)
		so := newStyle(stdout)
		if err != nil {
			fmt.Fprintf(stdout, "%s turn %d: live request failed: %v\n", so.cross(), turn, err)
			drift = true
			continue
		}
		rep := policy.Compare(want, got)
		if rep.OK {
			if asJSON {
				fmt.Fprintf(stdout, `{"turn":%d,"status":"ok"}`+"\n", turn)
			} else {
				fmt.Fprintf(stdout, "%s turn %d: matches golden\n", so.check(), turn)
			}
			continue
		}
		drift = true
		if asJSON {
			fmt.Fprintf(stdout, `{"turn":%d,"status":"drift","fields":%q,"text_distance":%.3f}`+"\n", turn, strings.Join(rep.Fields, ","), rep.TextDist)
		} else {
			fmt.Fprintf(stdout, "%s turn %d: DRIFT on %v (text distance %.3f)\n", so.cross(), turn, rep.Fields, rep.TextDist)
		}
	}
	if drift {
		return exitFail
	}
	return exitOK
}

// liveTranscript re-issues a recorded request live and decodes the response.
func liveTranscript(client *http.Client, baseURL string, it *wirefmt.Interaction) (semequal.Transcript, error) {
	body := it.Request.Body.Bytes()
	req, err := http.NewRequest(it.Request.Method, strings.TrimRight(baseURL, "/")+it.Request.URL, bytes.NewReader(body))
	if err != nil {
		return semequal.Transcript{}, err
	}
	// Re-send recorded headers, skipping scrubbed (REDACTED) values — real auth
	// would be re-injected from the environment by the caller's transport.
	for _, h := range it.Request.Headers {
		for _, v := range h.Values {
			if v == "REDACTED" {
				continue
			}
			req.Header.Add(h.Name, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return semequal.Transcript{}, err
	}
	defer resp.Body.Close()
	live, _ := io.ReadAll(resp.Body)
	// Decode by re-using the engine's provider routing on a synthetic interaction.
	synth := &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{URL: it.Request.URL},
		Response: wirefmt.Response{Streaming: true, Body: wirefmt.NewBody(live)}}
	tr, _, _ := analysis.DecodeInteraction(synth)
	return tr, nil
}

// parsePolicy parses a spec like "prose:0.15" into a drift Policy.
func parsePolicy(spec string) semequal.Policy {
	p := semequal.Policy{}
	for _, part := range strings.Split(spec, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "prose":
			if b, err := strconv.ParseFloat(kv[1], 64); err == nil {
				p.TextBudget = b
				p.TextDistance = lenRatioDistance
			}
		}
	}
	return p
}

// lenRatioDistance is a cheap, dependency-free text distance in [0,1].
func lenRatioDistance(a, b string) float64 {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 && lb == 0 {
		return 0
	}
	d := la - lb
	if d < 0 {
		d = -d
	}
	max := la
	if lb > max {
		max = lb
	}
	return float64(d) / float64(max)
}

const watchUsageText = "usage: cassette watch <golden.yaml> --base-url URL [--policy prose:0.15] [--json]"

func watchUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, watchUsageText)
	return exitUsage
}
