package examples

import (
	"strings"
	"testing"
	"unicode"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// ---- :type_error функций встроенных модулей: ((:mod, :f), v) (§10.4) ----

// modTypeErrCase — вызов функции модуля с аргументом неверного вида на
// позиции arg; ждём raise (:type_error, ((:mod, :f), bad)).
type modTypeErrCase struct {
	fn   string // "Module.f"
	arg  int    // позиция неверного аргумента
	call string // выражение вызова
	bad  string // значение в теге
}

// modTypeErrCases — по строке на каждый аргумент, вид которого функция
// проверяет сама. Аргумент, чью ошибку бросает прелюдия (индекс, колбэк,
// поле записи), здесь не описан: это тег прелюдии (`(:index_key, v)`,
// `(:call, f)`), не модуля.
var modTypeErrCases = []modTypeErrCase{
	{"Actor.info", 0, `Actor.info(5)`, `5`},

	{"Behavior.spawn", 0, `Behavior.spawn(:nope, 0)`, `:nope`},
	{"Behavior.spawn", 0, `Behavior.spawn(Behavior{ handlers: [] }, 0)`, `[]`},

	{"Bytes.at", 0, `Bytes.at(5, 0)`, `5`},
	{"Bytes.concat", 0, `Bytes.concat(5, b"a")`, `5`},
	{"Bytes.concat", 1, `Bytes.concat(b"a", 5)`, `5`},
	{"Bytes.find", 0, `Bytes.find(5, b"a")`, `5`},
	{"Bytes.find", 1, `Bytes.find(b"a", 5)`, `5`},
	{"Bytes.slice", 0, `Bytes.slice(5, 0, 1)`, `5`},
	{"Bytes.split", 0, `Bytes.split(5, b",")`, `5`},
	{"Bytes.split", 1, `Bytes.split(b"a", 5)`, `5`},
	{"Bytes.to_str", 0, `Bytes.to_str(5)`, `5`},

	{"Enum.all?", 0, `Enum.all?(5, x -> true)`, `5`},
	{"Enum.any?", 0, `Enum.any?(5, x -> true)`, `5`},
	{"Enum.count", 0, `Enum.count(5)`, `5`},
	{"Enum.drop", 0, `Enum.drop(5, 1)`, `5`},
	{"Enum.drop", 1, `Enum.drop([1], :a)`, `:a`},
	{"Enum.each", 0, `Enum.each(5, x -> x)`, `5`},
	{"Enum.filter", 0, `Enum.filter(5, x -> true)`, `5`},
	{"Enum.find", 0, `Enum.find(5, x -> true)`, `5`},
	{"Enum.flat_map", 0, `Enum.flat_map(5, x -> [x])`, `5`},
	{"Enum.fold", 0, `Enum.fold(5, 0, fn (a, x) -> a)`, `5`},
	{"Enum.group_by", 0, `Enum.group_by(5, x -> x)`, `5`},
	{"Enum.join", 0, `Enum.join(5, ",")`, `5`},
	{"Enum.join", 1, `Enum.join(["a"], 5)`, `5`},
	{"Enum.map", 0, `Enum.map(5, x -> x)`, `5`},
	{"Enum.max", 0, `Enum.max(5)`, `5`},
	{"Enum.member?", 0, `Enum.member?(5, 1)`, `5`},
	{"Enum.min", 0, `Enum.min(5)`, `5`},
	{"Enum.reject", 0, `Enum.reject(5, x -> true)`, `5`},
	{"Enum.reverse", 0, `Enum.reverse(5)`, `5`},
	{"Enum.sort", 0, `Enum.sort(5)`, `5`},
	{"Enum.sort_by", 0, `Enum.sort_by(5, x -> x)`, `5`},
	{"Enum.sum", 0, `Enum.sum(5)`, `5`},
	{"Enum.take", 0, `Enum.take(5, 1)`, `5`},
	{"Enum.take", 1, `Enum.take([1], :a)`, `:a`},
	{"Enum.uniq", 0, `Enum.uniq(5)`, `5`},
	{"Enum.with_index", 0, `Enum.with_index(5)`, `5`},
	{"Enum.zip", 0, `Enum.zip(5, [1])`, `5`},
	{"Enum.zip", 1, `Enum.zip([1], 5)`, `5`},

	{"Float.round", 0, `Float.round(5, 1)`, `5`},
	{"Float.round", 1, `Float.round(1.5, :a)`, `:a`},
	{"Float.to_str", 0, `Float.to_str(5, 1)`, `5`},
	{"Float.to_str", 1, `Float.to_str(1.5, :a)`, `:a`},

	{"Json.at", 1, `Json.at(%{}, 5)`, `5`},

	{"List.concat", 0, `List.concat(5, [])`, `5`},
	{"List.concat", 1, `List.concat([], 5)`, `5`},

	{"Map.filter", 0, `Map.filter(5, fn (k, v) -> true)`, `5`},
	{"Map.from_list", 0, `Map.from_list(5)`, `5`},
	{"Map.from_list", 0, `Map.from_list([1])`, `1`},
	{"Map.get", 0, `Map.get(5, 1)`, `5`},
	{"Map.get_or", 0, `Map.get_or(5, 1, 0)`, `5`},
	{"Map.keys", 0, `Map.keys(5)`, `5`},
	{"Map.put", 0, `Map.put(5, 1, 2)`, `5`},
	{"Map.remove", 0, `Map.remove(5, 1)`, `5`},
	{"Map.to_list", 0, `Map.to_list(5)`, `5`},
	{"Map.update", 0, `Map.update(5, 1, 0, x -> x)`, `5`},
	{"Map.values", 0, `Map.values(5)`, `5`},

	{"Option.and_then", 0, `Option.and_then(1, x -> x)`, `1`},
	{"Option.map", 0, `Option.map(1, x -> x)`, `1`},
	{"Option.unwrap", 0, `Option.unwrap(1)`, `1`},
	{"Option.unwrap_or", 0, `Option.unwrap_or(1, 0)`, `1`},

	{"Result.all", 0, `Result.all(1)`, `1`},
	{"Result.all", 0, `Result.all([Ok(1), 2])`, `2`},
	{"Result.and_then", 0, `Result.and_then(1, x -> x)`, `1`},
	{"Result.map", 0, `Result.map(1, x -> x)`, `1`},
	{"Result.unwrap", 0, `Result.unwrap(1)`, `1`},
	{"Result.unwrap_or", 0, `Result.unwrap_or(1, 0)`, `1`},

	{"Server.reply", 0, `Server.reply(:nobody, 1)`, `:nobody`},

	{"Str.ends_with?", 0, `Str.ends_with?(5, "a")`, `5`},
	{"Str.ends_with?", 1, `Str.ends_with?("a", 5)`, `5`},
	{"Str.find", 0, `Str.find(5, "a")`, `5`},
	{"Str.find", 1, `Str.find("a", 5)`, `5`},
	{"Str.join", 0, `Str.join(5, ",")`, `5`},
	{"Str.join", 0, `Str.join([1], ",")`, `1`},
	{"Str.join", 1, `Str.join(["a"], 5)`, `5`},
	{"Str.lower", 0, `Str.lower(5)`, `5`},
	{"Str.pad_left", 0, `Str.pad_left(5, 3, " ")`, `5`},
	{"Str.pad_left", 1, `Str.pad_left("a", :w, " ")`, `:w`},
	{"Str.pad_left", 2, `Str.pad_left("a", 3, 5)`, `5`},
	{"Str.pad_right", 0, `Str.pad_right(5, 3, " ")`, `5`},
	{"Str.pad_right", 1, `Str.pad_right("a", :w, " ")`, `:w`},
	{"Str.pad_right", 2, `Str.pad_right("a", 3, 5)`, `5`},
	{"Str.replace", 0, `Str.replace(5, "a", "b")`, `5`},
	{"Str.replace", 1, `Str.replace("a", 5, "b")`, `5`},
	{"Str.replace", 2, `Str.replace("a", "a", 5)`, `5`},
	{"Str.slice", 0, `Str.slice(5, 0, 1)`, `5`},
	{"Str.split", 0, `Str.split(5, ",")`, `5`},
	{"Str.split", 1, `Str.split("a", 5)`, `5`},
	{"Str.starts_with?", 0, `Str.starts_with?(5, "a")`, `5`},
	{"Str.starts_with?", 1, `Str.starts_with?("a", 5)`, `5`},
	{"Str.to_bytes", 0, `Str.to_bytes(5)`, `5`},
	{"Str.to_float", 0, `Str.to_float(5)`, `5`},
	{"Str.to_int", 0, `Str.to_int(5)`, `5`},
	{"Str.trim", 0, `Str.trim(5)`, `5`},
	{"Str.upper", 0, `Str.upper(5)`, `5`},

	{"Supervisor.start", 0, `Supervisor.start({ strategy: :rest_for_one, max_restarts: 3, within: 5000, children: [] })`, `:rest_for_one`},
	{"Supervisor.which_children", 0, `Supervisor.which_children(:nope)`, `:nope`},

	{"Vec.get", 0, `Vec.get(5, 0)`, `5`},
	{"Vec.push", 0, `Vec.push(5, 1)`, `5`},
	{"Vec.set", 0, `Vec.set(5, 0, 1)`, `5`},
}

// modTypeErrExempt — функции модулей вне таблицы: ключ "Module.f" или
// "Module" (весь модуль) → причина.
var modTypeErrExempt = map[string]string{
	"Actor.list":      "нет аргументов",
	"Observer.nodes":  "нет аргументов",
	"Sys.args":        "нет аргументов",
	"Time":            "нет аргументов",
	"Server.call":     "pid проверяет send прелюдии: (:send, v) — тег прелюдии",
	"Supervisor.stop": "делегирует Server.call: ошибки — теги прелюдии",
	"Test.assert_eq":  "без :type_error",
	"Test.assert_ne":  "без :type_error",
	"Test.fail":       "без :type_error",
	"Test.run":        "нет аргументов",
	"Test.assert":     "короткий тег модуля — T-237 #373",
	"Test.describe":   "короткий тег модуля — T-237 #373",
	"Test.it":         "короткий тег модуля — T-237 #373",
	"Global":          "короткий тег модуля — T-237 #373",
	"Record.to_anon":  "короткий тег модуля — T-237 #373",
	"Sys.halt":        "короткий тег модуля — T-237 #373",
	"Json.encode":     "короткий тег модуля — T-237 #373",
	"Json.decode":     "короткий тег модуля — T-237 #373",
	"File":            "короткий тег модуля — T-237 #373",
	"HttpServer":      "короткий тег модуля — T-237 #373",
	"Port":            "короткий тег модуля — T-237 #373",
	"Signal":          "короткий тег модуля — T-237 #373",
	"Telemetry":       "короткий тег модуля — T-237 #373",
	"Timer":           "короткий тег модуля — T-237 #373",
}

// snakeCase — имя модуля в теге: HttpServer → http_server.
func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// runModTypeErr исполняет вызов под trap и возвращает пару
// (результат, ожидаемый Error).
func runModTypeErr(t *testing.T, c modTypeErrCase) (got, want runtime.Value) {
	t.Helper()
	mod, fn, _ := strings.Cut(c.fn, ".")
	src := "module Main\n\nfn main() ->\n    r = trap(" + c.call + ")\n    (r, Error((:type_error, ((:" +
		snakeCase(mod) + ", :" + fn + "), " + c.bad + "))))\n"
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	if res := sema.Check(prog); res.HasErrors() {
		t.Fatalf("sema: %v\n%s", res.Diagnostics, src)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := vm.New()
	if err := compiler.InstallStdlib(m); err != nil {
		t.Fatal(err)
	}
	for name, f := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(f))
	}
	v, err := m.RunMain(m.Global("main"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return v.Tuple[0], v.Tuple[1]
}

// TestModuleTypeErrorTags — каждая функция модуля бросает :type_error со
// структурным тегом ((:mod, :f), v), v — неверный аргумент (§10.4, T-236).
func TestModuleTypeErrorTags(t *testing.T) {
	for _, c := range modTypeErrCases {
		if !strings.HasPrefix(c.call, c.fn+"(") {
			t.Errorf("%s: вызов %q не этой функции", c.fn, c.call)
			continue
		}
		t.Run(c.fn+"/"+c.call, func(t *testing.T) {
			got, want := runModTypeErr(t, c)
			if !runtime.Equal(got, want) {
				t.Errorf("arg %d: got %s, want %s", c.arg, got.Inspect(), want.Inspect())
			}
		})
	}
}

// TestModuleTypeErrorTagsComplete — каждая функция модуля есть в
// modTypeErrCases или в modTypeErrExempt.
func TestModuleTypeErrorTagsComplete(t *testing.T) {
	code := moduleFuncs(t, nil)
	covered := map[string]bool{}
	for _, c := range modTypeErrCases {
		covered[c.fn] = true
		if _, ok := code[c.fn]; !ok {
			t.Errorf("%s в modTypeErrCases, но в коде её нет", c.fn)
		}
	}
	mods := map[string]bool{}
	for name := range code {
		mod, _, _ := strings.Cut(name, ".")
		mods[mod] = true
	}
	for _, name := range sortedStrings(code) {
		mod, _, _ := strings.Cut(name, ".")
		_, exempt := modTypeErrExempt[name]
		_, modExempt := modTypeErrExempt[mod]
		switch {
		case covered[name] && (exempt || modExempt):
			t.Errorf("%s есть и в modTypeErrCases, и в modTypeErrExempt", name)
		case !covered[name] && !exempt && !modExempt:
			t.Errorf("%s нет ни в modTypeErrCases, ни в modTypeErrExempt", name)
		}
	}
	for key := range modTypeErrExempt {
		if _, ok := code[key]; !ok && !mods[key] {
			t.Errorf("%s в modTypeErrExempt, но в коде такого модуля или функции нет", key)
		}
	}
}
