package main

import (
	"fmt"
	"io"
	"strings"
)

// FlagSet declares a command's accepted flags so one shared parser can replace
// the per-command hand-rolled loops. Bool flags take no value; value flags take
// one (via "--name value" or "--name=value") and may repeat. Unknown flags are a
// usage error. Short aliases (e.g. -o for --out) resolve to the long name.
//
// Output-flag convention (keep new commands consistent):
//   - -o / --out   primary file output (one artifact the command exists to produce)
//   - --report     a SECONDARY file dumped alongside normal stdout (e.g. whatif)
//   - --format X   when output has >2 shapes (table|json|jsonl|csv)
//   - --json       a binary toggle: human text vs one JSON blob
//
// All commands route through parseOrUsage(newFlags()...); none hand-roll arg loops.
type FlagSet struct {
	bools   map[string]bool
	vals    map[string]bool
	aliases map[string]string // short (no dash) -> long
}

func newFlags() *FlagSet {
	return &FlagSet{bools: map[string]bool{}, vals: map[string]bool{}, aliases: map[string]string{}}
}

// boolFlag declares value-less flags (e.g. --strict, --json).
func (fs *FlagSet) boolFlag(names ...string) *FlagSet {
	for _, n := range names {
		fs.bools[n] = true
	}
	return fs
}

// valFlag declares flags that take a value (e.g. --addr :8080).
func (fs *FlagSet) valFlag(names ...string) *FlagSet {
	for _, n := range names {
		fs.vals[n] = true
	}
	return fs
}

// alias maps a short flag (without dash) to a long name (e.g. "o" -> "out").
func (fs *FlagSet) alias(short, long string) *FlagSet {
	fs.aliases[short] = long
	return fs
}

// Inv is the parsed result: positionals plus flag values keyed by long name.
type Inv struct {
	pos  []string
	vals map[string][]string
	set  map[string]bool
}

func (in *Inv) nargs() int     { return len(in.pos) }
func (in *Inv) args() []string { return in.pos }
func (in *Inv) arg(i int) string {
	if i < len(in.pos) {
		return in.pos[i]
	}
	return ""
}
func (in *Inv) has(name string) bool      { return in.set[name] }
func (in *Inv) boolv(name string) bool    { return in.set[name] }
func (in *Inv) list(name string) []string { return in.vals[name] }

// flagSet parses a repeatable, comma-separated flag (e.g. --fail-on a,b --fail-on c)
// into a set, skipping empty tokens. Shared by the --fail-on gates so they all accept
// both repetition and comma lists identically.
func (in *Inv) flagSet(name string) map[string]bool {
	out := map[string]bool{}
	for _, spec := range in.list(name) {
		for _, tok := range strings.Split(spec, ",") {
			if tok = strings.TrimSpace(tok); tok != "" {
				out[tok] = true
			}
		}
	}
	return out
}

// str returns the last value for a flag, or "".
func (in *Inv) str(name string) string {
	v := in.vals[name]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// strOr returns the flag's value if set, else def.
func (in *Inv) strOr(name, def string) string {
	if in.has(name) {
		return in.str(name)
	}
	return def
}

// parse turns args into an Inv against the declared flags. "--" stops flag
// parsing; everything after is positional.
func (fs *FlagSet) parse(args []string) (*Inv, error) {
	in := &Inv{vals: map[string][]string{}, set: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			in.pos = append(in.pos, args[i+1:]...)
			return in, nil
		case strings.HasPrefix(a, "--"):
			name, val, hasEq := strings.Cut(a[2:], "=")
			if err := fs.assign(in, name, val, hasEq, args, &i); err != nil {
				return nil, err
			}
		case len(a) > 1 && a[0] == '-':
			short, val, hasEq := strings.Cut(a[1:], "=")
			long, ok := fs.aliases[short]
			if !ok {
				return nil, fmt.Errorf("unknown flag -%s", short)
			}
			if err := fs.assign(in, long, val, hasEq, args, &i); err != nil {
				return nil, err
			}
		default:
			in.pos = append(in.pos, a)
		}
	}
	return in, nil
}

func (fs *FlagSet) assign(in *Inv, name, val string, hasEq bool, args []string, i *int) error {
	switch {
	case fs.bools[name]:
		if hasEq {
			return fmt.Errorf("flag --%s takes no value", name)
		}
		in.set[name] = true
	case fs.vals[name]:
		if !hasEq {
			if *i+1 >= len(args) {
				return fmt.Errorf("flag --%s needs a value", name)
			}
			*i++
			val = args[*i]
		}
		in.vals[name] = append(in.vals[name], val)
		in.set[name] = true
	default:
		return fmt.Errorf("unknown flag --%s", name)
	}
	return nil
}

// parseOrUsage parses args; on error it prints "cassette: <err>" + the usage line
// and signals the caller to return exitUsage.
func parseOrUsage(fs *FlagSet, args []string, usage string, stderr io.Writer) (*Inv, bool) {
	in, err := fs.parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n%s\n", err, usage)
		return nil, false
	}
	return in, true
}
