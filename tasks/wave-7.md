# Wave 7 — контуры обратной связи и согласование

[← карта плана](README.md)

**Откуда:** второй аудит ([AUDIT_REPORT-2.md](../AUDIT_REPORT-2.md)), слои P и F.

**Вход:** Wave 6 закрыта. До старта мейнтейнер выполняет действия из
`README.md` («Перед Wave 7»).

**Зачем:** прежде чем менять язык, нужно, чтобы каждое изменение сразу
проверялось на трёх вещах:
- на коде библиотек — корпус;
- на спеке — исполняемые примеры;
- на плане — `plan-check`.

Всё здесь дёшево и не трогает семантику языка.

**Выход:**
- `make all` в CI гоняет `check-examples` (с компиляцией блоков),
  `run-examples` с эталонным выводом, `corpus`, `plan-check` и сверку
  таблиц спеки с кодом;
- fuzz идёт ночью, `-race` — на PR;
- процессные документы не ссылаются на удалённые файлы;
- у каждого файла корпуса есть ожидаемый уровень и список `needs`.

## Порядок и параллельность

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-110 | Процессные документы после удаления журнала | — | sonnet | low | всё |
| 2 | T-112 | Хук commit-msg ↔ CONTRIBUTING | — | sonnet | low | всё |
| 3 | T-111 | `make plan-check` | T-110 | sonnet | medium | T-113…T-119 |
| 4 | T-113 | Примеры спеки и `examples/` в `make all` | — | sonnet | low | T-111, T-114 |
| 5 | T-114 | CI: ночной fuzz и `-race` на PR | — | sonnet | low | всё |
| 6 | T-115 | Корпус библиотечного кода и `make corpus` | T-113 | opus | large | T-116, T-118 |
| 7 | T-116 | `check-examples`: компиляция блоков, `pending(T-NN)`, причина `invalid` | T-113 | opus | medium | T-115, T-118 |
| 8 | T-117 | `check-examples`: исполнение REPL-блоков | T-116 | opus | medium | T-118 |
| 9 | T-118 | Сверка таблиц спеки с кодом | T-113 | sonnet | medium | T-115…T-117 |
| 10 | T-119 | Устаревшие doc 02, architecture, skills, README | — | sonnet | low | всё |

Ни одна задача волны не трогает `internal/compiler/compiler.go`.

## Задачи

### T-110 · Docs: процессные документы после удаления журнала доски
<!-- meta
priority: P1
type: docs
effort: low
model: sonnet
wave: 7
depends_on: —
findings: P-4, P-5, P-8, P-13, P-14, S-12 (AUDIT_REPORT-2.md)
-->
- **Файлы:** `MAINTAINING.md:15,17,196`; `.claude/skills/brig-workflow/SKILL.md:54`; `CONTRIBUTING.md:103` (абзац «Апрувы»), §6 и §7 (docs в feature-задачах), `MAINTAINING.md` §7 («Задача требует менять спецификацию»); комментарии в issues #169 и #166 (через `gh issue comment`)
- **Тест-якорь:** — (docs); проверки из DoD
- **DoD:**
  - `rg -n 'PROMPT_SETUP_KANBAN|TASKS_EXAMPLE' --glob '!AUDIT_*.md' --glob '!tasks/wave-7.md'` пуст;
  - правило «следующий свободный T-NN» в `MAINTAINING.md` и `brig-workflow` одно и то же: `max(номера в titles issues, номера в tasks/) + 1` внутри десятка волны, со ссылкой на `make plan-check` (T-111), пока его нет — на команду `gh issue list --state all --json title`;
  - `CONTRIBUTING.md` §5 «Апрувы»: текущее правило (self-merge при зелёном CI) и правило «team: 2+» разделены двумя пунктами, противоречия внутри абзаца нет;
  - `CONTRIBUTING.md` §6 и §7 согласованы: feature-задача правит разделы спеки, перечисленные в её «Файлах» (таблицы прелюдии, снятие `pending`); новая семантика — только docs-задача; `MAINTAINING.md` §7 описывает то же;
  - в #169 оставлен комментарий: «Файлы» дополняются §F.2 (`fn -> expr`), §G.8 п.3 и §I.1 B2 (S-12) и ссылкой на T-138 вместо «#2, A5.4» (P-13);
  - в #166 оставлен комментарий со ссылкой на `AUDIT_REPORT-2.md` P-1/P-5: `backlog.md` удалён, волны 7+ перепланированы.
- **НЕ делать:** править тела issues, спеку, `tasks/wave-[0-6].md`, `AUDIT_REPORT.md`; менять сами правила процесса сверх устранения противоречий.

