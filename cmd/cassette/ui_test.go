package main

import (
	"strings"
	"testing"
)

func TestHumanBytes(t *testing.T) {
	cases := map[int]string{0: "0 B", 512: "512 B", 1000: "1.0 kB", 1536: "1.5 kB", 1500000: "1.5 MB"}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestStyle_ColorOnOff(t *testing.T) {
	plain := style{color: false}
	if plain.green("x") != "x" || plain.bold("y") != "y" || plain.check() != "✓" || plain.cross() != "✗" {
		t.Fatal("plain style must not add ANSI codes")
	}
	col := style{color: true}
	if !strings.Contains(col.green("x"), ansiGreen) || !strings.HasSuffix(col.green("x"), ansiReset) {
		t.Fatalf("color style must wrap with ANSI: %q", col.green("x"))
	}
	if !strings.Contains(col.check(), ansiGreen) || !strings.Contains(col.cross(), ansiRed) {
		t.Fatal("check/cross must be colored when color is on")
	}
}

func TestStatusColor(t *testing.T) {
	col := style{color: true}
	if !strings.Contains(col.statusColor(200), ansiGreen) {
		t.Fatal("2xx should be green")
	}
	if !strings.Contains(col.statusColor(429), ansiRed) {
		t.Fatal("4xx should be red")
	}
	if !strings.Contains(col.statusColor(302), ansiYellow) {
		t.Fatal("3xx should be yellow")
	}
	if got := (style{color: false}).statusColor(0); got != "-" {
		t.Fatalf("status 0 plain = %q, want -", got)
	}
}

func TestColorEnabled_EnvPrecedence(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")
	t.Setenv("FORCE_COLOR", "")
	// A non-*os.File writer (e.g. bytes.Buffer) is not a terminal → no color.
	if colorEnabled(&strings.Builder{}) {
		t.Fatal("non-terminal writer should not be colored by default")
	}
	// FORCE_COLOR / CLICOLOR_FORCE force color on even for a non-terminal.
	t.Setenv("FORCE_COLOR", "1")
	if !colorEnabled(&strings.Builder{}) {
		t.Fatal("FORCE_COLOR should force color on")
	}
	// NO_COLOR wins over FORCE_COLOR.
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(&strings.Builder{}) {
		t.Fatal("NO_COLOR must override FORCE_COLOR")
	}
}

func TestStripNoColor(t *testing.T) {
	defer func() { forceNoColor = false }()
	// --no-color is consumed from anywhere in args and sets the global.
	rest := stripNoColor([]string{"inspect", "--no-color", "x.yaml"})
	if forceNoColor != true {
		t.Fatal("stripNoColor should set forceNoColor")
	}
	if len(rest) != 2 || rest[0] != "inspect" || rest[1] != "x.yaml" {
		t.Fatalf("--no-color not stripped: %v", rest)
	}
	// Absent → cleared, args unchanged.
	rest = stripNoColor([]string{"version"})
	if forceNoColor || len(rest) != 1 {
		t.Fatalf("forceNoColor=%v rest=%v", forceNoColor, rest)
	}
	// The global forces color off regardless of FORCE_COLOR.
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	forceNoColor = true
	if colorEnabled(&strings.Builder{}) {
		t.Fatal("--no-color must override FORCE_COLOR")
	}
}

func TestStyleWarn(t *testing.T) {
	if (style{color: false}).warn() != "!" {
		t.Fatal("plain warn must be a bare !")
	}
	if !strings.Contains((style{color: true}).warn(), ansiYellow) {
		t.Fatal("colored warn must be yellow")
	}
}
