package term

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/termio"
)

// newTestEditor — редактор на строке in с хуками консоли Brig.
func newTestEditor(in string, out io.Writer) *Editor {
	e := NewEditor(strings.NewReader(in), out)
	e.NeedMore = repl.NeedMore
	e.Indent = repl.Indent
	e.IndentWidth = repl.IndentWidth
	return e
}

// drive применяет к e все клавиши из его входа, как ReadInput, но без
// ожидания конца ввода: возвращает завершённые вводы, а буфер остаётся
// в e.buf.
func drive(t *testing.T, e *Editor) []string {
	t.Helper()
	e.reset()
	var got []string
	for {
		k, err := e.readKey()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("readKey: %v", err)
		}
		src, done, err := e.handle(k)
		if done {
			if err == nil {
				got = append(got, src)
			}
			e.reset()
			continue
		}
		e.render()
	}
}

// show — буфер с курсором `|`.
func show(e *Editor) string {
	return string(e.buf.r[:e.buf.pos]) + "|" + string(e.buf.r[e.buf.pos:])
}

type keysCase struct {
	name, in, want string
}

func checkKeys(t *testing.T, cases []keysCase, setup func(*Editor)) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEditor(c.in, io.Discard)
			if setup != nil {
				setup(e)
			}
			if got := drive(t, e); len(got) != 0 {
				t.Fatalf("unexpected submit: %q", got)
			}
			if got := show(e); got != c.want {
				t.Errorf("buffer = %q, want %q", got, c.want)
			}
		})
	}
}

const (
	left      = "\x1b[D"
	right     = "\x1b[C"
	up        = "\x1b[A"
	down      = "\x1b[B"
	ctrlA     = "\x01"
	ctrlC     = "\x03"
	ctrlD     = "\x04"
	ctrlE     = "\x05"
	ctrlG     = "\x07"
	ctrlK     = "\x0b"
	ctrlL     = "\x0c"
	ctrlR     = "\x12"
	ctrlU     = "\x15"
	ctrlW     = "\x17"
	altB      = "\x1bb"
	altF      = "\x1bf"
	backspace = "\x7f"
	del       = "\x1b[3~"
	enter     = "\r"
)

// TestEditorKeys — T-202: последовательности байтов терминала → буфер и
// курсор.
func TestEditorKeys(t *testing.T) {
	checkKeys(t, []keysCase{
		{"insert", "abc", "abc|"},
		{"utf8", "привет" + left + backspace, "прив|т"},
		{"left", "abc" + left + left, "a|bc"},
		{"right", "abc" + left + left + right, "ab|c"},
		{"ctrl-b-f", "abc\x02\x02\x06", "ab|c"},
		{"left at start", "a" + left + left, "|a"},
		{"ctrl-a", "abc" + ctrlA, "|abc"},
		{"ctrl-e", "abc" + ctrlA + ctrlE, "abc|"},
		{"home csi", "abc\x1b[H", "|abc"},
		{"home tilde", "abc\x1b[1~", "|abc"},
		{"home ss3", "abc\x1bOH", "|abc"},
		{"end csi", "abc" + ctrlA + "\x1b[F", "abc|"},
		{"end tilde", "abc" + ctrlA + "\x1b[4~", "abc|"},
		{"end ss3", "abc" + ctrlA + "\x1bOF", "abc|"},
		{"backspace", "abc" + backspace, "ab|"},
		{"ctrl-h", "abc\x08", "ab|"},
		{"backspace at start", "abc" + ctrlA + backspace, "|abc"},
		{"delete", "abc" + ctrlA + del, "|bc"},
		{"delete at end", "abc" + del, "abc|"},
		{"ctrl-d deletes", "abc" + ctrlA + ctrlD, "|bc"},
		{"ctrl-k", "hello world" + ctrlA + altF + ctrlK, "hello|"},
		{"ctrl-u", "hello world" + left + left + ctrlU, "|ld"},
		{"ctrl-w", "foo bar baz" + ctrlW, "foo bar |"},
		{"ctrl-w spaces", "foo bar  " + ctrlW, "foo |"},
		{"alt-backspace", "foo bar\x1b\x7f", "foo |"},
		{"alt-b", "foo(bar)" + altB, "foo(|bar)"},
		{"alt-b twice", "foo bar" + altB + altB, "|foo bar"},
		{"alt-f", "foo bar" + ctrlA + altF, "foo| bar"},
		{"alt-f twice", "foo bar" + ctrlA + altF + altF, "foo bar|"},
		{"ctrl-left", "foo bar\x1b[1;5D", "foo |bar"},
		{"ctrl-right", "foo bar" + ctrlA + "\x1b[1;5C", "foo| bar"},
		{"unknown escape", "a\x1b[Zb\x1bxc", "abc|"},
		{"tab ignored", "a\tb", "ab|"},
		{"ctrl-l keeps buffer", "abc" + ctrlL, "abc|"},
		{"ctrl-c clears", "abc" + ctrlC + "de", "de|"},
		{"ctrl-c on block", "fn f() ->" + enter + "1" + ctrlC, "|"},
	}, nil)
}

