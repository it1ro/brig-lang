// Package highlight раскрашивает исходник Brig для REPL, вывода и
// документации: лексерные классы и проверка имён, без разбора
// парсером и sema на каждое нажатие.
package highlight

// Class — класс подсветки. Имена совпадают с ключами BRIG_COLORS.
type Class string

// Классы словаря: от ключевых слов и литералов до неизвестного имени.
const (
	Keyword Class = "keyword"
	Atom    Class = "atom"
	String  Class = "string"
	Interp  Class = "interp"
	Bytes   Class = "bytes"
	Regex   Class = "regex"
	Number  Class = "number"
	Comment Class = "comment"
	Doc     Class = "doc"
	Module  Class = "module"
	Type    Class = "type"
	Op      Class = "operator"
	Punct   Class = "punct"
	Binding Class = "binding"
	Prelude Class = "prelude"
	Helper  Class = "helper"
	Unknown Class = "unknown"
	Error   Class = "error"
)

// Classes — словарь классов в стабильном порядке.
func Classes() []Class {
	return []Class{
		Keyword, Atom, String, Interp, Bytes, Regex, Number, Comment, Doc,
		Module, Type, Op, Punct, Binding, Prelude, Helper, Unknown, Error,
	}
}

// Env — имена, известные подсветке до разбора текущего буфера.
// Привязки сессии, прелюдия, хелперы и модули. nil-карты — пустые
// множества.
type Env struct {
	Bindings map[string]bool
	Prelude  map[string]bool
	Helpers  map[string]bool
	Modules  map[string]map[string]bool
	Types    map[string]bool
}

// REPLEnv — имена консоли: прелюдия (§11.5), хелперы Repl (§11.4),
// встроенные модули и типы. Привязки сессии добавляет вызывающий.
func REPLEnv() Env {
	e := Env{
		Bindings: map[string]bool{},
		Prelude:  setOf(preludeNames),
		Helpers:  setOf(helperNames),
		Modules:  map[string]map[string]bool{},
		Types:    setOf(typeNames),
	}
	for mod, fns := range builtinMods {
		e.Modules[mod] = setOf(fns)
	}
	bare := map[string]bool{}
	for _, n := range preludeNames {
		if n != "" && n[0] >= 'a' && n[0] <= 'z' {
			bare[n] = true
		}
	}
	e.Modules["Prelude"] = bare
	e.Modules["Repl"] = setOf(helperNames)
	return e
}

func setOf(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

var preludeNames = []string{
	"map", "filter", "find", "fold", "all", "any", "len",
	"list", "set",
	"to_str", "to_int", "to_float",
	"send", "spawn", "spawn_linked", "link", "watch", "unwatch",
	"self", "make_ref", "mailbox_size",
	"print", "eprint", "log",
	"assert", "raise",
	"Some", "Ok", "Error", "None",
}

var helperNames = []string{
	"h", "i", "v", "bindings", "reset", "load", "flush", "time", "dis", "recompile",
}

var typeNames = []string{
	"Int", "Float", "Decimal", "Bool", "Atom",
	"Str", "Bytes", "List", "Map", "Set", "Range",
	"Pid", "Ref", "Option", "Result",
}

var builtinMods = map[string][]string{
	"Vec":   {"push", "set", "get", "len"},
	"Map":   {"put", "get", "remove", "keys"},
	"Str":   {"to_bytes"},
	"Bytes": {"to_str"},
	"Json":  {"encode", "decode"},
	"Test":  {"describe", "it", "run", "assert_eq", "assert_ne", "assert", "fail"},
	"Sys":   {"args"},
}
