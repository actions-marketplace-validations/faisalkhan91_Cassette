package main

import (
	"bytes"
	"testing"
	"time"
)

// smokeSkip lists commands that are unsafe to invoke with no args in-process:
// they mutate the working tree rather than exiting with a usage error. (Every
// other command — including the servers — returns a usage error promptly when
// given no positional/flags, which is exactly what this test exercises.)
var smokeSkip = map[string]bool{
	"init": true, // scaffolds ./testdata/cassettes and writes ./.gitignore in CWD
	"up":   true, // zero-config front door: creates ./testdata/cassettes and BLOCKS serving
}

// TestAllCommands_NoArgsSmoke invokes every registered command with no arguments
// and asserts it returns a sane exit code promptly without panicking or blocking.
// It is the durable guard against a newly-added command shipping with a 0%-covered
// wrapper (the schema-check gap) or accidentally blocking on no input — a new
// command is covered automatically unless explicitly skipped above.
func TestAllCommands_NoArgsSmoke(t *testing.T) {
	for _, c := range commands {
		if smokeSkip[c.Name] {
			continue
		}
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var out, errb bytes.Buffer
			done := make(chan int, 1)
			go func() { done <- c.Run(nil, &out, &errb) }()
			select {
			case code := <-done:
				switch code {
				case exitOK, exitFail, exitUsage:
					// any of these is a legitimate no-args outcome
				default:
					t.Errorf("%s: no-args exit code %d (want OK/fail/usage)", c.Name, code)
				}
			case <-time.After(15 * time.Second):
				t.Fatalf("%s: did not return on no-args within 15s — blocking? add it to smokeSkip", c.Name)
			}
		})
	}
}