// TestEditorReadInput — T-202: публичный цикл ReadInput: Enter, Ctrl-D,
// Ctrl-C, конец потока.
func TestEditorReadInput(t *testing.T) {
	e := newTestEditor("1 + 2"+enter+"abc"+ctrlC+"x"+enter+"  "+ctrlD+enter+ctrlD+"rest", io.Discard)
	var got []string
	for {
		src, err := e.ReadInput("> ", ". ")
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("ReadInput: %v", err)
			}
			break
		}
		got = append(got, src)
	}
	want := []string{"1 + 2\n", "x\n", "  \n"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("inputs = %q, want %q", got, want)
	}
	// Ctrl-D на пустом буфере закончил ввод: "rest" не прочитан.
	if rest, _ := io.ReadAll(e.in); string(rest) != "rest" {
		t.Errorf("after Ctrl-D: unread %q, want %q", rest, "rest")
	}
}

// TestEditorMultiline — T-202: многострочный ввод — один буфер; Enter по
// NeedMore и внутри блока, автоотступ, ↑/↓ по строкам и по истории.
func TestEditorMultiline(t *testing.T) {
	checkKeys(t, []keysCase{
		{"block header indents", "fn f(x) ->" + enter, "fn f(x) ->\n    |"},
		{"body keeps indent", "fn f(x) ->" + enter + "x + 1" + enter, "fn f(x) ->\n    x + 1\n    |"},
		{"nested block", "fn f(x) ->" + enter + "match x" + enter + "0 -> 1" + enter,
			"fn f(x) ->\n    match x\n        0 -> 1\n        |"},
		{"open bracket keeps indent", "xs = [1," + enter + "2", "xs = [1,\n2|"},
		{"backspace dedents", "fn f(x) ->" + enter + "match x" + enter + backspace,
			"fn f(x) ->\n    match x\n    |"},
		{"backspace dedents partial", "fn f(x) ->" + enter + "  " + backspace, "fn f(x) ->\n    |"},
		{"backspace to line start joins", "fn f(x) ->" + enter + backspace + backspace, "fn f(x) ->|"},
		{"backspace inside text", "fn f(x) ->" + enter + "ab" + backspace, "fn f(x) ->\n    a|"},
		{"up moves by lines", "fn f(x) ->" + enter + "x + 1" + up, "fn f(x) -|>\n    x + 1"},
		{"up clamps column", "fn f() ->" + enter + "some_long_name" + up, "fn f() ->|\n    some_long_name"},
		{"down moves by lines", "fn f(x) ->" + enter + "x + 1" + up + down, "fn f(x) ->\n    x + 1|"},
		{"ctrl-a is line start", "fn f(x) ->" + enter + "x" + ctrlA, "fn f(x) ->\n|    x"},
		{"ctrl-e is line end", "fn f(x) ->" + enter + "x" + up + ctrlA + ctrlE, "fn f(x) ->|\n    x"},
		{"ctrl-k joins at line end", "fn f(x) ->" + enter + "x" + up + ctrlE + ctrlK, "fn f(x) ->|    x"},
		{"ctrl-u is line only", "fn f(x) ->" + enter + "x + 1" + ctrlU, "fn f(x) ->\n|"},
		{"enter in middle of block", "fn f(x) ->" + enter + "a" + enter + "b" + up + enter,
			"fn f(x) ->\n    a\n    |\n    b"},
		{"left crosses lines", "fn f() ->" + enter + ctrlA + left, "fn f() ->|\n    "},
	}, nil)

	t.Run("blank line submits block", func(t *testing.T) {
		e := newTestEditor("fn f(x) ->"+enter+"x + 1"+enter+enter+"f(2)"+enter, io.Discard)
		got := drive(t, e)
		want := []string{"fn f(x) ->\n    x + 1\n    \n", "f(2)\n"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("inputs = %q, want %q", got, want)
		}
	})

	t.Run("enter mid single line submits", func(t *testing.T) {
		e := newTestEditor("1 + 2"+left+left+enter, io.Discard)
		if got := drive(t, e); len(got) != 1 || got[0] != "1 + 2\n" {
			t.Errorf("inputs = %q, want [\"1 + 2\\n\"]", got)
		}
	})

	hist := func(e *Editor) {
		e.History = &History{entries: []string{"x = 1", "fn g() ->\n    1", "y = 2"}}
	}
	// T-290 (D.8): после ↑ курсор — на первой строке записи, после ↓ — на
	// последней; непустой черновик — префикс поиска.
	checkKeys(t, []keysCase{
		{"up on empty: history", up, "y = 2|"},
		{"up twice: first line of entry", up + up, "fn g() ->|\n    1"},
		{"up past entry: older", up + up + up, "x = 1|"},
		{"up at oldest stays", up + up + up + up, "x = 1|"},
		{"down to newer: last line", up + up + up + down, "fn g() ->\n    1|"},
		{"down past entry: newer", up + up + up + down + down, "y = 2|"},
		{"down inside entry: last line", up + up + down, "fn g() ->\n    1|"},
		{"down restores draft", up + up + down + down + down, "|"},
		{"prefix up", "x" + up, "x = 1|"},
		{"prefix no match stays", "a" + up, "a|"},
		{"prefix down restores draft", "x" + up + down, "x|"},
		{"prefix multiline", "fn" + up, "fn g() ->|\n    1"},
		{"alt-p alt-n", "y" + "\x1bp" + "\x1bn", "y|"},
		{"alt-p inside entry", up + up + "\x1bp", "x = 1|"},
		{"down at draft stays", "a" + down, "a|"},
	}, hist)

	t.Run("recalled block needs blank line", func(t *testing.T) {
		e := newTestEditor(up+up+enter+enter, io.Discard)
		hist(e)
		got := drive(t, e)
		if len(got) != 1 || got[0] != "fn g() ->\n    1\n    \n" {
			t.Errorf("inputs = %q", got)
		}
	})
}

