package loader

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(parts ...string) string {
	return filepath.Join(append([]string{"testdata", "modules"}, parts...)...)
}

// moduleIndex — имя модуля → путь файла.
func moduleIndex(g *Graph) map[string]string {
	out := make(map[string]string, len(g.Modules))
	for _, m := range g.Modules {
		out[m.Name] = m.Path
	}
	return out
}

func TestLoadResolvesPathToModule(t *testing.T) {
	g, err := Load(fixture("resolve", "main.brig"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.Root != fixture("resolve") {
		t.Errorf("Root = %q, want %q", g.Root, fixture("resolve"))
	}
	want := map[string]string{
		"Main":        fixture("resolve", "main.brig"),
		"Util":        fixture("resolve", "util.brig"),
		"Http.Client": fixture("resolve", "http", "client.brig"),
	}
	got := moduleIndex(g)
	if len(got) != len(want) {
		t.Fatalf("modules = %v, want %v (Json — встроенный, не загружается; Http.Client — один раз)", got, want)
	}
	for name, path := range want {
		if got[name] != path {
			t.Errorf("module %s: path %q, want %q", name, got[name], path)
		}
	}
	if g.Entry != g.Modules[0] || g.Entry.Name != "Main" {
		t.Errorf("Entry = %+v, want first module Main", g.Entry)
	}
	for _, m := range g.Modules {
		if m.Prog == nil {
			t.Errorf("module %s: Prog == nil", m.Name)
		}
	}
}

func TestLoadExplicitModuleOverridesPath(t *testing.T) {
	g, err := Load(fixture("explicit", "main.brig"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.Entry.Name != "App" {
		t.Errorf("entry name = %q, want App (явный module у входного файла)", g.Entry.Name)
	}
	if _, ok := moduleIndex(g)["Util"]; !ok {
		t.Errorf("module Util not loaded: %v", moduleIndex(g))
	}

	_, err = Load(fixture("mismatch", "main.brig"))
	var le *Error
	if !errors.As(err, &le) {
		t.Fatalf("mismatch: err = %v, want *loader.Error", err)
	}
	if le.File != fixture("mismatch", "main.brig") || le.Line != 1 || le.Col != 1 {
		t.Errorf("mismatch: position %s:%d:%d, want import directive in main.brig at 1:1", le.File, le.Line, le.Col)
	}
	if !strings.Contains(le.Msg, "Util") || !strings.Contains(le.Msg, "Other") {
		t.Errorf("mismatch: msg = %q, want both Util and Other", le.Msg)
	}
}

func TestLoadImportCycle(t *testing.T) {
	g, err := Load(fixture("cycle", "main.brig"))
	if err != nil {
		t.Fatalf("Load: %v (циклы импорта разрешены, T-122 п.2)", err)
	}
	var names []string
	for _, m := range g.Modules {
		names = append(names, m.Name)
	}
	if got := strings.Join(names, ","); got != "Main,A,B" {
		t.Errorf("modules = %s, want Main,A,B (каждый — один раз)", got)
	}
}

func TestLoadMissingModule(t *testing.T) {
	file := fixture("missing", "main.brig")
	_, err := Load(file)
	if err == nil {
		t.Fatal("Load: want error for missing module")
	}
	want := file + ":3:1: module Util not found"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

// T-134 (§11.1): `alias` неизвестного модуля — та же ошибка, что и у
// `import` выше: loader не различает директивы при резолве.
func TestLoadMissingModuleAlias(t *testing.T) {
	file := fixture("missing-alias", "main.brig")
	_, err := Load(file)
	if err == nil {
		t.Fatal("Load: want error for missing aliased module")
	}
	want := file + ":3:1: module Util.Missing not found"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestPathName(t *testing.T) {
	for _, tc := range []struct{ name, rel string }{
		{"Util", "util.brig"},
		{"Http.Client", filepath.Join("http", "client.brig")},
		{"HttpClient", "http_client.brig"},
		{"Http2", "http2.brig"},
	} {
		if got := modulePath(tc.name); got != tc.rel {
			t.Errorf("modulePath(%q) = %q, want %q", tc.name, got, tc.rel)
		}
		if got := pathName(tc.rel); got != tc.name {
			t.Errorf("pathName(%q) = %q, want %q", tc.rel, got, tc.name)
		}
	}
}

func TestProjectRootWalksUp(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "project.brig"), []byte("module Project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ProjectRoot(sub)
	if err != nil || got != root {
		t.Fatalf("ProjectRoot = %q, %v; want %q", got, err, root)
	}
	if _, err := ProjectRoot(t.TempDir()); err == nil {
		t.Fatal("missing project.brig: want error")
	}
}

func TestLoadFromNestedEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "http"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "util.brig"), []byte("module Util\n\nfn n() -> 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "http", "client.brig")
	if err := os.WriteFile(entry, []byte("module Http.Client\n\nimport Util\n\nfn ping() -> Util.n()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := LoadFrom(root, entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := moduleIndex(g)["Util"]; !ok {
		t.Fatalf("modules %v, want Util", moduleIndex(g))
	}
	if _, err := Load(entry); err == nil {
		t.Fatal("Load from the nested directory should not find Util")
	}
}
