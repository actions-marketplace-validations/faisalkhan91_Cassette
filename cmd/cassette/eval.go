package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	cassette "github.com/faisalkhan91/cassette"
	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/semequal"
	"gopkg.in/yaml.v3"
)

type evalSuite struct {
	Deterministic struct {
		NoTools          []string `yaml:"no_tools"`
		FinishesClean    bool     `yaml:"finishes_clean"`
		NoDuplicateTools bool     `yaml:"no_duplicate_tools"`
	} `yaml:"deterministic"`
	Judge *struct {
		Rubric     string   `yaml:"rubric"`
		Model      string   `yaml:"model"`
		Cassette   string   `yaml:"cassette"`
		PassMarker string   `yaml:"pass_marker"`
		MinScore   *float64 `yaml:"min_score"` // when set, pass iff the extracted score >= MinScore
	} `yaml:"judge"`
}

// judgeVerdict is a decoded judge reply: its text plus an optional numeric score.
type judgeVerdict struct {
	Text     string
	Score    float64
	HasScore bool
}

// cmdEval runs an assertion suite over a subject recording. Deterministic
// assertions reuse semequal invariants. A judge assertion renders the subject
// into a deterministic prompt and replays it against a JUDGE CASSETTE — so the
// judge model is itself a fixture and the whole eval is zero-dial and
// bit-reproducible run to run.
func cmdEval(args []string, stdout, stderr io.Writer) int {
	inv, ok := parseOrUsage(newFlags().valFlag("suite", "judge"), args, evalUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if inv.nargs() > 1 {
		return exitUsage
	}
	subject := inv.arg(0)
	suitePath := inv.str("suite")
	judgeOverride := inv.str("judge")
	if subject == "" || suitePath == "" {
		return evalUsage(stderr)
	}
	raw, err := os.ReadFile(suitePath)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	var suite evalSuite
	if err := yaml.Unmarshal(raw, &suite); err != nil {
		fmt.Fprintf(stderr, "cassette: parse suite: %v\n", err)
		return exitFail
	}
	f, ok := loadOrErr(subject, stderr)
	if !ok {
		return exitFail
	}
	turns := analysis.CollectTurns(f)
	st := newStyle(stdout)
	failed := false

	// Deterministic assertions.
	var invs []semequal.Invariant
	for _, n := range suite.Deterministic.NoTools {
		invs = append(invs, semequal.NoToolCalled(n))
	}
	if suite.Deterministic.NoDuplicateTools {
		invs = append(invs, semequal.NoDuplicateToolCall())
	}
	if suite.Deterministic.FinishesClean {
		invs = append(invs, semequal.FinishesCleanly())
	}
	for _, e := range semequal.Check(turns, invs...) {
		fmt.Fprintf(stdout, "%s %v\n", st.cross(), e)
		failed = true
	}
	if len(invs) > 0 && !failed {
		fmt.Fprintf(stdout, "%s %d deterministic assertion(s) passed\n", st.check(), len(invs))
	}

	// Judge assertion (judge is itself a replayed fixture).
	if suite.Judge != nil {
		judgePath := suite.Judge.Cassette
		if judgeOverride != "" {
			judgePath = judgeOverride
		}
		if !filepath.IsAbs(judgePath) {
			judgePath = filepath.Join(filepath.Dir(suitePath), judgePath)
		}
		v, err := runJudge(judgePath, suite, turns)
		if err != nil {
			fmt.Fprintf(stderr, "%s judge: %v\n", st.cross(), err)
			return exitFail
		}
		pass := judgePasses(suite.Judge.PassMarker, suite.Judge.MinScore, v)
		switch {
		case suite.Judge.MinScore != nil && v.HasScore:
			fmt.Fprintf(stdout, "%s judge score: %g (min %g)\n", verdictMark(st, pass), v.Score, *suite.Judge.MinScore)
		case suite.Judge.MinScore != nil:
			fmt.Fprintf(stdout, "%s judge: no score found in verdict\n", st.cross())
		default:
			fmt.Fprintf(stdout, "%s judge verdict: %s\n", verdictMark(st, pass), passWord(pass))
		}
		if !pass {
			failed = true
		}
	}

	if failed {
		return exitFail
	}
	return exitOK
}

func runJudge(judgePath string, suite evalSuite, turns []semequal.Transcript) (judgeVerdict, error) {
	c, err := cassette.Open(judgePath, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		return judgeVerdict{}, err
	}
	prompt := analysis.JudgePrompt(suite.Judge.Rubric, suite.Judge.Model, turns)
	resp, err := c.HTTPClient().Post("http://judge.local/v1/messages", "application/json", bytes.NewReader(prompt))
	if err != nil {
		return judgeVerdict{}, fmt.Errorf("replay judge cassette (prompt did not match a recorded judge call): %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// Decode the judge reply to clean text (robust to SSE chunk boundaries).
	tr, _ := semequal.DecodeAnthropicSSE(body)
	text := tr.Text
	if text == "" {
		text = string(body) // fall back to raw bytes if the judge reply isn't a streaming transcript
	}
	v := judgeVerdict{Text: text}
	if s, ok := extractScore(text); ok {
		v.Score, v.HasScore = s, true
	}
	return v, nil
}

var scoreRe = regexp.MustCompile(`(?i)score\s*[:=]?\s*([0-9]+(?:\.[0-9]+)?)`)
var ratioRe = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s*(?:/|out of)\s*[0-9]+`)

// extractScore pulls a deterministic numeric score from a judge verdict: a
// `score: N` field first, else an `N/100`/`N out of 100` ratio.
func extractScore(s string) (float64, bool) {
	if m := scoreRe.FindStringSubmatch(s); m != nil {
		f, _ := strconv.ParseFloat(m[1], 64)
		return f, true
	}
	if m := ratioRe.FindStringSubmatch(s); m != nil {
		f, _ := strconv.ParseFloat(m[1], 64)
		return f, true
	}
	return 0, false
}

// judgePasses applies the scored gate (MinScore) when set, else the substring marker.
func judgePasses(marker string, minScore *float64, v judgeVerdict) bool {
	if minScore != nil {
		return v.HasScore && v.Score >= *minScore
	}
	if marker == "" {
		marker = "PASS"
	}
	return strings.Contains(strings.ToUpper(v.Text), strings.ToUpper(marker))
}

func verdictMark(st style, pass bool) string {
	if pass {
		return st.check()
	}
	return st.cross()
}

func passWord(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

const evalUsageText = "usage: cassette eval <subject.yaml> --suite suite.yaml [--judge judge.yaml]"

func evalUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, evalUsageText)
	return exitUsage
}
