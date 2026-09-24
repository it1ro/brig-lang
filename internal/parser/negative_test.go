package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNegative(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "negative")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".brig") {
			continue
		}
		name := e.Name()
		base := strings.TrimSuffix(name, ".brig")

		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		errBytes, err := os.ReadFile(filepath.Join(dir, base+".err"))
		if err != nil {
			t.Fatalf("missing .err for %s", name)
		}
		wantSub := strings.TrimSpace(string(errBytes))

		_, perr := ParseProgram(ModeModule, string(src))
		if perr == nil {
			t.Fatalf("%s: expected parse error", name)
		}
		if !strings.Contains(perr.Error(), wantSub) {
			t.Fatalf("%s: error %q does not contain %q", name, perr, wantSub)
		}
	}
}
