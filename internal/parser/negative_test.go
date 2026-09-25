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

// S-F5: блок после `ensure expr` раньше молча выбрасывался, а блочная
// форма `ensure NEWLINE INDENT` падала с невнятной ошибкой.
func TestAuditEnsureHybridRejected(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub []string
	}{
		{
			name: "hybrid",
			src: `fn main() ->
    result = trap
        ensure print("cleanup")
            print("dropped_block_stmt")
        :ok
    print(result)
`,
			wantSub: []string{"ensure"},
		},
		{
			name: "block",
			src: `fn main() ->
    result = trap
        ensure
            print("cleanup")
        :ok
    print(result)
`,
			wantSub: []string{"ensure", "MVP"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseProgram(ModeModule, c.src)
			if err == nil {
				t.Fatalf("expected parse error")
			}
			for _, sub := range c.wantSub {
				if !strings.Contains(err.Error(), sub) {
					t.Fatalf("error %q does not contain %q", err, sub)
				}
			}
		})
	}

	inline := `fn main() ->
    result = trap
        ensure print("cleanup")
        :ok
    print(result)
`
	if _, err := ParseProgram(ModeModule, inline); err != nil {
		t.Fatalf("inline ensure: unexpected error: %v", err)
	}
}