// TestBracketedPaste — T-202: вставка попадает в буфер как есть: без
// автоотступа и без исполнения по строкам.
func TestBracketedPaste(t *testing.T) {
	const code = "fn f(x) ->\r\n    match x\r\n        0 -> 1\r\n        _ -> x\r\n\r\nf(2)"
	checkKeys(t, []keysCase{
		{"multiline as is", "\x1b[200~" + code + "\x1b[201~",
			"fn f(x) ->\n    match x\n        0 -> 1\n        _ -> x\n\nf(2)|"},
		{"cr only", "\x1b[200~a\rb\x1b[201~", "a\nb|"},
		{"controls dropped, tab kept", "\x1b[200~a\x03\tb\x1b\x7f\x1b[201~", "a\tb|"},
		{"into middle", "()" + left + "\x1b[200~1,\n2\x1b[201~", "(1,\n2|)"},
		{"escape inside is text", "\x1b[200~\x1b[A\x1b[201~", "[A|"},
		{"after paste keys work", "\x1b[200~ab\x1b[201~" + backspace, "a|"},
	}, nil)

	t.Run("enter after paste", func(t *testing.T) {
		e := newTestEditor("\x1b[200~"+code+"\n\x1b[201~"+enter, io.Discard)
		got := drive(t, e)
		// T-290 (D.11): хвостовой перевод строки вставки обрезан.
		want := "fn f(x) ->\n    match x\n        0 -> 1\n        _ -> x\n\nf(2)\n"
		if len(got) != 1 || got[0] != want {
			t.Errorf("inputs = %q, want [%q]", got, want)
		}
	})
}

