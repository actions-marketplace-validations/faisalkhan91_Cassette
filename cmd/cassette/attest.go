package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// attestation is the signed artifact: the behavioral manifest plus the public
// key and an ed25519 signature over the canonical manifest JSON.
type attestation struct {
	Manifest  analysis.AttestManifest `json:"manifest"`
	PublicKey string                  `json:"public_key"`
	Signature string                  `json:"signature"`
}

// cmdAttest signs a recording's behavioral manifest. Subcommands:
//
//	cassette attest gen-key <keyfile>
//	cassette attest <cassette.yaml> --key <keyfile> -o <out.att>
func cmdAttest(args []string, stdout, stderr io.Writer) int {
	if len(args) >= 1 && args[0] == "gen-key" {
		if len(args) != 2 {
			return attestUsage(stderr)
		}
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		if err := os.WriteFile(args[1], []byte(hex.EncodeToString(priv)), 0o600); err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		st := newStyle(stdout)
		fmt.Fprintf(stdout, "%s wrote ed25519 private key to %s\n", st.check(), args[1])
		return exitOK
	}

	in, ok := parseOrUsage(newFlags().valFlag("key", "out").alias("o", "out"), args, attestUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() > 1 {
		return exitUsage
	}
	path := in.arg(0)
	keyPath := in.str("key")
	out := in.str("out")
	if path == "" || keyPath == "" || out == "" {
		return attestUsage(stderr)
	}
	priv, err := loadPrivKey(keyPath)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	m := analysis.BuildAttestManifest(f)
	mb, _ := json.Marshal(m)
	att := attestation{
		Manifest:  m,
		PublicKey: hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
		Signature: hex.EncodeToString(ed25519.Sign(priv, mb)),
	}
	ab, _ := json.MarshalIndent(att, "", "  ")
	if err := os.WriteFile(out, ab, 0o644); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s attested %s → %s (%d turns, %d tools)\n", st.check(), path, out, len(m.TurnDigests), len(m.Tools))
	return exitOK
}

// cmdVerifyAttest checks an attestation: the signature is valid AND the cassette
// still has the attested BEHAVIOR (semantic digests match). A benign re-record
// (same behavior, new ids) passes; a behavior change fails.
func cmdVerifyAttest(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		return verifyAttestUsage(stderr)
	}
	f, ok := loadOrErr(args[0], stderr)
	if !ok {
		return exitFail
	}
	raw, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	var att attestation
	if err := json.Unmarshal(raw, &att); err != nil {
		fmt.Fprintf(stderr, "cassette: parse attestation: %v\n", err)
		return exitFail
	}
	st := newStyle(stderr)
	pub, err := hex.DecodeString(att.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		fmt.Fprintf(stderr, "%s cassette: bad public key\n", st.cross())
		return exitFail
	}
	sig, err := hex.DecodeString(att.Signature)
	if err != nil {
		fmt.Fprintf(stderr, "%s cassette: bad signature encoding\n", st.cross())
		return exitFail
	}
	mb, _ := json.Marshal(att.Manifest)
	if !ed25519.Verify(ed25519.PublicKey(pub), mb, sig) {
		fmt.Fprintf(stderr, "%s cassette: signature does not verify (attestation tampered)\n", st.cross())
		return exitFail
	}
	got := analysis.BuildAttestManifest(f)
	if !got.SemanticEqual(att.Manifest) {
		fmt.Fprintf(stderr, "%s cassette: behavior changed since attestation (semantic digests differ)\n", st.cross())
		return exitFail
	}
	so := newStyle(stdout)
	advisory := ""
	if got.CassetteSHA != att.Manifest.CassetteSHA {
		advisory = " (bytes changed but behavior identical — benign re-record)"
	}
	fmt.Fprintf(stdout, "%s attestation valid: signature OK and behavior unchanged%s\n", so.check(), advisory)
	return exitOK
}

func loadPrivKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("key must be a %d-byte ed25519 private key (got %d)", ed25519.PrivateKeySize, len(b))
	}
	return ed25519.PrivateKey(b), nil
}

const attestUsageText = "usage: cassette attest gen-key <keyfile>\n" +
	"       cassette attest <cassette.yaml> --key <keyfile> -o <out.att>"

func attestUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, attestUsageText)
	return exitUsage
}

func verifyAttestUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "usage: cassette verify-attest <cassette.yaml> <out.att>")
	return exitUsage
}
