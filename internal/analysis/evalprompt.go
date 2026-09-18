package analysis

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/faisalkhan91/cassette/semequal"
)

// JudgePrompt builds the DETERMINISTIC judge request body for evaluating a
// subject transcript against a rubric. Determinism is the whole point: the same
// (rubric, model, turns) always produces byte-identical JSON, so the prompt binds
// to a stored match key and the judge call can be replayed from a fixture with
// zero network. The rendered transcript is the stable, provider-agnostic view of
// the subject (semequal), not raw bytes.
func JudgePrompt(rubric, model string, turns []semequal.Transcript) []byte {
	if model == "" {
		model = "judge"
	}
	var sb strings.Builder
	sb.WriteString(rubric)
	sb.WriteString("\n\nTRANSCRIPT:\n")
	for i, tr := range turns {
		fmt.Fprintf(&sb, "[turn %d]\n", i)
		if tr.Text != "" {
			fmt.Fprintf(&sb, "assistant: %s\n", tr.Text)
		}
		for _, tc := range tr.ToolCalls {
			fmt.Fprintf(&sb, "tool_call: %s(%s)\n", tc.Name, tc.Args)
		}
		if tr.FinishReason != "" {
			fmt.Fprintf(&sb, "finish: %s\n", tr.FinishReason)
		}
	}
	req := struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{Model: model, MaxTokens: 1024}
	req.Messages = append(req.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: sb.String()})
	b, _ := json.Marshal(req)
	return b
}
