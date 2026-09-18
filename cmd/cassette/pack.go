package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	cassette "github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// index.html is identical across every packed binary, so it is baked into the
// cassette binary itself rather than re-stored in each packed payload.
//
//go:embed assets/index.html
var indexHTML []byte

// --- self-extracting binary format -----------------------------------------
//
// A packed binary is the running cassette executable with a payload appended and
// a fixed 24-byte trailer at EOF:
//
//	[ host binary (unmodified) ][ payload (N) ][ uint64 LE N (8) ][ magic (16) ]
//
// Appended bytes sit beyond the Go linker's signed code region, so the kernel
// still runs the binary (verified on darwin/arm64 + linux); `codesign -v` reports
// it invalid. This needs no Go toolchain at pack time — it is pure file I/O.

const (
	trailerMagicLen = 16
	trailerLenSize  = 8
	trailerSize     = trailerLenSize + trailerMagicLen // 24
)

var trailerMagic = [trailerMagicLen]byte{'C', 'A', 'S', 'S', 'E', 'T', 'T', 'E', 'P', 'A', 'C', 'K', 0, 'v', '0', '1'}

type bundleFile struct {
	Name string
	Data []byte
}

// encodeBundle serializes files into a deterministic, timestamp-free container:
// [uint32 count] then per file [uint16 nameLen][name][uint64 dataLen][data].
func encodeBundle(files []bundleFile) []byte {
	var b bytes.Buffer
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], uint32(len(files)))
	b.Write(u32[:])
	for _, f := range files {
		var u16 [2]byte
		binary.LittleEndian.PutUint16(u16[:], uint16(len(f.Name)))
		b.Write(u16[:])
		b.WriteString(f.Name)
		var u64 [8]byte
		binary.LittleEndian.PutUint64(u64[:], uint64(len(f.Data)))
		b.Write(u64[:])
		b.Write(f.Data)
	}
	return b.Bytes()
}

// decodeBundle parses encodeBundle's framing, bounds-checking every length so
// garbage input returns an error rather than panicking or over-allocating.
func decodeBundle(b []byte) ([]bundleFile, error) {
	r := bytes.NewReader(b)
	var u32 [4]byte
	if _, err := io.ReadFull(r, u32[:]); err != nil {
		return nil, fmt.Errorf("bundle: %w", err)
	}
	n := binary.LittleEndian.Uint32(u32[:])
	if n > 1<<20 {
		return nil, errors.New("bundle: implausible file count")
	}
	files := make([]bundleFile, 0, n)
	for i := uint32(0); i < n; i++ {
		var u16 [2]byte
		if _, err := io.ReadFull(r, u16[:]); err != nil {
			return nil, fmt.Errorf("bundle: %w", err)
		}
		name := make([]byte, binary.LittleEndian.Uint16(u16[:]))
		if _, err := io.ReadFull(r, name); err != nil {
			return nil, fmt.Errorf("bundle: %w", err)
		}
		var u64 [8]byte
		if _, err := io.ReadFull(r, u64[:]); err != nil {
			return nil, fmt.Errorf("bundle: %w", err)
		}
		dataLen := binary.LittleEndian.Uint64(u64[:])
		if dataLen > uint64(r.Len()) {
			return nil, errors.New("bundle: data length overruns buffer")
		}
		data := make([]byte, dataLen)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("bundle: %w", err)
		}
		files = append(files, bundleFile{Name: string(name), Data: data})
	}
	return files, nil
}

func writeTrailer(w io.Writer, payload []byte) error {
	if _, err := w.Write(payload); err != nil {
		return err
	}
	var u64 [8]byte
	binary.LittleEndian.PutUint64(u64[:], uint64(len(payload)))
	if _, err := w.Write(u64[:]); err != nil {
		return err
	}
	_, err := w.Write(trailerMagic[:])
	return err
}

