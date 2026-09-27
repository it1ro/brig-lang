package repl_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/vm"
)

var update = flag.Bool("update", false, "rewrite golden REPL sessions")

// goldenSep отделяет в testdata/repl/*.txt ввод сессии от ожидаемого
// вывода plain-фронтенда (значения и ошибки одним потоком).
const goldenSep = "-- output --\n"

// TestReplGoldenSessions — T-201: ввод из testdata/repl/*.txt через
// plain-фронтенд (без приглашений) → ожидаемый вывод.
func TestReplGoldenSessions(t *testing.T) {
	files, err := filepath.Glob("../../testdata/repl/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden sessions: %v", err)
	}
	for _, path := range files {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".txt"), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			input, want, ok := strings.Cut(string(data), goldenSep)
			if !ok {
				t.Fatalf("%s: no %q separator", path, strings.TrimSpace(goldenSep))
			}
			var out bytes.Buffer
			fe := repl.Plain{In: strings.NewReader(input), Out: &out, Err: &out}
			s := repl.New(vm.New(), &out)
			t.Cleanup(s.Close)
			if err := fe.Run(s); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if *update {
				if err := os.WriteFile(path, []byte(input+goldenSep+out.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if out.String() != want {
				t.Errorf("%s: output mismatch\n--- got ---\n%s--- want ---\n%s", path, out.String(), want)
			}
		})
	}
}