### T-111 · Build: `make plan-check` — согласованность плана
<!-- meta
priority: P2
type: test-infra
effort: medium
model: sonnet
wave: 7
depends_on: T-110
findings: P-1, F-7
-->
- **Файлы:** `scripts/plan-check/` (Go, `package main`, создать) или `internal/plancheck/` + `cmd/`; `Makefile` (цель `plan-check`, в `all`); `.github/workflows/ci.yml` — не менять, если `plan-check` входит в `all`
- **Тест-якорь:** создать тесты на фикстурах: `TestPlanCheckDuplicateNumber`, `TestPlanCheckDependsOnLarger`, `TestPlanCheckMissingLink`, `TestPlanCheckOnlineCollision` (онлайн-часть — на подставном списке titles)
- **DoD:**
  - офлайн-проверки (без сети, входят в `make all`): каждый `### T-NNN ·` в `tasks/*.md` уникален; `depends_on` ссылается только на меньшие номера, которые есть в `tasks/` (строки таблиц волн 0–6 тоже считаются); относительные ссылки из `tasks/*.md` ведут на существующие файлы; у каждого блока есть meta, «Файлы», «Тест-якорь», «DoD», «НЕ делать»;
  - онлайн-режим `make plan-check ONLINE=1`: список titles issues (`gh`) — ни один `T-NNN` блока без ссылки на issue не совпадает с номером в title issue; для строк таблицы со ссылкой на issue номер в title совпадает;
  - на текущем `tasks/` оба режима дают 0; вывод онлайн-режима — в body PR;
  - `make all` → 0.
- **НЕ делать:** ходить в сеть в `make all`; менять формат блоков; править `tasks/` сверх того, что найдёт проверка (найденное — отдельным коммитом в том же PR, если это опечатка, иначе issue).

### T-112 · Chore: хук commit-msg и CONTRIBUTING §4 — одни правила
<!-- meta
priority: P3
type: full-fix
effort: low
model: sonnet
wave: 7
depends_on: —
findings: P-7
-->
- **Файлы:** `.githooks/commit-msg:13`; `CONTRIBUTING.md` §4 (таблица типов, строка про scope)
- **Тест-якорь:** создать `scripts/test-commit-msg.sh` (или Go-тест над хуком через `exec`): набор subject'ов «принять/отвергнуть»
- **DoD:**
  - принимаются: `build(make): …`, `ci: …`, `fix(cmd/brig): … [T-45]`, `docs(tasks): …`; отвергаются: кириллица, > 72 символов, неизвестный тип, `Fix(parser): …`;
  - список типов в хуке и в таблице §4 совпадает (`ci` либо добавлен в §4, либо убран из хука — выбор в body PR);
  - `make git-hooks` ставит обновлённый хук; тест-скрипт → 0.
- **НЕ делать:** делать `[T-NN]` обязательным в хуке (CONTRIBUTING §4 говорит «не enforced»); менять `pre-push`.

### T-113 · Build: примеры спеки и `examples/` — в `make all`
<!-- meta
priority: P1
type: test-infra
effort: low
model: sonnet
wave: 7
depends_on: —
findings: P-6, P-10, F-2
-->
- **Файлы:** `Makefile:15` (`all`), `:83` (`run-examples` — в `.PHONY`), `:99`; `tools/check-examples/` (удалить); `examples/*.out` (создать); цель `update-examples`
- **Тест-якорь:** `make check-examples`, `make run-examples`
- **DoD:**
  - `make all` включает `check-examples` и `run-examples`; CI (`make all`) их гоняет;
  - `run-examples` сравнивает stdout каждого `examples/X.brig` с `examples/X.out` и падает с diff; `make update-examples` перезаписывает `.out`; `.out` сгенерированы и просмотрены глазами (отдельный коммит `test(examples): add expected output [T-113]`);
  - каталога `tools/check-examples` нет; `rg -n 'tools/check-examples' --glob '!docs/**' --glob '!AUDIT_REPORT*'` пуст (спека §G.2 правится в T-126);
  - `make all` → 0.
- **НЕ делать:** менять `internal/examples` (это T-116); добавлять новые примеры в `examples/` (корпус — T-115); менять язык ради примера.

