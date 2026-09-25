# Skills для Claude Code — проект Brig

Набор skill-файлов, разбитых по подсистемам компилятора/VM языка Brig
(`lexer → parser → ast → sema → compiler → vm`). Каждый skill описывает
локальные инварианты подсистемы, типичные ошибки и чек-лист перед коммитом,
чтобы Claude Code не переоткрывал контекст заново в каждой сессии и не
нарушал задокументированные гарантии.

## Установка

Скопируйте папки в `.claude/skills/` корня репозитория `brig`:

```sh
cp -r brig-overview brig-lexer brig-parser-ast brig-sema brig-compiler brig-vm brig-testing-workflow \
    /path/to/brig/.claude/skills/
```

Claude Code подхватывает `SKILL.md` из `.claude/skills/<name>/SKILL.md`
автоматически по релевантности задачи (см. описание в каждом файле).

## Состав

| Skill | Когда триггерится |
|---|---|
| `brig-overview` | Любая задача — общие принципы §0, источники истины, структура репо |
| `brig-lexer` | Правки `internal/lexer/*`, офсайд-алгоритм, escape-последовательности |
| `brig-parser-ast` | Правки `internal/parser/*`, `internal/ast/*`, грамматика `brig.ebnf` |
| `brig-sema` | Правки `internal/sema/*`, контекстный анализ §F.3 |
| `brig-compiler` | Правки `internal/compiler/*`, регистровый аллокатор, TCO, trap/ensure |
| `brig-vm` | Правки `internal/vm/*`, scheduler, опкоды, `vm.Verify` |
| `brig-testing-workflow` | Любая задача, требующая прогона тестов/golden/bytecode |

## Рекомендация по моделям

- **Opus** — дизайн новой семантики, отладка нарушений инвариантов, ревью
  диффов по `compiler.go` / `scheduler.go` / `verify.go`.
- **Sonnet** — реализация по уже согласованному плану, prelude-функции,
  типовые тесты.
- **Haiku** — навигация по кодовой базе, мелкая косметика, обновление
  `STATUS.md`/`CHANGELOG.md`.
