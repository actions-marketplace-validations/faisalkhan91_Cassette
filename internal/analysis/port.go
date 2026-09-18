package analysis

import (
	"fmt"

	"github.com/tidwall/gjson"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Port re-encodes a recording's streaming turns from their source dialect into the
// target dialect `to` (Anthropic Messages ⇄ OpenAI Chat ⇄ OpenAI Responses) by
// decode → Transcript → encode, and re-shapes each request (path + body) into the
// target dialect so a client built for `to` can replay the SAME recorded behavior
// offline. The per-turn semantic digest is preserved (decode of the ported
// response equals decode of the original). This is NOT a claim of model
// equivalence across providers — it re-targets the wire dialect of a recording,
// nothing more.
//
// Non-streaming (unary) responses, error streams, and non-http interactions cannot
// have their response dialect transpiled and are copied through unchanged. Returns
// the ported file and the number of turns actually transpiled.
func Port(f *wirefmt.File, to wireenc.Provider) (*wirefmt.File, int, error) {
	out := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Notice: f.Notice, Expect: f.Expect}
	ported := 0
	for i, it := range f.Interactions {
		if it.Kind != "http" {
			out.Interactions = append(out.Interactions, it)
			continue
		}
		src := wireenc.ProviderForURL(it.Request.URL)
		_, unary, renderable := DecodeInteraction(it)
		if src == to || unary || !renderable {
			out.Interactions = append(out.Interactions, it) // nothing to transpile
			continue
		}
		tr, err := wireenc.Decode(src, it.Response.Body.Bytes())
		if err != nil {
			out.Interactions = append(out.Interactions, it)
			continue
		}
		env := wireenc.Envelope{MessageID: fmt.Sprintf("msg_ported_%d", i+1), ModelID: portModel(it.Request.Body.Bytes())}
		newResp := wireenc.Encode(to, tr, env)

		model, system, msgs := wireenc.DecodeRequest(src, it.Request.Body.Bytes())
		newBody := wireenc.EncodeRequest(to, model, system, msgs)
		newURL := wireenc.RequestPath(to)
		key, err := match.Key(match.Input{Method: it.Request.Method, Path: newURL, Body: newBody, ContentType: "application/json"}, match.Config{})
		if err != nil {
			return nil, 0, fmt.Errorf("port turn %d: %w", i, err)
		}

		out.Interactions = append(out.Interactions, &wirefmt.Interaction{
			Kind: "http",
			Request: wirefmt.Request{
				Method:   it.Request.Method,
				URL:      newURL,
				Headers:  wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}},
				Body:     wirefmt.NewBody(newBody),
				MatchKey: key,
			},
			Response: wirefmt.Response{
				Status:    200,
				Headers:   wirefmt.Headers{{Name: "Content-Type", Values: []string{"text/event-stream"}}},
				Streaming: true,
				Body:      wirefmt.NewBody(newResp),
			},
		})
		ported++
	}
	return out, ported, nil
}

func portModel(reqBody []byte) string {
	if m := gjson.GetBytes(reqBody, "model").String(); m != "" {
		return m
	}
	return "model"
}