// TestHistorySearch — T-202: Ctrl-R — инкрементальный поиск по истории.
func TestHistorySearch(t *testing.T) {
	hist := func(e *Editor) {
		e.History = &History{entries: []string{"x = 1", "fn add(a, b) ->\n    a + b", "y = x + 1", "add(1, 2)"}}
	}
	checkKeys(t, []keysCase{
		{"newest match", ctrlR + "add", "|add(1, 2)"},
		{"incremental", ctrlR + "a", "|add(1, 2)"},
		{"query narrows", ctrlR + "a" + " +", "fn add(a, b) ->\n    |a + b"},
		{"ctrl-r older", ctrlR + "add" + ctrlR, "fn |add(a, b) ->\n    a + b"},
		{"no older keeps last", ctrlR + "add" + ctrlR + ctrlR, "fn |add(a, b) ->\n    a + b"},
		{"not found keeps buffer", "q" + ctrlR + "zzz", "q|"},
		{"backspace in query", ctrlR + "add" + ctrlR + backspace, "|add(1, 2)"},
		{"ctrl-g cancels", "q" + ctrlR + "add" + ctrlG, "q|"},
		{"key accepts and applies", ctrlR + "y =" + right, "y| = x + 1"},
		{"accept then up: older", ctrlR + "y =" + up, "fn add(a, b) ->|\n    a + b"},
		{"ctrl-c clears", "q" + ctrlR + "add" + ctrlC, "|"},
		{"no history: ignored", "", "|"},
	}, hist)

	t.Run("enter accepts and submits", func(t *testing.T) {
		e := newTestEditor(ctrlR+"x ="+enter, io.Discard)
		hist(e)
		if got := drive(t, e); len(got) != 1 || got[0] != "x = 1\n" {
			t.Errorf("inputs = %q", got)
		}
	})

	t.Run("prompt", func(t *testing.T) {
		scr := newScreen(80)
		e := newTestEditor(ctrlR+"add"+ctrlR+ctrlR, scr)
		hist(e)
		e.prompt = "> "
		drive(t, e)
		// Продолжение ввода рисуется с приглашением cont (здесь пустым).
		want := "(failed i-search)`add': fn █add(a, b) ->\n    a + b"
		if got := scr.String(); got != want {
			t.Errorf("screen:\n%s\nwant:\n%s", got, want)
		}
	})
}

