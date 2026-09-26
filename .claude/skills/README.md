# Skills для агентов — проект Brig

Skill-файлы, разбитые по подсистемам компилятора/VM языка Brig
(`lexer → parser → ast → sema → compiler → vm`), плюс протокол рабочей
сессии. Каждый skill описывает локальные инварианты, известные дефекты со
ссылками на issues и чек-лист перед коммитом, чтобы агент не
переоткрывал контекст в каждой сессии.

Claude Code и Cursor подхватывают `.claude/skills/<name>/SKILL.md`
автоматически по полю `description`. Как запускать агентов на задачах с
доски — `MAINTAINING.md` в корне репозитория.

## Состав

| Skill | Когда триггерится |
|---|---|
| `brig-workflow` | Начало и конец любой задачи с доски: issue, ветка, коммиты `[T-NN]`, PR, статусы, новые issues |
| `brig-overview` | Любая задача — принципы §0, источники истины, структура репо, где искать известные findings |
| `brig-lexer` | Правки `internal/lexer/*`, офсайд-алгоритм, escape-последовательности |
| `brig-parser-ast` | Правки `internal/parser/*`, `internal/ast/*`, грамматика `brig.ebnf` |
| `brig-sema` | Правки `internal/sema/*`, контекстный анализ §F.3 |
| `brig-compiler` | Правки `internal/compiler/*`, регистровый аллокатор, TCO, trap/ensure |
| `brig-vm` | Правки `internal/vm/*`, scheduler, опкоды, `vm.Verify` |
| `brig-testing-workflow` | Любая задача, требующая прогона тестов/golden/bytecode |

## Актуальность

Skills описывают состояние `iter/regvm` @ `8ab58cf` по `AUDIT_REPORT.md`.
Известные дефекты помечены `T-NN (#issue)`. Задача, которая снимает такое
ограничение, правит соответствующую строку skill в том же PR:
`rg -n 'T-NN' .claude/skills`.

## Модели

Модель задачи задаёт поле **Model** на доске (`sonnet` / `opus` /
`human`); для Effort large — только она.

- **opus** — дизайн семантики, инварианты компилятора и Verify, крупные
  full-fix (`compiler.go`, `scheduler.go`, `verify.go`).
- **sonnet** — реализация по готовому DoD, fail-fast, тесты, docs.
- **human** — design decisions и merge integration-ветки; агенту не
  отдаются.

`CHANGELOG.md` вручную не правится — он генерируется `make changelog`
(git-cliff).
