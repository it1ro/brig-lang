// Package highlight раскрашивает исходник Brig для REPL, вывода и
// документации: лексерные классы и проверка имён, без разбора
// парсером и sema на каждое нажатие.
package highlight

import "github.com/it1ro/brig-lang/internal/sema"

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
// встроенные модули и типы. Имена прелюдии, хелперов и модулей берутся
// из sema, чтобы подсветка не расходилась с проверкой. Привязки сессии
// добавляет вызывающий.
func REPLEnv() Env {
	helpers := sema.ReplHelperNames()
	e := Env{
		Bindings: map[string]bool{},
		Prelude:  setOf(sema.PreludeNames()),
		Helpers:  setOf(helpers),
		Modules:  map[string]map[string]bool{},
		Types:    setOf(typeNames),
	}
	for mod, fns := range sema.BuiltinModules() {
		e.Modules[mod] = setOf(fns)
	}
	e.Modules["Repl"] = setOf(helpers)
	return e
}

func setOf(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

var typeNames = []string{
	"Int", "Float", "Decimal", "Bool", "Atom",
	"Str", "Bytes", "List", "Map", "Set", "Range",
	"Pid", "Ref", "Option", "Result",
}
