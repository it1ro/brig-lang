package examples

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/lexer"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Сверка таблиц спеки с кодом (F-5, S-9, P-12). Расхождение не чинится
// правкой спеки или кода под тест: оно попадает в allowlist с номером
// задачи или разделом спеки, где имя описано.

const (
	specPath = "../../docs/01-language-design.md"
	ebnfPath = "../../brig.ebnf"
)

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// specSection — текст раздела от строки-заголовка с префиксом head до
// следующего заголовка того же или более высокого уровня (вне fenced-блоков).
func specSection(t *testing.T, doc, head string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, head) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("раздел %q не найден в спеке", head)
	}
	level := headingLevel(lines[start])
	inFence := false
	for j := start + 1; j < len(lines); j++ {
		l := lines[j]
		if strings.HasPrefix(l, "```") {
			inFence = !inFence
			continue
		}
		if lv := headingLevel(l); !inFence && lv > 0 && lv <= level {
			return strings.Join(lines[start+1:j], "\n")
		}
	}
	return strings.Join(lines[start+1:], "\n")
}

// firstFence — содержимое первого fenced-блока раздела.
func firstFence(t *testing.T, sec string) string {
	t.Helper()
	var body []string
	in := false
	for _, l := range strings.Split(sec, "\n") {
		if strings.HasPrefix(l, "```") {
			if in {
				return strings.Join(body, "\n")
			}
			in = true
			continue
		}
		if in {
			body = append(body, l)
		}
	}
	t.Fatal("fenced-блок не найден")
	return ""
}

func toSet(xs []string) map[string]bool {
	s := make(map[string]bool, len(xs))
	for _, x := range xs {
		s[x] = true
	}
	return s
}

