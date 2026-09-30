# Wave 16 — противоречия и контуры (третий аудит)

[← карта плана](README.md)

**Откуда:** третий аудит ([AUDIT_REPORT-3.md](../AUDIT_REPORT-3.md)),
слои P, S, G, F, D; промпт — [AUDIT_PROMPT-3.md](../AUDIT_PROMPT-3.md).

**Вход:** `main` @ `77c0cea` или новее. Задачи волны от DD третьего аудита
(T-250…T-259) не зависят и берутся сразу.

**Зачем:** второй аудит поставил контуры первыми, но на деле они влиты
после кода волн 9–15 (P-15), а два главных — `brig test` (F-4) и
`brig check` (F-8) — не работают за пределами простых случаев (G-17,
G-18). Прежде чем менять коллекции, ящик и синтаксис (волны 17–18), нужно,
чтобы каждое изменение сразу проверялось:
- тестами и доктестами пользовательского проекта (T-244, T-245);
- `brig check` — на несвязанных именах и script-файлах (T-242, T-243);
- асимптотикой коллекций (T-247) и корпусом диагностик (T-248);
- корпусом, который измеряет язык, а не отсутствие фреймворков (T-249);
- честными метками `needs`/`pending` (T-246).

Заодно чинится единственный молча неверный результат, найденный вне
рантайма, — повторные ключи в литерале `Map` (T-241), и спека догоняет
код (T-240).

**Выход:**
- `brig test` проходит в проекте из нескольких модулей, доктест отвечает
  записью своего типа;
- `brig check` ловит несвязанное имя и понимает `script`;
- `make bench-scaling` и корпус диагностик в CI (первый — информационно
  до волны 18);
- `make corpus` — число файлов на уровне `check` ≥ 12, `needs` без
  закрытых задач;
- `brig.ebnf` и §B.1/§2.2 описывают `<>`; `make check-examples` —
  `pending 0`.

## Перед Wave 16 — действия мейнтейнера

Работа на доске и в процессных документах; агенту не отдаётся.

1. Ревью `AUDIT_REPORT-3.md` и решения по DD T-250…T-259
   ([decisions.md](decisions.md)); после ревью — завести issues волн
   16–19 из блоков.