// readTrailerAt reads the appended payload from a ReaderAt of total length size.
// A file with no trailer returns (nil,false,nil) — normal CLI mode, no error. A
// matching magic with a length that overruns the file returns an error.
func readTrailerAt(r io.ReaderAt, size int64) ([]byte, bool, error) {
	if size < trailerSize {
		return nil, false, nil
	}
	tail := make([]byte, trailerSize)
	if _, err := r.ReadAt(tail, size-trailerSize); err != nil {
		return nil, false, err
	}
	if !bytes.Equal(tail[trailerLenSize:], trailerMagic[:]) {
		return nil, false, nil
	}
	n := int64(binary.LittleEndian.Uint64(tail[:trailerLenSize]))
	payloadStart := size - trailerSize - n
	if n < 0 || payloadStart < 0 {
		return nil, false, errors.New("corrupt cassette-pack trailer")
	}
	payload := make([]byte, n)
	if _, err := r.ReadAt(payload, payloadStart); err != nil {
		return nil, false, err
	}
	return payload, true, nil
}

// readTrailer resolves the running executable and reads its appended payload.
// Any failure to inspect the executable resolves to plain mode (no error), so a
// normal CLI invocation is never blocked.
func readTrailer() ([]byte, bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, false, nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	f, err := os.Open(exe)
	if err != nil {
		return nil, false, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false, nil
	}
	return readTrailerAt(f, st.Size())
}

// writePacked copies the host binary's original bytes to out, then appends
// payload + trailer, atomically (temp + rename) and executable. If hostPath is
// itself already packed, only its original prefix is copied (re-base, not nest).
func writePacked(hostPath, out string, payload []byte) error {
	host, err := os.Open(hostPath)
	if err != nil {
		return err
	}
	defer host.Close()
	st, err := host.Stat()
	if err != nil {
		return err
	}
	prefix := st.Size()
	if _, ok, _ := readTrailerAt(host, prefix); ok {
		tail := make([]byte, trailerSize)
		if _, err := host.ReadAt(tail, prefix-trailerSize); err != nil {
			return err
		}
		prefix = prefix - trailerSize - int64(binary.LittleEndian.Uint64(tail[:trailerLenSize]))
	}

	tmp := out + ".tmp"
	w, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	fail := func(e error) error { w.Close(); os.Remove(tmp); return e }
	if _, err := io.Copy(w, io.NewSectionReader(host, 0, prefix)); err != nil {
		return fail(err)
	}
	if err := writeTrailer(w, payload); err != nil {
		return fail(err)
	}
	if err := w.Sync(); err != nil {
		return fail(err)
	}
	if err := w.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}

// --- transcript projection + serving ----------------------------------------

// packTurn is the presentation view-model the embedded UI consumes (one entry
// per recorded interaction). All fields are optional; the page branches on kind.
type packTurn struct {
	Index        int                 `json:"index"`
	Kind         string              `json:"kind"`
	Method       string              `json:"method,omitempty"`
	URL          string              `json:"url,omitempty"`
	Status       int                 `json:"status,omitempty"`
	Tool         string              `json:"tool,omitempty"`
	Role         string              `json:"role,omitempty"`
	Text         string              `json:"text,omitempty"`
	ToolCalls    []semequal.ToolCall `json:"tool_calls,omitempty"`
	FinishReason string              `json:"finish_reason,omitempty"`
}

// renderTranscriptJSON projects a cassette into the UI view-model, reusing the
// same decodeTranscript path as `cassette doc` so it can never drift.
func renderTranscriptJSON(f *wirefmt.File) []byte {
	turns := make([]packTurn, 0, len(f.Interactions))
	for i, it := range f.Interactions {
		pt := packTurn{Index: i, Kind: it.Kind}
		switch it.Kind {
		case "mcp":
			pt.Method = it.Request.MCPMethod
			pt.Tool = it.Request.MCPTool
			if body := it.Response.Body.Bytes(); len(body) > 0 {
				pt.Text = string(body)
			}
		default:
			pt.Method = it.Request.Method
			pt.URL = it.Request.URL
			pt.Status = it.Response.Status
			if tr, ok := decodeTranscript(it); ok {
				pt.Role = tr.Role
				pt.Text = tr.Text
				pt.ToolCalls = tr.ToolCalls
				pt.FinishReason = tr.FinishReason
			}
		}
		turns = append(turns, pt)
	}
	b, _ := json.MarshalIndent(turns, "", "  ")
	return b
}

