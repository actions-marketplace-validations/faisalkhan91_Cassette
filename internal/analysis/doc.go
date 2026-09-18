// Package analysis is the inspection AND pure-transformation toolkit over recorded
// cassettes: decoding interactions into provider-agnostic transcripts, diffing and
// bisecting recordings, projecting token usage and sampling-determinism, classifying
// refusals, building signed behavioral manifests, and rendering deterministic judge
// prompts — plus the write-side transforms that produce or rewrite recordings
// (Compile, Port, Merge, Stitch/Split, CanonicalizeFile) backing the CLI's authoring
// commands.
//
// It depends only on the wire format and the semantic-equivalence layer (never on
// the record/replay runtime in the root cassette package), so importing it never
// pulls in an HTTP transport. The root cassette package stays a small, stable VCR
// surface (Open/New/Transport/Options/Verify/serve); the analysis toolkit lives
// here so a library consumer can choose exactly what they need.
package analysis
