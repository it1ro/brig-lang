package repl_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/vm"
)

func TestLoadFileScriptBinds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.brig")
	if err := os.WriteFile(path, []byte("a = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)
	if err := s.LoadFile(path, false); err != nil {
		t.Fatalf("LoadFile: %v\n%s", err, out.String())
	}
	res, err := s.Eval("a\n")
	if err != nil || len(res) == 0 || res[len(res)-1].Value.Inspect() != "1" {
		t.Fatalf("a = %v err %v out %s", res, err, out.String())
	}
}
