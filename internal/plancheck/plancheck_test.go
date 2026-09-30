package plancheck

import (
	"strings"
	"testing"
	"testing/fstest"
)

// block — полный блок задачи без issue.
func block(num, deps string) string {
	return "### T-" + num + " · задача\n<!-- meta\ndepends_on: " + deps + "\n-->\n" +
		"- **Файлы:** `x.go`\n- **Тест-якорь:** создать\n- **DoD:**\n  - `make all` → 0.\n" +
		"- **НЕ делать:** ничего лишнего.\n"
}

const header = "| # | T-NN | Задача | Ждёт | Модель |\n|---|---|---|---|---|\n"

func row(n, num, issue, deps string) string {
	link := ""
	if issue != "" {
		link = " [#" + issue + "](https://github.com/it1ro/brig-lang/issues/" + issue + ")"
	}
	return "| " + n + " | T-" + num + link + " | задача | " + deps + " | sonnet |\n"
}

func load(t *testing.T, files map[string]string) *Plan {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, src := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(src)}
	}
	p, err := Load(fsys, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// expect: ровно одна ошибка, и она содержит want; want == "" — ошибок нет.
func expect(t *testing.T, errs []string, want string) {
	t.Helper()
	if want == "" {
		if len(errs) != 0 {
			t.Fatalf("want no problems, got %q", errs)
		}
		return
	}
	if len(errs) != 1 || !strings.Contains(errs[0], want) {
		t.Fatalf("want one problem with %q, got %q", want, errs)
	}
}

func TestPlanCheckValid(t *testing.T) {
	p := load(t, map[string]string{
		"tasks/README.md": "[волна](wave-1.md), [аудит](../AUDIT.md#p-1), [web](https://example.com)\n",
		"AUDIT.md":        "аудит\n",
		"tasks/wave-1.md": "# Wave 1\n\n" + header + row("1", "10", "5", "—") + row("2", "11", "", "T-10") +
			"\nT-9 (#4) решён раньше.\n\n## Задачи\n\n" + block("11", "T-10, T-9"),
	})
	expect(t, p.Check(), "")
	expect(t, p.CheckOnline(map[int]string{4: "T-9 · старая", 5: "T-10 · задача"}), "")
}

func TestPlanCheckDuplicateNumber(t *testing.T) {
	p := load(t, map[string]string{
		"tasks/wave-1.md": block("11", "—"),
		"tasks/wave-2.md": block("11", "—"),
	})
	expect(t, p.Check(), "wave-2.md:1: duplicate block T-11 (first at tasks/wave-1.md:1)")
}

func TestPlanCheckMissingPart(t *testing.T) {
	src := strings.Replace(block("11", "—"), "- **Тест-якорь:** создать\n", "", 1)
	p := load(t, map[string]string{"tasks/wave-1.md": src})
	expect(t, p.Check(), "block T-11 has no Тест-якорь")
}

func TestPlanCheckDependsOnLarger(t *testing.T) {
	p := load(t, map[string]string{
		"tasks/wave-1.md": header + row("1", "12", "7", "—") + "\n" + block("11", "T-12"),
	})
	expect(t, p.Check(), "T-11 depends on T-12, not a smaller number")
}

func TestPlanCheckDependsOnUnknown(t *testing.T) {
	p := load(t, map[string]string{
		"tasks/wave-1.md": header + row("1", "12", "7", "T-5") + "\n" + block("11", "—"),
	})
	expect(t, p.Check(), "T-12 depends on T-5, unknown in tasks/")
}

func TestPlanCheckMissingLink(t *testing.T) {
	p := load(t, map[string]string{
		"tasks/README.md": "См. [волну](wave-9.md#вход).\n",
	})
	expect(t, p.Check(), "tasks/README.md:1: broken link wave-9.md#вход")
}

func TestPlanCheckIssueLinkMismatch(t *testing.T) {
	p := load(t, map[string]string{
		"tasks/wave-1.md": "[#5](https://github.com/it1ro/brig-lang/issues/6)\n",
	})
	expect(t, p.Check(), "issue link text #5 points to issues/6")
}

func TestPlanCheckOnlineCollision(t *testing.T) {
	p := load(t, map[string]string{
		"tasks/wave-1.md": header + row("1", "10", "5", "—") + "\n" + block("11", "T-10"),
	})
	// Номер блока без issue уже занят issue.
	expect(t, p.CheckOnline(map[int]string{5: "T-10 · задача", 8: "T-11 · чужая"}),
		"block T-11 has no issue link, but issue #8 is titled T-11")
	// Строка таблицы ссылается на issue с другим номером в title.
	expect(t, p.CheckOnline(map[int]string{5: "T-12 · задача"}),
		`T-10 links to #5 titled "T-12 · задача"`)
	expect(t, p.CheckOnline(map[int]string{}), "T-10 links to #5, no such issue")
}
