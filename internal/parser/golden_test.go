package parser

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestGolden сверяет Pretty/Format с testdata/golden/*.ast и *.round.brig.
// В golden-кейсах нет комментариев: Format их теряет (в AST их нет).
func TestGolden(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "golden")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".brig") || strings.HasSuffix(e.Name(), ".round.brig") {
			continue
		}
		name := e.Name()
		base := strings.TrimSuffix(name, ".brig")
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}

		prog, err := ParseProgram(ModeModule, string(src))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		checkOrUpdate(t, filepath.Join(dir, base+".ast"), ast.Pretty(prog))
		checkOrUpdate(t, filepath.Join(dir, base+".round.brig"), ast.Format(prog))
	}
}

func checkOrUpdate(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run make update-golden)", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s mismatch:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
