package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// style renders optional ANSI color. Color is on when writing to a real terminal,
// or when forced via CLICOLOR_FORCE / FORCE_COLOR (handy for piping into a pager
// or a recorder); NO_COLOR disables it and wins over everything. Otherwise
// piped/redirected/test output stays plain and byte-stable.
type style struct{ color bool }

func newStyle(w io.Writer) style { return style{color: colorEnabled(w)} }

// forceNoColor is set by the global --no-color flag (parsed in run()), for users
// who can't set NO_COLOR in the environment (Makefile recipes, some CI).
var forceNoColor bool

func colorEnabled(w io.Writer) bool {
	if forceNoColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" || os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	// Legacy Windows consoles garble raw ANSI; only auto-color inside Windows
	// Terminal (WT_SESSION). Explicit FORCE_COLOR above still wins.
	if runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

func (s style) wrap(code, text string) string {
	if !s.color {
		return text
	}
	return code + text + ansiReset
}

func (s style) bold(t string) string   { return s.wrap(ansiBold, t) }
func (s style) dim(t string) string    { return s.wrap(ansiDim, t) }
func (s style) green(t string) string  { return s.wrap(ansiGreen, t) }
func (s style) red(t string) string    { return s.wrap(ansiRed, t) }
func (s style) yellow(t string) string { return s.wrap(ansiYellow, t) }
func (s style) cyan(t string) string   { return s.wrap(ansiCyan, t) }

func (s style) check() string { return s.green(glyph("✓", "[ok]")) }
func (s style) cross() string { return s.red(glyph("✗", "[x]")) }
func (s style) warn() string  { return s.yellow(glyph("!", "!")) }

// glyph returns the Unicode form normally, or the ASCII fallback when CASSETTE_ASCII
// is set or the locale is not UTF-8 — so a single switch downgrades every symbol.
func glyph(unicode, ascii string) string {
	if asciiOnly() {
		return ascii
	}
	return unicode
}

func asciiOnly() bool {
	if os.Getenv("CASSETTE_ASCII") != "" {
		return true
	}
	loc := os.Getenv("LC_ALL")
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	if loc == "" {
		return false // unknown locale: assume a modern UTF-8 terminal
	}
	u := strings.ToUpper(loc)
	return !strings.Contains(u, "UTF-8") && !strings.Contains(u, "UTF8")
}

// statusColor colors an HTTP status by class (2xx green, 4xx/5xx red, else dim).
func (s style) statusColor(code int) string {
	txt := fmt.Sprintf("%d", code)
	switch {
	case code >= 200 && code < 300:
		return s.green(txt)
	case code >= 400:
		return s.red(txt)
	case code == 0:
		return s.dim("-")
	default:
		return s.yellow(txt)
	}
}

// humanBytes renders a byte count compactly (e.g. 1.0 kB).
func humanBytes(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	const units = "kMGT"
	f := float64(n)
	i := -1
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	return fmt.Sprintf("%.1f %cB", f, units[i])
}
