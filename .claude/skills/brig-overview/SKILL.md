---
name: brig-overview
description: >
  Общий контекст проекта Brig (референсный интерпретатор языка на Go):
  принципы дизайна (§0), иерархия источников истины, структура репозитория,
  как организована работа (доска, milestones, tasks/), exit-коды.
  Читать первым в любой задаче по этому репозиторию, до погружения в
  конкретную подсистему.
---

# Brig — общий контекст

Brig — язык с отступами (offside), неизменяемыми значениями, акторами и
TCO. Референсная реализация на Go: `lexer → parser → ast → sema →
compiler → vm`, без циклов зависимостей (`parser` не видит `vm`, `vm` не
видит `cmd/*`).

## Как организована работа

- Задачи — только issues на доске GitHub Projects v2 (`it1ro/brig-lang`,
  проект 5). Протокол сессии — skill `brig-workflow`; правила и команды —
  `WORKFLOW.md`.
- [`AUDIT_REPORT.md`](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT.md) — первый аудит (`iter/regvm` @ `8ab58cf`, ID вида
  S-F2, A-F3, I-F9, O-F1); [`AUDIT_REPORT-2.md`](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT-2.md) — второй (`main` @ `fefb355`,
  ID вида P-1, S-1, G-1, R-1, F-1). Перед тем как считать что-то новой
  находкой — искать в обоих. Отчёты аудитов удалены из репозитория
  2026-09-30 (все задачи — на доске, эпик третьего аудита — #276); читать
  по permalink или `git show 28003da:AUDIT_REPORT.md`.
- План — milestones GitHub (M1…M5 по третьему аудиту, эпик #276;
  критерий выхода — в описании milestone). `tasks/` — карта плана
  (`tasks/README.md` — индекс) и архив волн 0–15: волны 0–6 закрыты; волны 7–13 спланированы вторым аудитом (контуры
  обратной связи → DD и спека → остаток Must → язык для библиотек →
  укрепление → рантайм → Should); DD по §17 — `decisions.md`. Новые задачи
  сразу заводятся issues, без блоков в `tasks/`. Статусов в
  `tasks/` нет — только на доске.
- Регистровая VM (Sprint 7) вмержена в `main` задачей T-07 (#7); ветки
  `iter/regvm` больше нет.

## Источники истины (по убыванию приоритета)

1. `docs/01-language-design.md` (v0.4.7) — единственный нормативный
   документ языка, включая §16 (Must/Should/Nice/Не надо). Part I —
   дизайн, Part II — формальная спецификация, история изменений — `docs/spec-history.md`.
2. `brig.ebnf` — исполнительная грамматика, должна совпадать с §A.
3. `docs/02-register-based-virtual-machine.md` —
   дизайн VM (опкоды, кадры, TCO, trap/ensure, соглашения K-1…K-8).
   Если они противоречат п.1–2 — ошибка в них, а не в спеке (пример:
   K-2 truthiness против строгого Bool §7.2/§16 — design decision #41
   решён в пользу строгого Bool, K-2 переписывается в T-62).
4. Skills (`.claude/skills/`), `tasks/`, `README.md` — рабочие заметки
   и обзоры, не нормативны.

Код не является источником истины: если код расходится со спекой — это
finding (см. [`AUDIT_REPORT.md`](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT.md)), а не «фактическое поведение, под которое
надо подстроить доку». Известное противоречие внутри тира 1–2: проза §12.4
(«else/after — только блочная форма») против `after_clause` с инлайн-формой
в `brig.ebnf` — решается в T-60 (#38). Иерархия восстановлена по
[`AUDIT_REPORT.md`](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT.md) §8: исходного `AUDIT_PROMPT.md` в репозитории нет.

## Принципы §0 (нарушать нельзя без явного решения по дизайну)

1. Лаконичность без двусмысленности.
2. Один способ для одной задачи.
3. Явное лучше неявного.
4. `nil` в языке нет — Option/Result/Unit.
5. Акторы — рантайм, модули — неймспейсы. Не смешивать.
6. Indent-driven код, скобки — для данных.
7. Функции — identity equality; записи — гибридно.
8. Рост очереди виден (`mailbox_size`) и ограничен (HWM).
9. `..` — одна идея («остаток/развёртка») везде.
10. Примеры в докстроках — валидный код, гоняются через `check-examples`.
11. VM гарантирует TCO, в том числе сквозь `ensure` (T-258).
12. Единственное присваивание, shadowing разрешён между областями.
13. Иммутабельность — языковая гарантия.

## Структура репозитория

```
cmd/                    # brig (CLI), check-examples, corpus
internal/
  lexer/                # токены + offside
  parser/               # recursive descent → AST
  ast/                  # узлы, форматтер, Equal, visitor
  loader/               # граф модулей из файлов (§11.1, T-135); компиляция графа — compiler.CompileProgram (T-137)
  sema/                 # контекстный анализ (§F.3)
  compiler/             # AST → регистровый байткод
  vm/                   # регистровая ВМ + scheduler + прелюдия + Verify
  runtime/              # Value, Kind, Equal, Json
  repl/                 # persistent REPL
  examples/             # A2-инструмент (check-examples)
  corpus/               # раннер корпуса (make corpus)
stdlib/                 # встроенные модули на Brig (List, Option, Result — T-146; Server, Supervisor — T-170; Observer.nodes — снимок для tree(), T-223); тесты — *_test.brig рядом
docs/                   # спецификация, дизайн VM, архитектура
examples/               # .brig-программы (make run-examples)
corpus/                 # код библиотек + manifest.tsv (make corpus, T-115)
testdata/               # golden, bytecode, negative, positive
.claude/skills/         # skills для агентов (этот файл и соседи)
WORKFLOW.md             # правила и команды работы с доской, ветками, PR
tasks/                  # карта плана: milestones, decisions, архив волн 0–15
```

## Exit-коды CLI (`cmd/brig`)

- `0` — ok
- `1` — ошибка парсинга/sema/пользовательский compile (в т.ч. «срез: …»)
- `2` — runtime uncaught raise
- `3` — внутренняя ошибка (сообщения с префиксом `internal:`)

Классификация в `cmd/brig` (T-45, #57): compile/`срез` → `1`;
`internal:` → `3`; uncaught raise → `2`. A-F7 (3): `runModule` /
`runModuleErr` прогоняют sema (T-46, #58). Комментарий
про exit-коды в `cmd/brig/main.go:27` ссылается на несуществующий skill
(чистится в T-60, #38).

## Что НЕ делать без явного запроса пользователя

- Не начинать новую крупную миграцию (типа Sprint 7) параллельно с другой
  большой задачей.
- Не менять AST-формы «попутно» с правкой байткода/VM.
- Не трогать `Regex` (§3.3) — отложен, ждёт решения по движку.
- Не переписывать `check-examples` целиком — он обязан давать
  `failed 0`, не ломать.
- Не добавлять правила в `check-smallint` без необходимости.
- Не путать `Pos()/End()` в `ast.Node`: по факту это `(Line, Col)`, а не
  байтовые смещения (см. `sema.posOf`) — это исторический артефакт
  именования, не баг для исправления походя.
- Не чинить findings из [`AUDIT_REPORT.md`](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT.md) вне их issue — даже если
  «рядом и просто».

## Прежде чем предлагать план

1. Найти issue задачи и прочитать её DoD и «НЕ делать» (`brig-workflow`).
2. Определить затронутые подсистемы и подключить их skills
   (`brig-lexer`, `brig-parser-ast`, `brig-sema`, `brig-compiler`,
   `brig-vm`, `brig-testing-workflow`).
3. Проверить §16 (Should/Nice/Не надо) — если фича там, явно сообщить её
   статус, а не реализовывать произвольно.
4. Если задача меняет семантику языка или публичный синтаксис —
   `docs/01-language-design.md` должен измениться синхронно
   (`WORKFLOW.md` §6), но агент doc-файлы вне docs-задач не правит
   (§7): остановиться и описать нужную правку спеки в issue.