// servePackedMux mounts the steppable UI (GET /), the transcript view-model
// (GET /transcript.json), and the G1 serve API (every other path) on one mux.
// The exact routes win over the provider matcher, which 404s unknown paths.
func servePackedMux(c *cassette.Cassette, f *wirefmt.File) http.Handler {
	api := c.Handler(cassette.ServeOptions{})
	transcript := renderTranscriptJSON(f)
	mux := http.NewServeMux()
	mux.HandleFunc("/transcript.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(transcript)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(indexHTML)
			return
		}
		api.ServeHTTP(w, r)
	})
	return mux
}

// setupPacked decodes the embedded recording and binds a listener + handler,
// returning a ready-to-serve http.Server and the listener. It performs all the
// fallible work (decode, open, listen) so it is unit-testable without entering
// the blocking serve loop; on any failure it returns a nil server and an exit
// code. runPacked wraps it with the signal-driven serve loop.
func setupPacked(payload []byte, derr error, args []string, stdout, stderr io.Writer) (http.Handler, net.Listener, int) {
	if derr != nil {
		fmt.Fprintf(stderr, "cassette: corrupt pack: %v\n", derr)
		return nil, nil, exitFail
	}
	files, err := decodeBundle(payload)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: corrupt pack: %v\n", err)
		return nil, nil, exitFail
	}
	var raw []byte
	for _, bf := range files {
		if bf.Name == "cassette.yaml" {
			raw = bf.Data
		}
	}
	if raw == nil {
		fmt.Fprintln(stderr, "cassette: pack is missing cassette.yaml")
		return nil, nil, exitFail
	}
	c, err := cassette.OpenBytes("packed", raw, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return nil, nil, exitFail
	}
	f, err := wirefmt.Unmarshal(raw)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return nil, nil, exitFail
	}

	addr := "127.0.0.1:7070"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--addr":
			if i+1 < len(args) {
				addr = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--addr="):
			addr = strings.TrimPrefix(a, "--addr=")
		}
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: listen %s: %v\n", addr, err)
		return nil, nil, exitFail
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s cassette packed demo serving on http://%s\n", st.check(), ln.Addr())
	return servePackedMux(c, f), ln, exitOK
}

// runPacked is the entrypoint when the binary detects it is packed: it serves the
// embedded recording offline until interrupted. It ignores subcommands (the
// binary IS the product), accepting only an optional --addr.
func runPacked(payload []byte, derr error, args []string, stdout, stderr io.Writer) int {
	handler, ln, code := setupPacked(payload, derr, args, stdout, stderr)
	if handler == nil {
		return code
	}
	if err := serveUntilSignal(ln, handler); err != nil {
		fmt.Fprintf(stderr, "cassette: serve: %v\n", err)
		return exitFail
	}
	return exitOK
}

// cmdPack appends the cassette to a copy of the running binary, producing one
// self-contained offline demo executable.
func cmdPack(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("out").alias("o", "out"), args, packUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() > 1 {
		return exitUsage
	}
	path := in.arg(0)
	out := in.str("out")
	if path == "" || out == "" {
		return packUsage(stderr)
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintln(stderr, "cassette: pack is unsupported on Windows (PE binaries reject appended payloads)")
		return exitFail
	}

	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	raw, err := wirefmt.Marshal(f)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	st := newStyle(stderr)
	if hits := scrub.SecretScan(raw); len(hits) > 0 {
		fmt.Fprintf(stderr, "%s cassette: refusing to pack — secret pattern(s) present: %v\n", st.cross(), hits)
		return exitFail
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	payload := encodeBundle([]bundleFile{{Name: "cassette.yaml", Data: raw}})
	if err := writePacked(exe, out, payload); err != nil {
		fmt.Fprintf(stderr, "cassette: pack: %v\n", err)
		return exitFail
	}
	so := newStyle(stdout)
	if info, err := os.Stat(out); err == nil {
		fmt.Fprintf(stdout, "%s packed %s (%s) — run ./%s to serve offline\n",
			so.check(), so.bold(out), humanBytes(int(info.Size())), filepath.Base(out))
	} else {
		fmt.Fprintf(stdout, "%s packed %s\n", so.check(), so.bold(out))
	}
	return exitOK
}

const packUsageText = "usage: cassette pack <cassette.yaml> -o <output-binary>"

func packUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, packUsageText)
	return exitUsage
}
