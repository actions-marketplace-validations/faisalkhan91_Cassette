package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// Overridden at release time via
// -ldflags "-X main.version=v1.2.3 -X main.commit=<sha> -X main.date=<iso8601>".
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// resolveVersion falls back to the module version the Go toolchain embeds for a
// `go install …@vX.Y.Z` build, so the recommended install path reports the real
// semver instead of "dev" when no ldflags version was injected.
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

func cmdVersion(args []string, stdout, stderr io.Writer) int {
	fmt.Fprintf(stdout, "cassette %s (%s/%s, %s)\n", resolveVersion(), runtime.GOOS, runtime.GOARCH, runtime.Version())
	if commit != "" {
		line := "  commit " + commit
		if date != "" {
			line += " (" + date + ")"
		}
		fmt.Fprintln(stdout, line)
	}
	return exitOK
}
