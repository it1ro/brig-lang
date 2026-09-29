package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFilePortE2E — `brig <file>` копирует файл через порты File (§12.12):
// чтение порциями по Port.request, запись с Error(:busy) и :port_ready,
// Port.close дописывает принятое до выхода; нет файла — :enoent.
func TestFilePortE2E(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "in.bin")
	dst := filepath.Join(dir, "out.bin")
	data := bytes.Repeat([]byte("0123456789abcdef"), 12500) // 200000 байт
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "main.brig")
	if err := os.WriteFile(prog, []byte(`module Main
fn write_all(out, chunk) ->
    match Port.write(out, chunk)
        Ok(()) -> ()
        Error(:busy) ->
            recv
                (:port_ready, p) when p == out -> write_all(out, chunk)

fn copy(inp, out, n) ->
    Ok(()) = Port.request(inp)
    recv
        (:port_data, p, chunk) when p == inp ->
            write_all(out, chunk)
            copy(inp, out, n + 1)
        (:port_eof, p) when p == inp -> n

fn main() ->
    [src, dst, missing] = Sys.args()
    inp = File.open(src, :read)
    out = File.open(dst, :write)
    n = copy(inp, out, 0)
    Port.close(out)
    bad = File.open(missing, :read)
    Ok(()) = Port.request(bad)
    reason = recv
        (:port_error, p, r) when p == bad -> r
    print((n, reason))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, prog, src, dst, filepath.Join(dir, "nope")).CombinedOutput()
	if err != nil {
		t.Fatalf("brig: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "(4, :enoent)" {
		t.Fatalf("stdout = %q, want (4, :enoent)", got)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("out.bin: %d bytes, want %d", len(got), len(data))
	}
}
