package canon

import (
	"bytes"
	"testing"
)

func TestCanonicalize_SortsKeysStable(t *testing.T) {
	a, ok := Canonicalize([]byte(`{"b":1,"a":2}`))
	if !ok {
		t.Fatal("expected valid JSON")
	}
	b, ok := Canonicalize([]byte(`{"a":2,"b":1}`))
	if !ok {
		t.Fatal("expected valid JSON")
	}
	if string(a) != string(b) {
		t.Fatalf("key order must not matter: %q vs %q", a, b)
	}
	if string(a) != `{"a":2,"b":1}` {
		t.Fatalf("unexpected canonical form: %q", a)
	}
}

func TestCanonicalize_PreservesNumberText(t *testing.T) {
	c, _ := Canonicalize([]byte(`{"x": 1.0, "y": 1, "big": 100000000000000000000}`))
	got := string(c)
	want := `{"big":100000000000000000000,"x":1.0,"y":1}`
	if got != want {
		t.Fatalf("number text not preserved:\n got=%s\n want=%s", got, want)
	}
}

func TestCanonicalize_NoUnnecessaryEscapes(t *testing.T) {
	c, _ := Canonicalize([]byte(`{"s":"a<b>c&d é"}`))
	want := `{"s":"a<b>c&d é"}`
	if string(c) != want {
		t.Fatalf("HTML escaping should be disabled:\n got=%s\n want=%s", c, want)
	}
}

func TestCanonicalize_RejectsNonJSON(t *testing.T) {
	if _, ok := Canonicalize([]byte("not json")); ok {
		t.Fatal("expected non-JSON to be rejected")
	}
	if _, ok := Canonicalize([]byte(`{"a":1}{"b":2}`)); ok {
		t.Fatal("expected trailing token to be rejected")
	}
}

func TestCanonicalizeDropping(t *testing.T) {
	c, ok := CanonicalizeDropping([]byte(`{"a":1,"meta":{"ts":"x","keep":2}}`), []string{"meta.ts", "missing.path"})
	if !ok {
		t.Fatal("expected valid JSON")
	}
	want := `{"a":1,"meta":{"keep":2}}`
	if string(c) != want {
		t.Fatalf("drop path failed:\n got=%s\n want=%s", c, want)
	}
}

func TestSumHexStable(t *testing.T) {
	// Golden vector: SHA-256("abc"). Pins the exact digest, not just self-equality.
	const abc = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := SumHex([]byte("abc")); got != abc {
		t.Fatalf("SumHex(abc) = %s, want %s", got, abc)
	}
	if SumHex([]byte("abc")) == SumHex([]byte("abd")) {
		t.Fatal("different inputs must differ")
	}
}

func TestCanonicalizeDropping_ArrayPath(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"SECRET","role":"assistant"}}]}`)
	a, ok := CanonicalizeDropping(body, []string{"choices.0.message.content"})
	if !ok {
		t.Fatal("expected valid JSON")
	}
	// Same structure with a DIFFERENT content value must canonicalize identically
	// once the array-indexed path is dropped.
	body2 := []byte(`{"choices":[{"message":{"content":"DIFFERENT","role":"assistant"}}]}`)
	b, _ := CanonicalizeDropping(body2, []string{"choices.0.message.content"})
	if string(a) != string(b) {
		t.Fatalf("array-path drop must make differing values match:\n a=%s\n b=%s", a, b)
	}
	if bytes.Contains(a, []byte("SECRET")) {
		t.Fatalf("dropped array path still present: %s", a)
	}
}

func TestDigest(t *testing.T) {
	// Valid JSON: digest is canonical (key order independent).
	if Digest([]byte(`{"b":2,"a":1}`)) != Digest([]byte(`{"a":1,"b":2}`)) {
		t.Error("Digest should canonicalize JSON before hashing")
	}
	// Non-JSON: falls back to a raw-bytes hash (stable, non-empty).
	raw := Digest([]byte("not json\x00\xff"))
	if raw == "" || raw == Digest([]byte("different")) {
		t.Errorf("raw-bytes digest unstable or empty: %q", raw)
	}
}
