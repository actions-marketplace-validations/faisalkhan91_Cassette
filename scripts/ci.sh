#!/usr/bin/env bash
# The single CI gate. Runs fully offline (no network, no secrets). Every step's
# exit code is checked; the script exits nonzero on the first failure.
#
#   scripts/ci.sh
#
# It is the loop's STOP condition: green here == done.
set -euo pipefail

cd "$(dirname "$0")/.."

# Fully offline + vendored deps. GOPROXY=off proves no module is ever fetched.
export GOFLAGS=-mod=vendor
export GOPROXY=off
# Note: -race (step 7) requires cgo, so CGO is left enabled by default; the
# Windows cross-build (step 6) disables it explicitly to avoid a C toolchain.

COVER_FLOOR=82
PKG_FLOOR=78

step() { printf '\n=== %s ===\n' "$1"; }
have() { command -v "$1" >/dev/null 2>&1; }

step "identity guard (no placeholder module path)"
if grep -rn 'github.com/example/' --exclude-dir=vendor \
	--include='*.go' --include='go.mod' --include='*.yaml' --include='*.yml' .; then
	echo "placeholder path 'github.com/example/' found in code/config — module rename is incomplete"
	exit 1
fi
echo "identity: no placeholder module path"

step "1/9 gofmt"
fmt_out="$(gofmt -l . | grep -v '^vendor/' || true)"
if [ -n "$fmt_out" ]; then
	echo "gofmt found unformatted files:"
	echo "$fmt_out"
	exit 1
fi
echo "gofmt: clean"
if have gofumpt; then
	gofumpt_out="$(gofumpt -l . | grep -v '^vendor/' || true)"
	if [ -n "$gofumpt_out" ]; then
		echo "gofumpt found unformatted files:"; echo "$gofumpt_out"; exit 1
	fi
	echo "gofumpt: clean"
else
	echo "gofumpt: not installed (skipped; see DECISIONS.md)"
fi

step "2/9 go vet"
go vet ./...

step "3/9 go build"
go build ./...
go build -o /tmp/cassette ./cmd/cassette
echo "built ./cmd/cassette -> /tmp/cassette"

step "4/9 linters (optional)"
if have staticcheck; then staticcheck ./...; echo "staticcheck: ok"; else echo "staticcheck: not installed (skipped; see DECISIONS.md)"; fi
if have golangci-lint && [ -f .golangci.yml ]; then golangci-lint run; else echo "golangci-lint: not configured (skipped)"; fi

step "5/9 go.mod tidy + verify (no drift)"
# yaml.v3 has no go directive, so its TEST-only dep gopkg.in/check.v1 is dragged
# into the graph and is not cacheable offline. `tidy -e` (run without vendor mode)
# tidies our module while tolerating that unreachable test-only dep. See DECISIONS.md.
cp go.mod /tmp/go.mod.bak
cp go.sum /tmp/go.sum.bak
GOFLAGS= go mod tidy -e
if ! diff -q /tmp/go.mod.bak go.mod >/dev/null || ! diff -q /tmp/go.sum.bak go.sum >/dev/null; then
	echo "go.mod/go.sum changed after 'go mod tidy -e' (dependency drift):"
	diff -u /tmp/go.mod.bak go.mod || true
	diff -u /tmp/go.sum.bak go.sum || true
	exit 1
fi
go mod verify
echo "modules: tidy and verified"

step "6/9 cross-compile (Windows best-effort)"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
echo "GOOS=windows build: ok"

step "7/9 go test -race -cover"
# The coverage gate measures shippable LOGIC only: data-only / example / fixture
# packages have no statements worth covering and would otherwise dilute the number
# (or, worse, let a real package hide below the floor). They still build, vet, and
# run their tests — they're just excluded from the coverage denominator.
PKGS="$(go list ./... | grep -vE '/internal/wirefix$|/examples/|/cmd/mcpfixture$')"
go test -race -cover -coverprofile=cover.out $PKGS | tee /tmp/cassette-cover.txt
total="$(go tool cover -func=cover.out | awk '/^total:/ {gsub(/%/,"",$3); print $3}')"
echo "total coverage: ${total}% (floor ${COVER_FLOOR}%)"
awk -v t="$total" -v f="$COVER_FLOOR" 'BEGIN { exit !(t+0 >= f+0) }' || {
	echo "coverage ${total}% is below floor ${COVER_FLOOR}%"; exit 1; }
# Per-package floor: no single shippable package may hide below PKG_FLOOR.
awk -v f="$PKG_FLOOR" '/coverage: [0-9.]+% of statements/ {
		c=""; for (i=1;i<=NF;i++) if ($i=="coverage:") { c=$(i+1); gsub(/%/,"",c) }
		if (c != "" && c+0 < f+0) { printf "  %s: %s%% < per-package floor %s%%\n", $2, c, f; bad=1 }
	}
	END { exit bad }' /tmp/cassette-cover.txt || {
	echo "a shippable package is below the per-package floor (${PKG_FLOOR}%)"; exit 1; }
echo "per-package floor ${PKG_FLOOR}%: ok"

step "8/9 secret-scan committed fixtures"
secret_hits=0
while IFS= read -r f; do
	if ! /tmp/cassette verify "$f" >/dev/null 2>&1; then
		echo "secret-scan/verify FAILED: $f"
		/tmp/cassette verify "$f" || true
		secret_hits=1
	fi
done < <(find testdata -type f -name '*.yaml' 2>/dev/null)
# Defense-in-depth raw grep over committed fixtures (cassettes + wire fixtures).
if grep -rnE 'sk-[A-Za-z0-9]|Bearer [A-Za-z0-9]|x-api-key:[[:space:]]*[^[:space:]]' testdata internal/wirefix/wire 2>/dev/null; then
	echo "raw secret pattern found in committed fixtures"
	secret_hits=1
fi
[ "$secret_hits" -eq 0 ] && echo "secret-scan: clean"
[ "$secret_hits" -eq 0 ] || exit 1

# Replay self-check over the known-good corpus (testdata/cassettes/): proves the
# SHIPPED binary replays every committed cassette with zero dials, not just that
# they are secret-free. (The library-level gate is TestCorpus_Conformance.)
conf_hits=0
while IFS= read -r f; do
	if ! /tmp/cassette conformance "$f" >/dev/null 2>&1; then
		echo "conformance FAILED: $f"
		/tmp/cassette conformance "$f" || true
		conf_hits=1
	fi
done < <(find testdata/cassettes -type f -name '*.yaml' 2>/dev/null)
[ "$conf_hits" -eq 0 ] && echo "conformance: corpus replays cleanly"
[ "$conf_hits" -eq 0 ] || exit 1

step "9/9 north-star demos (Anthropic + OpenAI)"
scripts/demo.sh

printf '\n=== CI PASSED (offline) ===\n'
