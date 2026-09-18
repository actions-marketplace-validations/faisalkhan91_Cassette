package egress

import "testing"

func TestDetectors(t *testing.T) {
	dets := Default()
	cases := map[string]string{
		"email":        "contact jane.doe@example.com please",
		"ssn":          "ssn 123-45-6789 here",
		"jwt":          "token eyJhbGc.eyJzdWIi.SflKxwRJ",
		"aws_key":      "key AKIAIOSFODNN7EXAMPLE end",
		"github_token": "ghp_0123456789abcdefghijklmnopqrstuvwxyz",
		"creditcard":   "card 4111 1111 1111 1111 ok",
	}
	for kind, text := range cases {
		found := false
		for _, f := range Scan([]byte(text), dets) {
			if f.Kind == kind {
				found = true
				if f.Redacted == text || len(f.Redacted) == 0 {
					t.Errorf("%s finding not masked: %q", kind, f.Redacted)
				}
			}
		}
		if !found {
			t.Errorf("expected to detect %s in %q", kind, text)
		}
	}
}

func TestLuhnRejectsInvalidCard(t *testing.T) {
	// 4111...1112 fails Luhn, so it must NOT be reported as a creditcard.
	for _, f := range Scan([]byte("4111 1111 1111 1112"), Default()) {
		if f.Kind == "creditcard" {
			t.Fatal("Luhn-invalid number should not be flagged as a credit card")
		}
	}
}

func TestHighEntropy(t *testing.T) {
	// A long random-looking token trips high_entropy; an English sentence does not.
	hits := Scan([]byte("Zk9Lm2Qp7Xr4Vn8Bt6Wd1Yc3Fg5Hj0Ks"), Default())
	got := false
	for _, f := range hits {
		if f.Kind == "high_entropy" {
			got = true
		}
	}
	if !got {
		t.Fatal("expected high_entropy detection on a long random token")
	}
	for _, f := range Scan([]byte("the quick brown fox jumps over the lazy dog again"), Default()) {
		if f.Kind == "high_entropy" {
			t.Fatal("plain prose should not trip high_entropy")
		}
	}
}

func TestFingerprintDistinguishesMaskCollisions(t *testing.T) {
	// Two distinct emails that mask() collapses to the same sample ("al…om")
	// must still get distinct fingerprints, so taint correlation can't conflate them.
	dets := Default()
	fa := Scan([]byte("alice@x.com"), dets)
	fb := Scan([]byte("alibi@x.com"), dets)
	if len(fa) == 0 || len(fb) == 0 {
		t.Fatal("expected email findings")
	}
	if fa[0].Redacted != fb[0].Redacted {
		t.Skipf("masks differ (%q vs %q) — collision precondition not met", fa[0].Redacted, fb[0].Redacted)
	}
	if fa[0].Fingerprint == fb[0].Fingerprint {
		t.Errorf("distinct secrets share a fingerprint (%s) despite same mask %q", fa[0].Fingerprint, fa[0].Redacted)
	}
	// Same value → same fingerprint (stable).
	if again := Scan([]byte("alice@x.com"), dets); again[0].Fingerprint != fa[0].Fingerprint {
		t.Error("fingerprint not stable for the same value")
	}
}
