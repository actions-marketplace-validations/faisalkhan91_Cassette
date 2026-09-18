package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/faisalkhan91/cassette/internal/egress"
)

// cmdEgressAudit scans every recorded OUTBOUND request (body, header values, URL
// path) for typed sensitive data classes the agent may have emitted. Reports
// redacted findings; exits nonzero when a finding matches the policy (--fail-on
// kinds, or ANY finding when --fail-on is omitted).
func cmdEgressAudit(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("fail-on"), args, egressUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return egressUsage(stderr)
	}
	failOn := in.flagSet("fail-on")
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}

	dets := egress.Default()
	st := newStyle(stdout)
	type hit struct {
		idx  int
		find egress.Finding
	}
	var hits []hit
	for i, it := range f.Interactions {
		var sb strings.Builder
		sb.WriteString(it.Request.URL)
		sb.WriteByte('\n')
		for _, hf := range it.Request.Headers {
			for _, v := range hf.Values {
				sb.WriteString(v)
				sb.WriteByte('\n')
			}
		}
		sb.Write(it.Request.Body.Bytes())
		for _, fnd := range egress.Scan([]byte(sb.String()), dets) {
			hits = append(hits, hit{i, fnd})
		}
	}

	if len(hits) == 0 {
		fmt.Fprintf(stdout, "%s no sensitive data classes detected in outbound requests\n", st.check())
		return exitOK
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].idx != hits[j].idx {
			return hits[i].idx < hits[j].idx
		}
		return hits[i].find.Kind < hits[j].find.Kind
	})
	failed := false
	for _, h := range hits {
		gate := len(failOn) == 0 || failOn[h.find.Kind]
		marker := st.yellow("•")
		if gate {
			marker = st.red("✗")
			failed = true
		}
		fmt.Fprintf(stdout, "%s turn %d  %s  %s\n", marker, h.idx, h.find.Kind, h.find.Redacted)
	}
	fmt.Fprintf(stdout, "\nquery strings are not part of the recording (path-only match key) and are out of scope.\n")
	if failed {
		return exitFail
	}
	return exitOK
}

const egressUsageText = "usage: cassette egress-audit <cassette.yaml> [--fail-on email,creditcard,...]"

func egressUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, egressUsageText)
	return exitUsage
}
