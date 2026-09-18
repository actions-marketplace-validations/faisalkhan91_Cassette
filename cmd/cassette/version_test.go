package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestCLI_Version(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		code, out, _ := runArgs(args...)
		if code != exitOK {
			t.Fatalf("%v: exit %d", args, code)
		}
		if !strings.Contains(out, "cassette ") || !strings.Contains(out, runtime.GOOS) {
			t.Fatalf("%v: version output %q", args, out)
		}
	}
}
