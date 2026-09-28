package repl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/it1ro/brig-lang/internal/repl"
)

func caret(t *testing.T, marked string) (string, int) {
	t.Helper()
	i := strings.IndexByte(marked, '|')
	if i < 0 {
		t.Fatalf("no caret in %q", marked)
	}
	src := marked[:i] + marked[i+1:]
	return src, utf8.RuneCountInString(marked[:i])
}

func at(t *testing.T, s *repl.Session, marked string) repl.Completion {
	t.Helper()
	src, pos := caret(t, marked)
	return s.Complete(src, pos)
}

func one(t *testing.T, c repl.Completion, insert string) repl.Candidate {
	t.Helper()
	var got []string
	var found repl.Candidate
	n := 0
	for _, cand := range c.Candidates {
		if cand.Insert == insert {
			n++
			found = cand
		}
		got = append(got, cand.Insert+"="+cand.Display)
	}
	if n != 1 {
		t.Fatalf("candidate %q count = %d, want 1; all: %s", insert, n, strings.Join(got, ", "))
	}
	return found
}

func none(t *testing.T, c repl.Completion, insert string) {
	t.Helper()
	for _, cand := range c.Candidates {
		if cand.Insert == insert {
			t.Fatalf("unexpected candidate %+v", cand)
		}
	}
}

// TestCompleteBindings — привязка побеждает прелюдию и хелпер, одно имя один раз.
func TestCompleteBindings(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, "x = 1\nf = len\n")

	c := at(t, s, "x|")
	if got := one(t, c, "x"); got.Display != "x" || c.From != 0 || c.To != 1 {
		t.Fatalf("x: %+v span %d:%d", got, c.From, c.To)
	}

	if got := one(t, at(t, s, "f|"), "f"); got.Display != "f/1" {
		t.Fatalf("f = len display = %q, want f/1", got.Display)
	}

	if got := one(t, at(t, s, "h|"), "h"); got.Display != "h/1" {
		t.Fatalf("helper h display = %q, want h/1", got.Display)
	}
	mustEval(t, s, out, "h = 1\nmap = 1\n")
	if got := one(t, at(t, s, "h|"), "h"); got.Display != "h" {
		t.Fatalf("binding h display = %q, want h", got.Display)
	}
	c = at(t, s, "map|")
	if got := one(t, c, "map"); got.Display != "map" {
		t.Fatalf("shadowed map display = %q, want map", got.Display)
	}
}

// TestCompletePrelude — голые имена прелюдии с арностью, ключевое слово не дополняется.
func TestCompletePrelude(t *testing.T) {
	s, _ := helperSession(t)

	if got := one(t, at(t, s, "len|"), "len"); got.Display != "len/1" {
		t.Fatalf("len display = %q", got.Display)
	}
	if got := one(t, at(t, s, "map|"), "map"); got.Display != "map/2" {
		t.Fatalf("map display = %q", got.Display)
	}
	if got := one(t, at(t, s, "mailbox_size|"), "mailbox_size"); got.Display != "mailbox_size/0,1" {
		t.Fatalf("mailbox_size display = %q", got.Display)
	}
	c := at(t, s, "ma|")
	for _, name := range []string{"map", "mailbox_size", "make_ref"} {
		if one(t, c, name).Insert != name {
			t.Fatalf("missing %s", name)
		}
	}
	if len(at(t, s, "fn|").Candidates) != 0 {
		t.Fatalf("keyword fn completed: %+v", at(t, s, "fn|"))
	}
}

// TestCompleteModuleMembers — видимость T-209 и арности; span — имя после точки.
func TestCompleteModuleMembers(t *testing.T) {
	s, out := helperSession(t)

	c := at(t, s, "List.ta|")
	got := one(t, c, "take")
	if got.Display != "take/2" || c.From != len("List.") || c.To != len("List.ta") {
		t.Fatalf("List.take: %+v span %d:%d", got, c.From, c.To)
	}
	none(t, at(t, s, "List.|"), "subject")
	none(t, at(t, s, "List.|"), "span")

	if got := one(t, at(t, s, "Json.en|"), "encode"); got.Display != "encode/1,2" {
		t.Fatalf("Json.encode display = %q", got.Display)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "m.brig")
	body := "module M\n\npub fn open() -> 1\n\nfn hid() -> 2\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadModules(path); err != nil {
		t.Fatalf("LoadModules: %v\n%s", err, out.String())
	}
	c = at(t, s, "M.|")
	if one(t, c, "open").Display != "open/0" {
		t.Fatalf("open: %+v", c.Candidates)
	}
	if one(t, c, "hid").Display != "hid/0" {
		t.Fatalf("hid: %+v", c.Candidates)
	}
}

// TestCompleteRecordFields — поля записи; запись прячет модуль, не-запись нет.
func TestCompleteRecordFields(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, "rec = { id: 1, name: \"ann\" }\n")

	c := at(t, s, "rec.n|")
	got := one(t, c, "name")
	if got.Display != "name" || c.From != len("rec.") || c.To != len("rec.n") {
		t.Fatalf("rec.name: %+v span %d:%d", got, c.From, c.To)
	}
	none(t, at(t, s, "rec.a.n|"), "name")
}

