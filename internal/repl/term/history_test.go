package term

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHistoryPersist — T-202: история переживает перезапуск:
// многострочные вводы целиком, без дублей подряд, не больше
// HistoryLimit записей.
func TestHistoryPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "brig", "history")
	h, err := LoadHistory(path)
	if err != nil || h.Len() != 0 {
		t.Fatalf("LoadHistory(missing) = %d entries, %v", h.Len(), err)
	}
	for _, in := range []string{
		"1 + 1\n",
		"1 + 1\n",                                // дубль подряд
		"  \n",                                   // пустой ввод
		"fn f(x) ->\n    \"a\\nb\" ++ x\n    \n", // многострочный, `\n` в строке
		"1 + 1",
		`s = "\\"`,
	} {
		if err := h.Add(in); err != nil {
			t.Fatalf("Add(%q): %v", in, err)
		}
	}
	want := []string{"1 + 1", "fn f(x) ->\n    \"a\\nb\" ++ x", "1 + 1", `s = "\\"`}
	check := func(h *History) {
		t.Helper()
		var got []string
		for i := range h.Len() {
			got = append(got, h.At(i))
		}
		if fmt.Sprintf("%q", got) != fmt.Sprintf("%q", want) {
			t.Errorf("entries = %q, want %q", got, want)
		}
	}
	check(h)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != len(want) {
		t.Errorf("history file has %d lines, want %d:\n%s", n, len(want), data)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("history file mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}

	h, err = LoadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	check(h)
	// Дубль последней записи прошлой сессии не пишется.
	if err := h.Add(`s = "\\"`); err != nil || h.Len() != len(want) {
		t.Errorf("Add(duplicate of last loaded) → %d entries, %v", h.Len(), err)
	}
}

func TestHistoryLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := &History{Path: path}
	for i := range HistoryLimit + 5 {
		if err := h.Add(fmt.Sprintf("x%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if h.Len() != HistoryLimit || h.At(0) != "x5" {
		t.Fatalf("in memory: %d entries, first %q; want %d, x5", h.Len(), h.At(0), HistoryLimit)
	}
	h, err := LoadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.Len() != HistoryLimit || h.At(0) != "x5" || h.At(HistoryLimit-1) != fmt.Sprintf("x%d", HistoryLimit+4) {
		t.Fatalf("loaded: %d entries, first %q", h.Len(), h.At(0))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != HistoryLimit {
		t.Errorf("history file after load has %d lines, want %d", n, HistoryLimit)
	}
}

func TestDefaultHistoryPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if p, err := DefaultHistoryPath(); err != nil || p != "/state/brig/history" {
		t.Errorf("with XDG_STATE_HOME: %q, %v", p, err)
	}
	t.Setenv("HOME", "/home/u")
	for _, xdg := range []string{"", "relative/dir"} {
		t.Setenv("XDG_STATE_HOME", xdg)
		if p, err := DefaultHistoryPath(); err != nil || p != "/home/u/.local/state/brig/history" {
			t.Errorf("XDG_STATE_HOME=%q: %q, %v", xdg, p, err)
		}
	}
}