// TestEditorRender — T-202: перерисовка многострочного ввода и переносов
// на эмуляторе экрана.
func TestEditorRender(t *testing.T) {
	cases := []struct {
		name, in string
		width    int
		want     string
	}{
		{"single line", "abc" + left, 80, "brig> ab█c"},
		{"multiline", "fn f(x) ->" + enter + "x + 1" + up, 80,
			"brig> fn f(x) -█>\n ...>     x + 1"},
		{"shrinks", "fn f(x) ->" + enter + "x" + backspace + backspace + backspace, 80,
			"brig> fn f(x) ->█"},
		{"wraps", "0123456789ab", 10, "brig> 0123\n456789ab█"},
		{"exact edge", "0123", 10, "brig> 0123\n█"},
		{"exact edge then more", "01234", 10, "brig> 0123\n4█"},
		{"wrap then edit above", "fn f() ->" + enter + "0123456789" + up + ctrlA, 10,
			"brig> █fn f\n() ->\n ...>\n0123456789\n"},
		{"wrapped line shrinks", "0123456789ab" + backspace + backspace + backspace + backspace + backspace, 10,
			"brig> 0123\n456█"},
		{"ctrl-c", "ab" + ctrlC + "c", 80, "brig> ab^C\nbrig> c█"},
		{"submit", "1" + enter + "2", 80, "brig> 1\nbrig> 2█"},
		{"control shown", "\x1b[200~a\tb\x1b[201~", 80, "brig> a^Ib█"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scr := newScreen(c.width)
			e := newTestEditor(c.in, scr)
			e.Width = func() int { return c.width }
			e.prompt, e.cont = "brig> ", " ...> "
			drive(t, e)
			if got := scr.String(); got != c.want {
				t.Errorf("screen:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

// TestEditorHooks — T-202: хук раскраски и «серый хвост»; без хуков —
// простой текст.
func TestEditorHooks(t *testing.T) {
	var out strings.Builder
	e := newTestEditor("x = 1", &out)
	drive(t, e)
	if regexp.MustCompile(`\x1b\[[0-9;]*m`).MatchString(out.String()) {
		t.Errorf("colors without hooks: %q", out.String())
	}

	out.Reset()
	scr := newScreen(80)
	e = newTestEditor("x = 1", io.MultiWriter(&out, scr))
	e.prompt = "> "
	e.Color = true
	e.Highlight = func(src string, _ int) string { return strings.ReplaceAll(src, "1", "\x1b[33m1\x1b[0m") }
	var hints []struct {
		src string
		pos int
	}
	e.Hint = func(src string, pos int) string {
		hints = append(hints, struct {
			src string
			pos int
		}{src, pos})
		return " + 2"
	}
	drive(t, e)
	if !strings.Contains(out.String(), "x = \x1b[33m1\x1b[0m\x1b[2m + 2\x1b[0m") {
		t.Errorf("render = %q, want colored buffer and grey hint", out.String())
	}
	if got := scr.String(); got != "> x = 1█ + 2" {
		t.Errorf("screen = %q", got)
	}
	if last := hints[len(hints)-1]; last.src != "x = 1" || last.pos != len([]rune("x = 1")) {
		t.Errorf("hint args = %+v, want src %q pos %d", last, "x = 1", len([]rune("x = 1")))
	}

	// Хвост — только в конце строки курсора; после Enter он стирается.
	scr = newScreen(80)
	e = newTestEditor("x = 1"+left, scr)
	e.prompt = "> "
	e.Color = true
	e.Hint = func(string, int) string { return "!" }
	drive(t, e)
	if got := scr.String(); got != "> x = █1" {
		t.Errorf("hint mid-line: screen = %q", got)
	}
	scr = newScreen(80)
	e = newTestEditor("x"+enter, scr)
	e.prompt = "> "
	e.Color = true
	e.Hint = func(string, int) string { return "yz" }
	drive(t, e)
	if got := scr.String(); got != "> x\n> █yz" {
		t.Errorf("hint after submit: screen = %q", got)
	}
}

func TestEditorComplete(t *testing.T) {
	food := func(_ string, pos int) Completion {
		return Completion{From: 0, To: pos, Candidates: []Candidate{
			{Insert: "food", Display: "food/1"},
			{Insert: "foot", Display: "foot/1"},
		}}
	}
	e := newTestEditor("fo\t", io.Discard)
	e.Complete = food
	drive(t, e)
	if got := show(e); got != "foo|" {
		t.Fatalf("common prefix: buffer = %q", got)
	}

	var raw strings.Builder
	scr := newScreen(80)
	e = newTestEditor("fo\t\t\t", io.MultiWriter(&raw, scr))
	e.Complete = food
	drive(t, e)
	// T-290 (D.10): повторный Tab перебирает кандидатов меню.
	if got := show(e); got != "food|" {
		t.Fatalf("third tab buffer = %q", got)
	}
	if !strings.Contains(scr.String(), "food/1") || !strings.Contains(scr.String(), "foot/1") {
		t.Fatalf("third tab dropped the menu: %q", scr.String())
	}
	e = newTestEditor("fo\t\t\t\t", io.Discard)
	e.Complete = food
	drive(t, e)
	if got := show(e); got != "foot|" {
		t.Fatalf("fourth tab buffer = %q", got)
	}
	e = newTestEditor("fo\t\t\t\t\t\x1b[Z", io.Discard)
	e.Complete = food
	drive(t, e)
	if got := show(e); got != "foot|" {
		t.Fatalf("shift-tab buffer = %q", got)
	}
	var bell strings.Builder
	e = newTestEditor("zz\t", &bell)
	e.Complete = func(string, int) Completion { return Completion{} }
	drive(t, e)
	if !strings.Contains(bell.String(), "\a") {
		t.Fatalf("no match: no bell in %q", bell.String())
	}

	scr = newScreen(80)
	e = newTestEditor("fo\t\tz", scr)
	e.Complete = food
	drive(t, e)
	if strings.Contains(scr.String(), "food") {
		t.Fatalf("rune left the menu up: %q", scr.String())
	}
	if got := show(e); got != "fooz|" {
		t.Fatalf("after rune: buffer = %q", got)
	}

	// Span задаёт сессия: редактору не нужно видеть границу имени.
	e = newTestEditor("xxfoyy\t", io.Discard)
	e.Complete = func(string, int) Completion {
		return Completion{From: 2, To: 4, Candidates: []Candidate{{Insert: "food", Display: "food"}}}
	}
	drive(t, e)
	if got := show(e); got != "xxfood|yy" {
		t.Fatalf("span replace: buffer = %q", got)
	}

	// Общий префикс пустой — меню на первом Tab.
	scr = newScreen(40)
	e = newTestEditor("\t", scr)
	e.Complete = func(string, int) Completion {
		return Completion{Candidates: []Candidate{
			{Insert: "ab", Display: "AB"},
			{Insert: "cd", Display: "CD"},
		}}
	}
	drive(t, e)
	if got := show(e); got != "|" {
		t.Fatalf("empty prefix changed the buffer: %q", got)
	}
	if !strings.Contains(scr.String(), "█") || !strings.Contains(scr.String(), "AB") {
		t.Fatalf("menu not under the cursor: %q", scr.String())
	}

	scr = newScreen(12)
	items := make([]Candidate, 30)
	for i := range items {
		s := fmt.Sprintf("%02d", i)
		items[i] = Candidate{Insert: s, Display: s}
	}
	e = newTestEditor("\t", scr)
	e.Width = func() int { return 12 }
	e.Complete = func(string, int) Completion {
		return Completion{Candidates: items}
	}
	drive(t, e)
	if !strings.Contains(scr.String(), "and 6 more") {
		t.Fatalf("menu cap: %q", scr.String())
	}
}

func TestEditorGhost(t *testing.T) {
	var raw strings.Builder
	scr := newScreen(80)
	e := newTestEditor("x", io.MultiWriter(&raw, scr))
	e.Color = false
	e.Hint = func(string, int) string { return "GHOST" }
	drive(t, e)
	if strings.Contains(scr.String(), "GHOST") || strings.Contains(raw.String(), "\x1b[90m") {
		t.Fatalf("color off drew the tail: screen %q raw %q", scr.String(), raw.String())
	}
	if got := show(e); got != "x|" {
		t.Fatalf("buffer = %q", got)
	}

	e = newTestEditor("x"+right, io.Discard)
	e.Color = false
	e.Hint = func(string, int) string { return "GHOST" }
	e.Complete = func(string, int) Completion { return Completion{} }
	drive(t, e)
	if got := show(e); got != "x|" {
		t.Fatalf("right without color inserted an invisible tail: buffer = %q", got)
	}

	e = newTestEditor("x\t", io.Discard)
	e.Color = false
	e.Hint = func(string, int) string { return "GHOST" }
	e.Complete = func(string, int) Completion { return Completion{} }
	drive(t, e)
	if got := show(e); got != "x|" {
		t.Fatalf("tab inserted the tail: buffer = %q", got)
	}

	e = newTestEditor("fn g() ->"+right, io.Discard)
	e.Color = true
	e.Hint = func(src string, _ int) string {
		if src == "fn g() ->" {
			return "\n    1"
		}
		return ""
	}
	drive(t, e)
	if got := show(e); got != "fn g() ->\n    1|" {
		t.Fatalf("multiline tail: buffer = %q", got)
	}

	scr = newScreen(80)
	raw.Reset()
	e = newTestEditor("x", io.MultiWriter(&raw, scr))
	e.Color = true
	e.Signature = func(string, int) (string, int, int) { return "len(v)", 4, 5 }
	drive(t, e)
	if !strings.Contains(raw.String(), "\x1b[4mv\x1b[24m") {
		t.Fatalf("underline missing: %q", raw.String())
	}
	if strings.Contains(scr.String(), "[v]") {
		t.Fatalf("color mode used brackets: %q", scr.String())
	}
	if line, _, _ := strings.Cut(scr.String(), "\n"); !strings.Contains(line, "█") {
		t.Fatalf("cursor left the input line: %q", scr.String())
	}

	scr = newScreen(80)
	e = newTestEditor("x", scr)
	e.Color = false
	e.Signature = func(string, int) (string, int, int) { return "len(v)", 4, 5 }
	e.Complete = func(string, int) Completion {
		return Completion{Candidates: []Candidate{{Insert: "ab", Display: "AB"}, {Insert: "cd", Display: "CD"}}}
	}
	// Подпись выше меню: отдельный кадр только с подписью, меню — по Tab.
	drive(t, e)
	if !strings.Contains(scr.String(), "len([v])") {
		t.Fatalf("brackets: %q", scr.String())
	}

	scr = newScreen(80)
	e = newTestEditor("\t", scr)
	e.Color = false
	e.Signature = func(string, int) (string, int, int) { return "len(v)", 4, 5 }
	e.Complete = func(string, int) Completion {
		return Completion{Candidates: []Candidate{{Insert: "ab", Display: "AB"}, {Insert: "cd", Display: "CD"}}}
	}
	drive(t, e)
	screen := scr.String()
	sig, menu := strings.Index(screen, "len([v])"), strings.Index(screen, "AB")
	if sig < 0 || menu < 0 || sig > menu {
		t.Fatalf("signature not above the menu: %q", screen)
	}
}

func TestReadKeyEOF(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[200~abc", "\x1bO"} {
		r := bufio.NewReader(strings.NewReader(in))
		if _, err := termio.ReadKey(r); !errors.Is(err, io.EOF) {
			t.Errorf("readKey(%q): err = %v, want EOF", in, err)
		}
	}
}

// chunkReader отдаёт вход порциями: одна порция на Read, как терминал,
// в котором байты приходят с паузами.
type chunkReader struct{ chunks []string }

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if r.chunks[0] == "" {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

// newChunked — редактор на порциях входа: хвост ESC ждётся только в
// текущей порции, следующая приходит позже EscDelay.
func newChunked(chunks ...string) *Editor {
	e := NewEditor(&chunkReader{chunks: chunks}, io.Discard)
	e.Wait = func(d time.Duration) bool {
		if d == termio.EscDelay {
			return e.in.Buffered() > 0
		}
		return true
	}
	return e
}

// TestEditorEscTimeout — T-290 (D.1): одиночный Esc без хвоста за
// EscDelay — KeyEsc, следующая клавиша не теряется; ESC с хвостом в той
// же порции — Alt-клавиша; в Ctrl-R Esc принимает найденное.
func TestEditorEscTimeout(t *testing.T) {
	e := newChunked("abc\x1b", "xy")
	drive(t, e)
	if got := show(e); got != "abcxy|" {
		t.Errorf("esc then x: buffer = %q, want abcxy|", got)
	}
	e = newChunked("foo bar", "\x1bb")
	drive(t, e)
	if got := show(e); got != "foo |bar" {
		t.Errorf("alt-b in one chunk: buffer = %q", got)
	}
	e = newChunked(ctrlR+"add", "\x1b", "!")
	e.History = &History{entries: []string{"add(1, 2)", "x"}}
	drive(t, e)
	if got := show(e); got != "!|add(1, 2)" {
		t.Errorf("esc in ctrl-r: buffer = %q, want the match kept", got)
	}
}

// TestEditorGhostNoColor — T-290 (D.2, D.3): без цвета хвост истории не
// предлагается и → не вставляет невидимый текст; с цветом хвост
// многострочной записи показан первой строкой с `…` и принимается по
// строке.
func TestEditorGhostNoColor(t *testing.T) {
	hist := &History{entries: []string{"List.map([1,2], f)", "fn g() ->\n    1"}}
	hint := func(src string, pos int) string { return hist.Suggest(string([]rune(src)[:pos])) }
	e := newTestEditor("Li"+right, io.Discard)
	e.Color = false
	e.History = hist
	e.Hint = hint
	drive(t, e)
	if got := show(e); got != "Li|" {
		t.Errorf("no color: buffer = %q, want Li|", got)
	}

	e = newTestEditor("fn"+right, io.Discard)
	e.Color = true
	e.History = hist
	e.Hint = hint
	drive(t, e)
	if got := show(e); got != "fn g() ->|" {
		t.Errorf("first right: buffer = %q, want one line", got)
	}

	scr := newScreen(80)
	e = newTestEditor("fn", scr)
	e.Color = true
	e.History = hist
	e.Hint = hint
	drive(t, e)
	if got := scr.String(); !strings.Contains(got, "fn█ g() -> …") {
		t.Errorf("multiline hint without marker: %q", got)
	}
}

// TestEditorTabIndent — T-290 (D.6): Tab в отступе вставляет уровень,
// Shift-Tab снимает; после текста Tab — дополнение.
func TestEditorTabIndent(t *testing.T) {
	called := false
	setup := func(e *Editor) {
		e.Complete = func(string, int) Completion {
			called = true
			return Completion{}
		}
	}
	checkKeys(t, []keysCase{
		{"tab at block line start", "fn f(x) ->" + enter + ctrlA + "\t", "fn f(x) ->\n    |    "},
		{"tab rounds to level", "fn f(x) ->" + enter + "  " + "\t", "fn f(x) ->\n        |"},
		{"shift-tab dedents", "fn f(x) ->" + enter + "\x1b[Z", "fn f(x) ->\n|"},
		{"shift-tab partial", "fn f(x) ->" + enter + "  \x1b[Z", "fn f(x) ->\n    |"},
		{"shift-tab from text", "fn f(x) ->" + enter + "x\x1b[Z", "fn f(x) ->\nx|"},
	}, setup)
	if called {
		t.Error("tab in indentation opened completion")
	}
}

// TestEditorYankUndo — T-290 (D.5, D.9): Ctrl-K/U/W кладут текст в kill
// ring, Ctrl-Y вставляет, Alt-Y листает, Ctrl-_ отменяет правку; набор
// подряд — один шаг undo.
func TestEditorYankUndo(t *testing.T) {
	const (
		ctrlY = "\x19"
		altY  = "\x1by"
		undo  = "\x1f"
		altD  = "\x1bd"
		ctrlT = "\x14"
	)
	checkKeys(t, []keysCase{
		{"kill and yank", "hello world" + ctrlW + ctrlA + ctrlY, "world|hello "},
		{"yank pop", "aa bb" + ctrlW + "cc" + ctrlW + ctrlY + altY, "aa bb|"},
		{"kills append", "one two" + ctrlW + ctrlW + ctrlY, "one two|"},
		{"kill end and yank twice", "abc" + ctrlA + ctrlK + ctrlY + ctrlY, "abcabc|"},
		{"undo typing", "abc def" + undo, "|"},
		{"undo kill", "abc def" + ctrlW + undo, "abc def|"},
		{"undo twice", "abc " + ctrlW + "x" + undo + undo, "abc |"},
		{"alt-d", "foo bar" + ctrlA + altD, "| bar"},
		{"ctrl-delete", "foo bar" + ctrlA + "\x1b[3;5~", "| bar"},
		{"alt-backspace alnum", "f(foo_bar" + "\x1b\x7f", "f(|"},
		{"ctrl-w by space", "f(foo_bar" + ctrlW, "|"},
		{"ctrl-t", "ab" + left + ctrlT, "ba|"},
		{"ctrl-t at end", "ab" + ctrlT, "ba|"},
		{"alt-<", "fn f() ->" + enter + "1" + "\x1b<", "|fn f() ->\n    1"},
		{"alt->", "fn f() ->" + enter + "1" + "\x1b<" + "\x1b>", "fn f() ->\n    1|"},
	}, nil)
	checkKeys(t, []keysCase{
		{"alt-dot", "print(\x1b.", "print(xs|"},
		{"alt-dot twice", "\x1b.\x1b.", "b|"},
	}, func(e *Editor) { e.History = &History{entries: []string{"a b", "f(1, xs)"}} })
}

// TestEditorAltEnter — T-290 (D.7): Alt-Enter отправляет многострочный
// ввод целиком из середины, не разрезая строку.
func TestEditorAltEnter(t *testing.T) {
	e := newTestEditor("fn f(x) ->"+enter+"x + 1"+up+left+left+"\x1b\r", io.Discard)
	got := drive(t, e)
	if len(got) != 1 || got[0] != "fn f(x) ->\n    x + 1\n" {
		t.Errorf("inputs = %q", got)
	}
	e = newTestEditor("\x1b\r", io.Discard)
	if got := drive(t, e); len(got) != 0 {
		t.Errorf("alt-enter on empty input submitted %q", got)
	}
}

// TestEditorResize — T-290 (D.4): после сужения окна терминал переносит
// строки, редактор рисует ввод заново с его первой строки: приглашение
// не дублируется, старые строки не остаются.
func TestEditorResize(t *testing.T) {
	scr := newScreen(40)
	width := 40
	changed := false
	e := NewEditor(&chunkReader{chunks: []string{"x = [1, 2, 3, 4, 5, 6, 7, 8]", "9"}}, scr)
	e.prompt, e.cont = "brig 1 ❯ ", "       · "
	e.Width = func() int { return width }
	e.Wait = func(time.Duration) bool {
		if e.in.Buffered() > 0 || changed || len(e.buf.r) == 0 {
			return true
		}
		changed = true
		width = 16
		scr.resize(16)
		return false
	}
	pending := true
	e.Resized = func() bool {
		r := changed && pending
		if r {
			pending = false
		}
		return r
	}
	drive(t, e)
	got := scr.String()
	if strings.Count(got, "brig 1") != 1 {
		t.Errorf("prompt duplicated after resize:\n%s", got)
	}
	want := "brig 1 ❯ x = [1,\n 2, 3, 4, 5, 6,\n7, 8]9█"
	if got != want {
		t.Errorf("screen after resize:\n%s\nwant:\n%s", got, want)
	}
}