// TestCompleteLoad — литеральный префикс, звёздочка не шаблон, ведущая ~ раскрыта.
func TestCompleteLoad(t *testing.T) {
	s, _ := helperSession(t)
	dir := t.TempDir()
	for _, name := range []string{"foo.txt", "a*b", "ab"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	c := at(t, s, `load("foo|`)
	if one(t, c, "foo.txt").Display != "foo.txt" {
		t.Fatalf("foo: %+v", c.Candidates)
	}
	if c.From != len(`load("`) || c.To != len(`load("foo`) {
		t.Fatalf("load span %d:%d", c.From, c.To)
	}

	c = at(t, s, `load("a*|`)
	one(t, c, "a*b")
	none(t, c, "ab")

	if len(at(t, s, `print("foo|`).Candidates) != 0 {
		t.Fatal("path completed outside load")
	}

	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "docs", "note"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	if got := one(t, at(t, s, `load("~/docs/n|`), "~/docs/note"); got.Display != "~/docs/note" {
		t.Fatalf("tilde insert = %q", got.Insert)
	}
	if len(at(t, s, `load("~nobody/x|`).Candidates) != 0 {
		t.Fatal("~user was expanded")
	}
}

// TestSignatureHint — аргумент по токенам, лишний аргумент без метки, локальная fn.
func TestSignatureHint(t *testing.T) {
	s, out := helperSession(t)

	text, a, b := sigAt(t, s, "len(|")
	if text != "len(v)" || a != 4 || b != 5 {
		t.Fatalf("len(: text %q mark %d:%d", text, a, b)
	}

	text, a, b = sigAt(t, s, `map("a,b"|)`)
	if !strings.Contains(text, "map(xs, f)") || a < 0 || text[a:b] != "xs" {
		t.Fatalf("comma in string: text %q mark %d:%d %q", text, a, b, sliceMark(text, a, b))
	}
	text, a, b = sigAt(t, s, `map("a,b", |)`)
	if a < 0 || sliceMark(text, a, b) != "f" {
		t.Fatalf("real comma: text %q mark %q", text, sliceMark(text, a, b))
	}

	text, a, b = sigAt(t, s, "len(1, |)")
	if text == "" || a != -1 || b != -1 {
		t.Fatalf("extra arg: text %q mark %d:%d", text, a, b)
	}

	text, _, _ = sigAt(t, s, "len(1)|")
	if text != "" {
		t.Fatalf("after paren: %q", text)
	}

	text, a, b = sigAt(t, s, "map(len(|), 1)")
	if !strings.HasPrefix(text, "len(") || sliceMark(text, a, b) != "v" {
		t.Fatalf("nested: text %q mark %q", text, sliceMark(text, a, b))
	}

	mustEval(t, s, out, "fn sq(n) -> n\n")
	text, a, b = sigAt(t, s, "sq(|")
	if text != "sq(n)" || sliceMark(text, a, b) != "n" {
		t.Fatalf("local fn: text %q mark %q", text, sliceMark(text, a, b))
	}
}

func sigAt(t *testing.T, s *repl.Session, marked string) (string, int, int) {
	t.Helper()
	src, pos := caret(t, marked)
	return s.Signature(src, pos)
}

func sliceMark(text string, a, b int) string {
	if a < 0 || b > len(text) || a > b {
		return ""
	}
	return text[a:b]
}

// TestCompletePartial — ввод ещё не программа: ни паники, ни среза по байтам.
func TestCompletePartial(t *testing.T) {
	s, _ := helperSession(t)
	cases := []struct {
		name, marked string
		pos          int
		past         bool
	}{
		{"unclosed", "len(|", -1, false},
		{"mid ident", "x = le|n + 1", -1, false},
		{"comment", "x = 1 # map|", -1, false},
		{"start", "|len", -1, false},
		{"end", "len|", -1, false},
		{"multibyte", "", 3, false},
		{"past end", "", 99, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, pos := "яблоко", c.pos
			if c.marked != "" {
				src, pos = caret(t, c.marked)
			}
			if c.past {
				src, pos = "ж", 99
			}
			comp := s.Complete(src, pos)
			text, a, b := s.Signature(src, pos)
			n := utf8.RuneCountInString(src)
			if c.past {
				if comp.Candidates != nil || comp.From != 0 || comp.To != 0 || text != "" || a != -1 || b != -1 {
					t.Fatalf("past end: %+v sig %q %d:%d", comp, text, a, b)
				}
				return
			}
			if comp.From < 0 || comp.To > n || comp.From > comp.To {
				t.Fatalf("span %d:%d outside 0:%d", comp.From, comp.To, n)
			}
			if a > b || b > utf8.RuneCountInString(text) && a >= 0 {
				t.Fatalf("mark %d:%d text %q", a, b, text)
			}
		})
	}

	c := at(t, s, "x = le|n + 1")
	if one(t, c, "len").Display != "len/1" {
		t.Fatal("mid-ident")
	}
	if c.From != len("x = ") || c.To != len("x = len") {
		t.Fatalf("mid-ident span %d:%d", c.From, c.To)
	}
	if len(at(t, s, "x = 1 # map|").Candidates) != 0 {
		t.Fatal("completed inside a comment")
	}
	text, a, b := sigAt(t, s, "len(|")
	if text != "len(v)" || a != 4 || b != 5 {
		t.Fatalf("unclosed call: %q %d:%d", text, a, b)
	}
}
