package cassette

import (
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// RekeyPath recomputes every interaction's persisted match key in the cassette at
// path from its stored request under cfg, then writes it back. Use it after editing
// a recording by hand (or after redacting a field), since replay trusts the stored
// key — a stale key silently desyncs replay. It is the public entry point for the
// shared HTTP+MCP key derivation; bodies are left untouched (only keys change).
func RekeyPath(path string, cfg MatchConfig) error {
	f, err := wirefmt.Load(path)
	if err != nil {
		return err
	}
	if err := rekey.File(f, cfg.toInternal()); err != nil {
		return err
	}
	return wirefmt.Save(path, f)
}