### T-114 · CI: ночной fuzz и `-race` на PR
<!-- meta
priority: P2
type: test-infra
effort: low
model: sonnet
wave: 7
depends_on: —
findings: P-6 (было T-111 и T-112 в PR #167)
-->
- **Файлы:** `.github/workflows/fuzz.yml` (создать); `.github/workflows/ci.yml` (job `race`); `Makefile` (`fuzz`, `test-race`)
- **Тест-якорь:** `make fuzz`, `make test-race`
- **DoD:**
  - `fuzz.yml`: `schedule` раз в сутки + `workflow_dispatch`, гоняет `make fuzz`; найденный вход сохраняется артефактом;
  - в `ci.yml` job `race` параллельно `all`, запускает `make test-race` на PR в `main`;
  - локально `make test-race` → 0 и `make fuzz` → PASS трёх таргетов (вывод в body PR); падение — отдельный issue, задача в Blocked;
  - job `all` не меняется.
- **НЕ делать:** чинить найденное здесь; добавлять fuzz-таргеты компилятора/VM; увеличивать время `make all`.

### T-115 · Test-infra: корпус библиотечного кода и `make corpus`
<!-- meta
priority: P1
type: test-infra
effort: large
model: opus
wave: 7
depends_on: T-113
findings: F-1, R-2
-->
Главный контур «библиотеки → язык». Корпус — код, написанный так, как
будут писать авторы stdlib, Whelk и Calmar. Каждый файл объявляет, на
каком уровне он должен проходить сейчас и каких задач ждёт. CI падает
в двух случаях: файл деградировал, или файл прошёл, хотя по манифесту
ещё ждёт задачу, а метку никто не снял.

- **Файлы:**
  - `corpus/` (создать): `corpus/whelk/hook.brig`, `corpus/lookout/**` — перенесены из `web-mvp-research/demo-whelk/` и `web-mvp-research/demo/` через `git mv`; в research — ссылки на новые пути;
  - `corpus/lang/` — программы по примерам спеки, которые должны работать: pipe-map, `with`, записи;
  - `corpus/manifest.tsv` (путь, уровень `parse|check|run`, ожидание `pass|fail`, `needs`);
  - раннер — Go-тест `internal/corpus/corpus_test.go` или `cmd/corpus`;
  - `Makefile` (`corpus`, `update-corpus`, в `all`).
- **Тест-якорь:** `make corpus`; тесты раннера на фикстурах: `TestCorpusRegression`, `TestCorpusUnexpectedPass`, `TestCorpusNeedsUnknownTask`
- **DoD:**
  - уровни: `parse` — лексер и парсер; `check` — `brig check` (sema и компиляция, после T-139 — имена); `run` — `brig run` с кодом 0 и stdout, равным `X.out`, если он есть;
  - у каждого файла корпуса есть строка манифеста; `# NEEDS:` заменены на `# needs: T-NNN, …` с номерами из `tasks/` (или issues); свободный текст причины — в комментарии рядом;
  - `make corpus` падает на регрессии (ожидался `pass` — получен `fail`), на неожиданном проходе (ожидался `fail` с `needs` — получен `pass`: снять метку и обновить манифест) и на `needs` с несуществующим T-NNN;
  - `make corpus` печатает сводку: файлов по уровням и топ задач по числу заблокированных файлов — это вход для приоритизации волн;
  - `make all` → 0.
- **НЕ делать:** переписывать demo под текущие ограничения языка (он показывает, чего не хватает); реализовывать недостающие модули заглушками; удалять research-тексты 00–23; тянуть в корпус `.bt` (шаблонов нет — только список в манифесте с `needs`).

### T-116 · Test-infra: `check-examples` компилирует блоки спеки
<!-- meta
priority: P1
type: test-infra
effort: medium
model: opus
wave: 7
depends_on: T-113
findings: F-2, S-2
-->
- **Файлы:** `internal/examples/examples.go` (режимы `module`/`stmt`/`expr` — после разбора прогнать sema и компилятор); `cmd/check-examples/main.go`; `docs/01-language-design.md` — только метки блоков, которые сейчас не компилируются (`brig pending(T-NNN)`), и §G.4 (новая метка); `internal/examples/examples_test.go`
- **Тест-якорь:** создать `TestCheckBlockCompiles`, `TestCheckBlockPendingNeedsTask`, `TestCheckInvalidReason`
- **DoD:**
  - блоки `module`/`stmt`/`expr` проходят sema и компиляцию; ошибка компиляции — FAIL с `line:col` в markdown;
  - новая метка `brig pending(T-NNN)`: блок обязан **не** компилироваться (иначе FAIL «снять pending») и ссылается на существующий T-NNN;
  - `brig invalid` принимает необязательную ожидаемую подстроку ошибки: ```` ```brig invalid "expected expression" ````; блок, который падает по другой причине, — FAIL (S-2: блок §3.2 падает из-за top-level `let`, а не из-за `if`);
  - примеры спеки, которые сейчас не компилируются (как минимум §4.1 `(a, b, c) = t`), помечены `pending` с номером задачи;
  - `make check-examples` → `failed 0`; число `pending` — в body PR.
- **НЕ делать:** исполнять блоки (T-117); менять текст спеки сверх меток и §G.4; переписывать `check-examples` целиком (skill `brig-overview`); помечать `pending` блок, который падает из-за бага, а не из-за нереализованной фичи (такой — issue).

### T-117 · Test-infra: `check-examples` исполняет REPL-блоки
<!-- meta
priority: P2
type: test-infra
effort: medium
model: opus
wave: 7
depends_on: T-116
findings: F-2, G-12
-->
- **Файлы:** `internal/examples/examples.go` (режим `repl`); `internal/repl/repl.go` (API прогона строк без терминала, если его нет); `docs/01-language-design.md` §G.5 `repl` (правило сравнения)
- **Тест-якорь:** создать `TestReplBlockExecutes`, `TestReplBlockMismatch`, `TestReplBlockRaise`
- **DoD:**
  - строки `> expr` исполняются в одной REPL-сессии; строка ответа разбирается как выражение и сравнивается с результатом через `==` (формат доктестов research 22); строка `raise <term>` сопоставляется с непойманным raise;
  - блок §11.4 (снимок замыкания `f() == 5`) проходит; нарочно испорченная копия в тесте — FAIL с ожидаемым и фактическим значением;
  - строки без ответа (связывания) не сравниваются;
  - `make check-examples` → `failed 0`.
- **НЕ делать:** менять формат вывода REPL для людей (G-12 — только сравнение через `==` здесь); вводить доктесты в `##` (T-147); исполнять `module`-блоки.

### T-118 · Test-infra: сверка таблиц спеки с кодом
<!-- meta
priority: P2
type: test-infra
effort: medium
model: sonnet
wave: 7
depends_on: T-113
findings: F-5, S-9, P-12
-->
- **Файлы:** новый тест `internal/examples/spec_tables_test.go` (или `internal/spectables`); `Makefile:102` (`ebnf-check` — заменить на запуск этого теста или удалить цель)
- **Тест-якорь:** создать `TestSpecKeywordsMatchLexer`, `TestSpecPreludeMatchesInstall`, `TestSpecAutoRaiseNames`, `TestSpecPipeForbiddenPrimitives`
- **DoD:**
  - ключевые слова §1.3 = §B.1 = ключевые слова лексера = keywords в `brig.ebnf`;
  - имена прелюдии §11.5 и модули прелюдии ⊆ `InstallPrelude` + опкоды акторных примитивов; лишние имена в коде перечислены в тесте явным allowlist с комментарием «нет в спеке — T-NNN»;
  - каждое имя авто-raise из §10.4 встречается в коде, где бросается; каждый атом, который код бросает как первый элемент авто-raise, есть в §10.4 или в allowlist с номером задачи;
  - сейчас в allowlist как минимум `:field_error` (T-121), `:guard_failed` отмечен «не бросается — T-131»;
  - `make all` → 0.
- **НЕ делать:** править спеку или код, чтобы тест прошёл (расхождение — allowlist с номером задачи); парсить EBNF целиком.

### T-119 · Docs: устаревшие doc 02, architecture, skills, README
<!-- meta
priority: P3
type: docs
effort: low
model: sonnet
wave: 7
depends_on: —
findings: P-9
-->
- **Файлы:** `docs/02-register-based-virtual-machine.md:34-42` (K-8, A-F8), `:866-904` (план Sprint 7, ссылки на `STATUS.md`); `docs/architecture.md:103,257` (`check-smallint` в `ci-quick`), «Статус реализации» (Wave 6); `.claude/skills/brig-overview/SKILL.md:116`, `brig-testing-workflow:90` (`checked 62`), `brig-compiler:132` (term order записей); `cmd/brig/main.go:73` (help `repl`)
- **Тест-якорь:** — (docs); проверки из DoD
- **DoD:**
  - K-8 и список A-F8 отражают текущее состояние (сделано — одной строкой со ссылкой на задачи Wave 6) или удалены; ссылок на `STATUS.md` нет (`rg -n STATUS.md docs` пуст); план Sprint 7 перенесён в историю (раздел «История» в конце doc 02 или удалён со ссылкой на git);
  - `architecture.md` не утверждает то, чего нет в `Makefile` (`check-smallint` в `ci-quick` — либо добавить в Makefile отдельным коммитом, либо убрать из текста; выбор в body PR);
  - число блоков в skills = фактический вывод `make check-examples`;
  - `make all` → 0.
- **НЕ делать:** трогать спеку (T-126); менять нормативные разделы doc 02 (K-1…K-7, опкоды); переписывать skills целиком.