2. DD T-174 (#291): записать выбранный вариант комментарием (закрыт без
   решения, P-16). T-237 (#373) — добавить на доску. DD T-190…T-192
   (#294–#296) — перевести в Backlog (`WORKFLOW.md` §2.2).
3. DD T-225 (#339): закрыть — ограничение снято T-138 (S-15, проба в
   отчёте); абзац §6.2 удаляет T-240.
4. `WORKFLOW.md` (docs-PR): §7.2 привести к фактическому режиму —
   автономный merge при зелёном CI для всего, кроме DD (P-18); §7.3
   и §8 — убрать историю Wave 0 / T-07 / `iter/regvm` в архивный абзац
   (P-20).
5. Разово удалить смерженные worktree: `git worktree list` → для веток,
   чьи `[T-NN]` есть в `git log origin/main`, `git worktree remove
   <path>` (P-17, сейчас 148 деревьев, 1.9 ГБ).

## Порядок и параллельность

Ни одна задача волны не трогает `internal/compiler/compiler.go`.
T-241 правит `internal/vm/scheduler.go` — одна в волне. T-243 и T-244
обе правят `cmd/brig` — по одной, T-243 первой.

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-240 | Docs: спека, грамматика и доки догоняют код | — | sonnet | medium | всё |
| 2 | T-241 | Литерал `Map` схлопывает повторные ключи | — | sonnet | low | всё |
| 3 | T-242 | sema: ссылка на несвязанное имя — ошибка компиляции | — | opus | medium | T-240, T-241, T-246…T-248 |
| 4 | T-243 | `brig check`: режим `script`, корень проекта, info о `main` | — | opus | medium | T-240, T-241, T-246…T-248 |
| 5 | T-244 | `brig test`: программа через загрузчик модулей | T-243 | opus | medium | T-240, T-241, T-246…T-248 |
| 6 | T-245 | Доктесты: ответ видит типы модуля, функции модуля главнее хелперов | T-244 | opus | medium | T-246…T-248 |
| 7 | T-246 | Контуры: закрытые задачи в `needs`/`pending`, гигиена меток | — | sonnet | medium | всё |
| 8 | T-247 | `make bench-scaling`: асимптотика коллекций | — | sonnet | low | всё |
| 9 | T-248 | Корпус диагностик | — | sonnet | medium | всё |
| 10 | T-249 | Корпус: приложения аудита и заглушки горизонта | T-243, T-244 | sonnet | medium | T-245…T-248 |

## Задачи

### T-240 · Docs: спека, грамматика и доки догоняют код
<!-- meta
priority: P1
type: docs
effort: medium
model: sonnet
wave: 16
depends_on: —
findings: S-13, S-15, S-16, S-17, S-18, P-20, D-1 (AUDIT_REPORT-3)
-->
- **Файлы:** `brig.ebnf`; `docs/01-language-design.md` — шапка, §2.2, §3.2, §6.2 («Ограничение реализации»), §6.3, §8.1, §9.3, §9.6, §17 (упоминания «Трек B», «v0.4.7»), §B.1, §C.2, §D.4, Part III §L (записи T-233, T-171, T-173); `README.md`; `docs/architecture.md`; `internal/examples/spec_tables_test.go`
- **Тест-якорь:** создать `TestSpecOperatorsMatchLexer` в `internal/examples/spec_tables_test.go`: операторы §B.1 = операторы лексера = операторные терминалы `brig.ebnf`; список продолжений §2.2 = §D.4 = `continuationOps` (`internal/lexer/token.go`). До правки падает на `<>`.
- **DoD:**
  - `brig.ebnf`: уровень `concat_expr` (`<>`, право-ассоциативный) между `pipe_expr` и `range_expr`; в `pattern_atom` — `string_lit "<>" pattern`; строка-заголовок без «v0.4.7»;
  - §2.2, §D.4, §B.1 содержат `<>`; `go test ./internal/examples -run TestSpecOperatorsMatchLexer` → ok;
  - §6.2: абзац «Ограничение реализации» удалён, пример блочной лямбды в аргументе вызова — блок `brig`, проходит `make check-examples`;
  - §8.1: якорь `else` — `stmt_indent` (как §2.4);
  - §3.2 и §C.2: физический перенос внутри `\(...)` — ошибка лексера `unclosed interpolation`; HTML-комментарий §3.2 о «известном ограничении» удалён;
  - три блока `pending(T-126)` (§6.3, §9.3, §9.6) исполняются или переведены в `text` с причиной; `make check-examples` → `failed 0, pending 0`;
  - шапка без «Целевой этап: Трек B»; §17 без «Трек B» и «v0.4.7»; §L дополнен пунктами T-233 (`<>`), T-171 (`Behavior`), T-173 (TCO сквозь `ensure`);
  - README: пример программы с `module Main` и запуск `brig hello.brig`; версия спеки без номера (ссылка на шапку спеки); `make fuzz` без «60s»; структура с `sema`, `loader`, `stdlib/`, `corpus/`;
  - `architecture.md`: слои с `loader` и stdlib, режимы с `script`, REPL — порция ввода (§11.4), строка «`Serialize()` вызывается…» удалена, статус — ссылка на `tasks/`;
  - `make all` → 0.
- **НЕ делать:** менять §0 и §16 (DD T-258, T-259); переносить Part III (DD T-259); менять поведение лексера, парсера и компилятора; править `AUDIT_REPORT*.md`.

### T-241 · Литерал `Map` схлопывает повторные ключи
<!-- meta
priority: P1
type: full-fix
effort: low
model: sonnet
wave: 16
depends_on: —
findings: G-16 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/vm/scheduler.go` (опкод `MAP`), `internal/vm/*_test.go`
- **Тест-якорь:** создать `TestMapLiteralDuplicateKeys` в `internal/vm`: `%{"a" => 1, "a" => 2}` и `%{1 => :int, 1.0 => :float}`.
- **DoD:**
  - `%{"a" => 1, "a" => 2} == %{"a" => 2}`, `len` — 1 (правый побеждает, §5.2);
  - `%{1 => :int, 1.0 => :float}` — одна пара, значение `:float` (§4.8: `1` и `1.0` — один ключ);
  - вывод `Inspect` пары — в порядке первого появления ключа, как у `Map.put`;
  - спред-путь (`MAPSPREAD`) и `Map.put` не меняются, их тесты зелёные;
  - `make all` → 0; `BRIG_VERIFY=1 go test ./...` → 0.
- **НЕ делать:** менять представление `Map` (DD T-250); менять `runtime.KeyEqual`; трогать `Set` и `set()`.

### T-242 · sema: ссылка на несвязанное имя — ошибка компиляции
<!-- meta
priority: P1
type: full-fix
effort: medium
model: opus
wave: 16
depends_on: —
findings: G-17, D-2 e02 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/sema/names.go`, `internal/sema/sema_test.go`, `cmd/brig/names_test.go`, `.claude/skills/brig-sema/SKILL.md`
- **Тест-якорь:** создать `TestUnboundName` в `internal/sema/sema_test.go`: `fn main() -> print(y)`; `f = x -> x + zz`; ссылка в теле локальной `fn` на связанное ниже имя.
- **DoD:**
  - `brig check` на `module Main⏎fn main() ->⏎    print(y)` → exit 1, `error: <file>:3:11: undefined name y` (§E, §F.3);
  - имя, связанное в охватывающей области, параметр, имя из паттерна, локальная `fn` (вперёд-ссылки разрешены, §6.5), функция модуля, прелюдии и видимого модуля — не ошибка;
  - REPL и ввод `-i` не проверяются (§F.3: в REPL — рантайм);
  - `make check-examples` → `failed 0`; `make corpus` → ok без понижения уровней; `brig test stdlib` → 0 failed;
  - `make all` → 0.
- **НЕ делать:** менять текст существующих ошибок `undefined function`; трогать компилятор и VM; проверять имена в REPL.

### T-243 · `brig check`: режим `script`, корень проекта, info о `main`
<!-- meta
priority: P2
type: full-fix
effort: medium
model: opus
wave: 16
depends_on: —
findings: G-21, G-27, D-1 (AUDIT_REPORT-3)
-->
- **Файлы:** `cmd/brig/main.go`, `cmd/brig/file.go`, `cmd/brig/cli_test.go`, `internal/loader`, `internal/sema` (info), `docs/01-language-design.md` §11.3 (фраза об info)
- **Тест-якорь:** создать `TestCheckScriptMode` и `TestScriptMainNotCalledInfo` в `cmd/brig/cli_test.go`; `TestCheckProjectRoot` на `corpus/lookout/lib/lookout/monitors.brig`.
- **DoD:**
  - `brig check` выбирает режим так же, как `brig <file>`: файл без `module` — `script` (§11.3); корректный script — `ok`, exit 0;
  - файл без `module`, в котором есть `fn main()` и нет вызова `main`: `info: <file>:<line>:<col>: script defines fn main() but never calls it; add module Main to run it` в stderr, exit по-прежнему 0 (§E.3); §11.3 содержит это правило;
  - `brig check <модуль проекта>` берёт корень по `project.brig` вверх от файла, как `brig -i .`; без `project.brig` — каталог файла (§11.1); `brig check corpus/lookout/lib/lookout/monitors.brig` не даёт `module Lookout.Repo not found`;
  - `brig <file>` не меняется;
  - `make all` → 0.
- **НЕ делать:** вызывать `main` в script автоматически; менять правило выбора режима запуска; менять `-i`.

### T-244 · `brig test`: программа через загрузчик модулей
<!-- meta
priority: P1
type: full-fix
effort: medium
model: opus
wave: 16
depends_on: T-243
findings: G-18 (AUDIT_REPORT-3)
-->
- **Файлы:** `cmd/brig/test.go`, `cmd/brig/test_test.go`, `internal/loader`, `.claude/skills/brig-testing-workflow/SKILL.md`
- **Тест-якорь:** создать `TestBrigTestImportsProjectModule` в `cmd/brig/test_test.go`: каталог с `calc.brig` (`module Calc`, `pub fn add`, доктест) и `calc_test.brig` (`import Calc`, `fn test_add`), плюс модуль с `##`, который импортирует `Calc`.
- **DoD:**
  - тестовый файл и файл с `##` компилируются как программа загрузчиком (`internal/loader` + `compiler.CompileProgram`), корень — как у `brig check` (T-243);
  - проба теста-якоря: `ok … test_add`, доктесты модуля с импортом исполняются, exit 0;
  - отсутствующий модуль — `module X not found` с `line:col`, а не `undefined function X.f`;
  - `brig test stdlib` → `51 passed, 0 failed` или больше; `make corpus` — ок;
  - `make all` → 0.
- **НЕ делать:** `Test.isolated`; параллельный запуск; менять формат вывода и exit-коды §11.6.

### T-245 · Доктесты: ответ видит типы модуля, функции модуля главнее хелперов
<!-- meta
priority: P1
type: full-fix
effort: medium
model: opus
wave: 16
depends_on: T-244
findings: G-19, R-9 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/examples/examples.go` (`runRepl`), `internal/examples/doctest.go`, `internal/repl` (разрешение имён хелперов), `docs/01-language-design.md` §11.4 («Затенение»), §11.6 («Доктесты»)
- **Тест-якорь:** создать `TestDoctestUserTypeAnswer` и `TestDoctestModuleFnShadowsHelper` в `internal/examples/doctest_test.go`; вход — `semver.brig` из приложения A `AUDIT_REPORT-3.md`.
- **DoD:**
  - ответ доктеста вычисляется на ВМ, где загружены декларации модуля файла (типы, конструкторы, функции) и нет привязок ввода; `## > parse("1.2.3")` / `## Ok(Version{ major: 1, minor: 2, patch: 3, pre: [] })` — ok;
  - функция загруженного модуля с именем хелпера `Repl` (`v`, `h`, `i`, `load`, `time`, `info`, `top`, `tree`, …) затеняет хелпер во вводе доктеста и в сессии `-i`; хелпер доступен как `Repl.v`; info — как у связывания (§11.4);
  - §11.6 и §11.4 описывают оба правила;
  - REPL-блоки спеки (§G.5) по-прежнему считают ответ на чистой сессии: `make check-examples` → `failed 0`;
  - `brig test` на `semver.brig` из приложения A: все 5 доктестов ok (после переименования `ver`/`con` обратно в `v`/`c` — тоже);
  - `make all` → 0.
- **НЕ делать:** `Test.isolated`; менять сравнение `==`; менять набор хелперов.

### T-246 · Контуры: закрытые задачи в `needs`/`pending`, гигиена меток
<!-- meta
priority: P1
type: test-infra
effort: medium
model: sonnet
wave: 16
depends_on: —
findings: F-9, S-18, P-17, P-19 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/corpus/corpus.go`, `cmd/corpus`, `internal/examples/examples.go` (`LoadTasks`), `Makefile`, `.github/workflows/ci.yml`, `corpus/manifest.tsv` и строки `# needs:` в файлах корпуса, `.claude/skills/brig-overview/SKILL.md`, `.claude/skills/brig-testing-workflow/SKILL.md`, `.claude/skills/brig-workflow/SKILL.md`
- **Тест-якорь:** создать `TestClosedTaskInNeeds` (`internal/corpus`) и `TestPendingClosedTask` (`internal/examples`) на фиктивном списке закрытых задач.
- **DoD:**
  - `make markers-check ONLINE=1`: каждая ссылка `needs` и `pending(T-NNN)` на закрытый issue (по titles `gh issue list --state closed`) печатается, exit 1; без `ONLINE` — не проверяется;
  - job `markers` в `ci.yml` на PR: из title PR берётся `[T-NNN]`; если `T-NNN` остался в `needs` манифеста или в `pending(…)` спеки — красный;
  - из `needs` манифеста и файлов корпуса сняты закрытые задачи (T-133, T-135, T-137, T-144, T-147, T-155, T-163…T-168); `make corpus` → ok, в «top needs» нет закрытых задач;
  - skills: счётчики `checked N, pending N` заменены на «`failed 0`»; в «Финиш» `brig-workflow` — шаг `git worktree remove <path>` после merge;
  - `make all` → 0.
- **НЕ делать:** заводить issues автоматически; поднимать уровни файлов вручную (только `make update-corpus`); править спеку (T-240).

### T-247 · `make bench-scaling`: асимптотика коллекций
<!-- meta
priority: P1
type: test-infra
effort: low
model: sonnet
wave: 16
depends_on: —
findings: F-11, X-1 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/vm/bench_test.go`, `Makefile`, `.github/workflows/bench.yml`, `.claude/skills/brig-testing-workflow/SKILL.md`
- **Тест-якорь:** создать `BenchmarkScaling/{list_prepend,map_put,vec_push}/{1k,8k}` в `internal/vm/bench_test.go` — коллекция строится Brig-кодом в хвостовой рекурсии (`[n, ..acc]`, `Map.put`, `Vec.push`).
- **DoD:**
  - `make bench-scaling` печатает t(8k)/t(1k) по каждой операции и завершается 1, если отношение > 16 (квадратичный рост; линейный — ~8);
  - на текущем `main` цель красная (X-1: ~17); job в `bench.yml` с `continue-on-error: true` и пометкой «до T-271…T-273»;
  - `make bench` (порог 5 %, T-152) не меняется;
  - `make all` → 0.
- **НЕ делать:** чинить коллекции; менять порог T-152; добавлять макро-бенчмарки.

### T-248 · Корпус диагностик
<!-- meta
priority: P2
type: test-infra
effort: medium
model: sonnet
wave: 16
depends_on: —
findings: F-12, D-2 (AUDIT_REPORT-3)
-->
- **Файлы:** `testdata/diagnostics/*.brig`, `cmd/brig/diag_test.go`, `.claude/skills/brig-testing-workflow/SKILL.md`
- **Тест-якорь:** создать `TestDiagnosticsCorpus` в `cmd/brig/diag_test.go`.
- **DoD:**
  - не меньше 28 файлов — программы e01…e28 и m1 из D-2 `AUDIT_REPORT-3.md` и их аналоги; первая строка — `# expect: <команда check|run> <exit> <line>:<col> <подстрока>`;
  - тест запускает `brig check` или `brig` и сверяет exit, позицию и подстроку;
  - 8 плохих сообщений из таблицы D-2 записаны с желаемым текстом и `# pending: T-265`; тест ожидает несовпадение и падает на неожиданном совпадении (как корпус);
  - `go test ./cmd/brig -run TestDiagnosticsCorpus` → ok; `make all` → 0.
- **НЕ делать:** чинить сообщения (T-265); менять формат §E; проверять тексты `raise` целиком.

### T-249 · Корпус: приложения аудита и заглушки горизонта
<!-- meta
priority: P2
type: test-infra
effort: medium
model: sonnet
wave: 16
depends_on: T-243, T-244
findings: F-10 (AUDIT_REPORT-3)
-->
- **Файлы:** `corpus/apps/kv/`, `corpus/apps/csv/`, `corpus/apps/semver/` (программы приложения A `AUDIT_REPORT-3.md`), `corpus/_horizon/` (модули-заглушки), `corpus/manifest.tsv`, `internal/corpus/corpus.go` (корень проекта и путь заглушек), `cmd/brig/http_test.go` (e2e KV)
- **Тест-якорь:** `make corpus`; создать `TestCorpusKvE2E` в `cmd/brig/http_test.go`: PUT, GET, TTL, `/stats`, SIGTERM → exit 0.
- **DoD:**
  - `corpus/apps/csv` и `corpus/apps/semver` — уровень `run` с `X.out`; `corpus/apps/kv` — уровень `check` + e2e-тест;
  - `corpus/_horizon/`: модули `Calmar.*`, `Sql`, `Http.Client`, `Env`, `Cli`, `Whelk`, `Changeset` и остальные, которых ждут файлы `lookout/` и `whelk/`, — `pub fn` с нужными арностями, тело `raise((:horizon, :<module>))`; корпус подключает их как дополнительный корень поиска модулей только для `check`;
  - у каждого fail-файла `needs` — задачи, которые реально блокируют его уровень; `horizon` — только там, где нужен рантайм горизонта;
  - `make corpus` → ok; в сводке файлов на уровне `check` и `run` вместе ≥ 12 (сейчас 6);
  - `make all` → 0.
- **НЕ делать:** реализовывать модули горизонта; менять `web-mvp-research/demo`; править код языка и stdlib.