func sortedSet(s map[string]bool) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// diffSets — элементы a, которых нет в b.
func diffSets(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func assertSameSet(t *testing.T, nameA string, a map[string]bool, nameB string, b map[string]bool) {
	t.Helper()
	if d := diffSets(a, b); len(d) > 0 {
		t.Errorf("есть в %s, нет в %s: %v", nameA, nameB, d)
	}
	if d := diffSets(b, a); len(d) > 0 {
		t.Errorf("есть в %s, нет в %s: %v", nameB, nameA, d)
	}
}

// ---- Ключевые слова: §1.3 = §B.1 = лексер = brig.ebnf ----

// ebnfNonKeywordTerminals — строчные терминалы brig.ebnf, которые не
// ключевые слова: части литералов, а не слова языка.
var ebnfNonKeywordTerminals = map[string]string{
	"a":   "hex_digit: \"a\"..\"f\"",
	"f":   "hex_digit: \"a\"..\"f\"",
	"e":   "экспонента float",
	"dec": "сигил Decimal dec\"...\"",
}

func TestSpecKeywordsMatchLexer(t *testing.T) {
	doc := readRepoFile(t, specPath)
	s13 := toSet(strings.Fields(firstFence(t, specSection(t, doc, "### 1.3 "))))
	sB1 := toSet(strings.Fields(firstFence(t, specSection(t, doc, "#### Ключевые слова"))))

	lex := map[string]bool{}
	for tt := lexer.KW_FN; tt <= lexer.KW_QUOTE; tt++ {
		lex[tt.String()] = true
	}
	for w := range lex {
		toks, err := lexer.Lex(w)
		if err != nil || len(toks) == 0 || !toks[0].IsKeyword() {
			t.Errorf("лексер не выдаёт ключевое слово для %q", w)
		}
	}

	ebnf := readRepoFile(t, ebnfPath)
	const marker = "(* LOWER_IDENT не совпадает с ключевым словом"
	i := strings.Index(ebnf, marker)
	if i < 0 {
		t.Fatalf("в brig.ebnf нет комментария %q", marker)
	}
	comment := ebnf[i : i+strings.Index(ebnf[i:], "*)")]
	eb := map[string]bool{}
	for _, l := range strings.Split(comment, "\n")[1:] {
		if strings.Contains(l, "зарезервирован") {
			break
		}
		for _, w := range strings.Fields(l) {
			eb[w] = true
		}
	}

	assertSameSet(t, "§1.3", s13, "§B.1", sB1)
	assertSameSet(t, "§1.3", s13, "лексере", lex)
	assertSameSet(t, "§1.3", s13, "brig.ebnf", eb)

	for _, m := range regexp.MustCompile(`"([a-z]+)"`).FindAllStringSubmatch(ebnf, -1) {
		w := m[1]
		if !s13[w] && ebnfNonKeywordTerminals[w] == "" {
			t.Errorf("терминал %q в brig.ebnf — не ключевое слово §1.3 и не в ebnfNonKeywordTerminals", w)
		}
	}
}

// ---- Прелюдия: §11.5 и §12.6 ⊆ InstallPrelude + акторные опкоды ----

var (
	reTableName = regexp.MustCompile("(?m)^\\| `([A-Za-z_][A-Za-z_.?]*)` \\|")
	reCallName  = regexp.MustCompile(`(?m)^([a-z_]+)\(`)
)

// preludeCodeOnly — имена прелюдии в коде, которых нет в таблицах §11.5.
var preludeCodeOnly = map[string]string{
	"Some":         "конструктор, §10.1",
	"Ok":           "конструктор, §10.1",
	"Error":        "конструктор, §10.1",
	"Str.to_bytes": "описан в §C.3/§C.6, в таблице §11.5 не дублируется",
	"Bytes.to_str": "описан в §C.3/§C.6, в таблице §11.5 не дублируется",
	"Map.put":      "функция модуля Map, §4.5; в §11.5 только Map.get_or",
	"Map.get":      "функция модуля Map, §4.5; в §11.5 только Map.get_or",
	"Map.remove":   "функция модуля Map, §4.5; в §11.5 только Map.get_or",
	"Map.keys":     "функция модуля Map, §4.5; в §11.5 только Map.get_or",
	"Json.encode":  "функция модуля Json, §4.7; в §11.5 только Json.at",
	"Json.decode":  "функция модуля Json, §4.7; в §11.5 только Json.at",
}

// codeActorPrimitives — акторные примитивы в коде: голые имена прелюдии
// sema, которые не глобалы-функции (опкоды ВМ), кроме значения None.
func codeActorPrimitives() map[string]bool {
	fns := toSet(sema.BuiltinModules()["Prelude"])
	out := map[string]bool{}
	for _, n := range sema.PreludeNames() {
		if !fns[n] && n != "None" {
			out[n] = true
		}
	}
	return out
}

func specActorPrimitives(t *testing.T, doc string) map[string]bool {
	t.Helper()
	fence := firstFence(t, specSection(t, doc, "### 12.6 "))
	out := map[string]bool{}
	for _, m := range reCallName.FindAllStringSubmatch(fence, -1) {
		out[m[1]] = true
	}
	return out
}

func TestSpecPreludeMatchesInstall(t *testing.T) {
	doc := readRepoFile(t, specPath)
	machine := vm.New()
	isFn := func(n string) bool { return machine.Global(n).Kind == runtime.KindFunction }
	actors := codeActorPrimitives()

	spec := map[string]bool{}
	for _, m := range reTableName.FindAllStringSubmatch(specSection(t, doc, "### 11.5 "), -1) {
		spec[m[1]] = true
	}
	if len(spec) == 0 {
		t.Fatal("таблица §11.5 не разобрана")
	}
	for _, n := range sortedSet(spec) {
		if !isFn(n) && !actors[n] {
			t.Errorf("§11.5: %q нет ни в InstallPrelude, ни среди акторных опкодов", n)
		}
	}

	specActors := specActorPrimitives(t, doc)
	assertSameSet(t, "§12.6 (акторные примитивы)", specActors, "коде (опкоды)", actors)

	code := map[string]bool{}
	for _, n := range sema.BuiltinModules()["Prelude"] {
		code[n] = true
	}
	for _, mod := range []string{"Str", "Bytes", "Map", "Json"} {
		for _, n := range sema.BuiltinModules()[mod] {
			code[mod+"."+n] = true
		}
	}
	for _, n := range sortedSet(code) {
		if !isFn(n) {
			t.Errorf("sema считает %q функцией прелюдии, а в ВМ такого глобала нет", n)
		}
		if !spec[n] && preludeCodeOnly[n] == "" {
			t.Errorf("%q есть в коде, нет в §11.5 и в preludeCodeOnly", n)
		}
	}
	for n := range preludeCodeOnly {
		if spec[n] {
			t.Errorf("%q уже есть в §11.5 — убрать из preludeCodeOnly", n)
		}
	}
}

// ---- Авто-raise: §10.4 ↔ атомы, которые код бросает ----

var (
	reSpecRaise = regexp.MustCompile("(?m)^- `\\(:([a-z_?]+),")
	reGoRaise   = regexp.MustCompile(`ErrRaise\{Val:\s*runtime\.Tuple\(\s*runtime\.Atom\("([a-z_?]+)"\)`)
	reGoKonst   = regexp.MustCompile(`konst\(runtime\.Atom\("([a-z_?]+)"\)\)`)
	reBrigRaise = regexp.MustCompile(`raise\(\(:([a-z_?]+)`)
)

// raiseNotInSpec104 — атомы, которые код бросает первым элементом, но
// которых нет в списке §10.4: либо описаны в своём разделе спеки, либо
// расхождение с номером задачи.
var raiseNotInSpec104 = map[string]string{
	"assert_eq_failed":  "Test.assert_eq, §11.6",
	"assert_ne_failed":  "Test.assert_ne, §11.6",
	"assert_fail":       "Test.fail, §11.6",
	"load_error":        "хелпер консоли load, §11.4",
	"no_value":          "хелпер консоли v, §11.4",
	"json_encode_error": "Json.encode, N10",
	"field_error":       "нет в спеке: User{ ..r } с полем не из типа — T-234",
	"unwrap":            "нет в спеке: Option.unwrap/Result.unwrap — T-234",
	"max_restarts":      "нет в спеке: Supervisor — T-234",
	"start_failed":      "нет в спеке: Supervisor — T-234",
	"timeout":           "нет в спеке: Supervisor.which_children — T-234",
	"no_handler":        "Behavior, §13.2",
	"bad_arity":         "Behavior, §13.2",
}

// thrownAtoms — атомы-теги raise в Go-коде internal/ и в stdlib/*.brig.
func thrownAtoms(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	add := func(atom, where string) { out[atom] = append(out[atom], where) }
	walk := func(root string, visit func(path, src string)) {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_test.brig") {
				return nil
			}
			if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".brig") {
				visit(path, readRepoFile(t, path))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	goVisit := func(path, src string) {
		if !strings.HasSuffix(path, ".go") {
			return
		}
		for _, re := range []*regexp.Regexp{reGoRaise, reGoKonst} {
			for _, m := range re.FindAllStringSubmatch(src, -1) {
				add(m[1], path)
			}
		}
	}
	walk("..", goVisit)
	walk("../../stdlib", func(path, src string) {
		if !strings.HasSuffix(path, ".brig") {
			return
		}
		for _, l := range strings.Split(src, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			for _, m := range reBrigRaise.FindAllStringSubmatch(l, -1) {
				add(m[1], path)
			}
		}
	})
	return out
}

func TestSpecAutoRaiseNames(t *testing.T) {
	doc := readRepoFile(t, specPath)
	spec := map[string]bool{}
	for _, m := range reSpecRaise.FindAllStringSubmatch(specSection(t, doc, "### 10.4 "), -1) {
		spec[m[1]] = true
	}
	if len(spec) == 0 {
		t.Fatal("список §10.4 не разобран")
	}
	thrown := thrownAtoms(t)
	for _, n := range sortedSet(spec) {
		if len(thrown[n]) == 0 {
			t.Errorf("§10.4: авто-raise %q нигде в коде не бросается", n)
		}
	}
	for atom, where := range thrown {
		if !spec[atom] && raiseNotInSpec104[atom] == "" {
			t.Errorf("код бросает (:%s, …) (%s), а в §10.4 и raiseNotInSpec104 его нет", atom, where[0])
		}
	}
	for atom := range raiseNotInSpec104 {
		if spec[atom] {
			t.Errorf("(:%s, …) уже в §10.4 — убрать из raiseNotInSpec104", atom)
		}
		if len(thrown[atom]) == 0 {
			t.Errorf("(:%s, …) в raiseNotInSpec104, но код его не бросает", atom)
		}
	}
}

// ---- Pipe-запрет: §7.5 = §12.6 = проверка sema ----

func TestSpecPipeForbiddenPrimitives(t *testing.T) {
	doc := readRepoFile(t, specPath)
	sec := specSection(t, doc, "### 7.5 ")
	const marker = "В качестве правой части `|>` запрещены"
	i := strings.Index(sec, marker)
	if i < 0 {
		t.Fatalf("в §7.5 нет фразы %q", marker)
	}
	para := sec[i:]
	if j := strings.Index(para, "\n"); j >= 0 {
		para = para[:j]
	}
	para = para[strings.Index(para, ":")+1:]
	spec := map[string]bool{}
	for _, m := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(para, -1) {
		spec[m[1]] = true
	}

	assertSameSet(t, "§7.5", spec, "§12.6", specActorPrimitives(t, doc))
	assertSameSet(t, "§7.5", spec, "коде (опкоды)", codeActorPrimitives())

	for _, n := range sortedSet(spec) {
		src := "module M\n\nfn main() -> 1 |> " + n + "(2)\n"
		prog, err := parser.ParseProgram(parser.ModeModule, src)
		if err != nil {
			t.Errorf("%s: parse: %v", n, err)
			continue
		}
		if !sema.Check(prog).HasErrors() {
			t.Errorf("sema пропускает %q в правой части |>", n)
		}
	}
}
