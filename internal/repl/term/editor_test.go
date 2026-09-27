package term

import (
	"bufio"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/repl"
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
		k, err := readKey(e.in)
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
	checkKeys(t, []keysCase{
		{"up on first line: history", "a" + up, "y = 2|"},
		{"up twice: multiline entry", "a" + up + up, "fn g() ->\n    1|"},
		{"up inside entry moves by lines", "a" + up + up + up, "fn g(|) ->\n    1"},
		{"up past entry: older", "a" + up + up + up + up, "x = 1|"},
		{"up at oldest stays", "a" + up + up + up + up + up, "x = 1|"},
		{"down to newer", "a" + up + up + down, "y = 2|"},
		{"down restores draft", "a" + up + up + down + down, "a|"},
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
		want := "fn f(x) ->\n    match x\n        0 -> 1\n        _ -> x\n\nf(2)\n\n"
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
		{"accept then up: older", ctrlR + "y =" + up, "fn add(a, b) ->\n    a + b|"},
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
	if !strings.Contains(out.String(), "x = \x1b[33m1\x1b[0m\x1b[90m + 2\x1b[0m") {
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
	e.Hint = func(string, int) string { return "!" }
	drive(t, e)
	if got := scr.String(); got != "> x = █1" {
		t.Errorf("hint mid-line: screen = %q", got)
	}
	scr = newScreen(80)
	e = newTestEditor("x"+enter, scr)
	e.prompt = "> "
	e.Hint = func(string, int) string { return "yz" }
	drive(t, e)
	if got := scr.String(); got != "> x\n> █yz" {
		t.Errorf("hint after submit: screen = %q", got)
	}
}

func TestReadKeyEOF(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[200~abc", "\x1bO"} {
		r := bufio.NewReader(strings.NewReader(in))
		if _, err := readKey(r); !errors.Is(err, io.EOF) {
			t.Errorf("readKey(%q): err = %v, want EOF", in, err)
		}
	}
}
