// Package egress provides typed data-class detectors for auditing the literal
// bytes an agent would have sent on the wire. Unlike scrub (which redacts known
// credential SHAPES) and CanaryHits (which needs the leaked string in advance),
// these detectors find UNKNOWN sensitive data the model emitted — e.g. a user's
// SSN pasted into a tool argument.
package egress

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"strings"
)

// Finding is one detected sensitive span.
type Finding struct {
	Kind     string // detector name, e.g. "email", "creditcard"
	Redacted string // a masked sample, safe to print/commit
	// Fingerprint is a stable, non-reversible id of the RAW matched value, so
	// callers can correlate the same secret across turns without the lossy mask
	// colliding distinct values — and without carrying the secret itself.
	Fingerprint string
}

// fingerprint is a short, non-reversible id of a raw matched value.
func fingerprint(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// Detector classifies sensitive spans of a kind in a byte slice.
type Detector struct {
	Kind string
	Find func(b []byte) []string // returns raw matches
}

var (
	reEmail   = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	reSSN     = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	rePhone   = regexp.MustCompile(`\b\+?\d{1,2}[\s\-.]?\(?\d{3}\)?[\s\-.]?\d{3}[\s\-.]?\d{4}\b`)
	reJWT     = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)
	reAWSKey  = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	reGitHub  = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`)
	reCard    = regexp.MustCompile(`\b(?:\d[ \-]?){13,19}\b`)
	reEntropy = regexp.MustCompile(`[A-Za-z0-9+/_\-]{32,}`)
)

// Default returns the built-in detector bank.
func Default() []Detector {
	return []Detector{
		{"email", reMatch(reEmail)},
		{"ssn", reMatch(reSSN)},
		{"phone", reMatch(rePhone)},
		{"jwt", reMatch(reJWT)},
		{"aws_key", reMatch(reAWSKey)},
		{"github_token", reMatch(reGitHub)},
		{"creditcard", findCard},
		{"high_entropy", findHighEntropy},
	}
}

func reMatch(re *regexp.Regexp) func([]byte) []string {
	return func(b []byte) []string { return re.FindAllString(string(b), -1) }
}

// findCard returns digit sequences that pass the Luhn checksum (real card-like).
func findCard(b []byte) []string {
	var out []string
	for _, m := range reCard.FindAllString(string(b), -1) {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, m)
		if len(digits) >= 13 && len(digits) <= 19 && luhn(digits) {
			out = append(out, m)
		}
	}
	return out
}

func luhn(digits string) bool {
	sum, alt := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

// findHighEntropy flags long base64/hex-ish tokens with high Shannon entropy —
// likely secrets/keys not covered by a named shape. Conservative to limit false
// positives; callers can exclude the "high_entropy" kind via an allowlist.
func findHighEntropy(b []byte) []string {
	var out []string
	for _, m := range reEntropy.FindAllString(string(b), -1) {
		if shannon(m) >= 4.0 {
			out = append(out, m)
		}
	}
	return out
}

func shannon(s string) float64 {
	if s == "" {
		return 0
	}
	freq := map[rune]float64{}
	for _, r := range s {
		freq[r]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range freq {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}

// Scan runs the detectors over b and returns redacted findings.
func Scan(b []byte, dets []Detector) []Finding {
	var out []Finding
	for _, d := range dets {
		for _, m := range d.Find(b) {
			out = append(out, Finding{Kind: d.Kind, Redacted: mask(m), Fingerprint: fingerprint(m)})
		}
	}
	return out
}

// mask shows only the first and last 2 chars, e.g. "jo…om".
func mask(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("•", len(r))
	}
	return string(r[:2]) + "…" + string(r[len(r)-2:])
}
