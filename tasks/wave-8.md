# Wave 8 — укрепление и повторный аудит

[← карта плана](README.md)

**Вход:** для T-110 — закрыты задачи Wave 7 с кодом (T-101, T-103, T-106, T-108); остальные задачи волны от Wave 7 не зависят и могут идти раньше. **Зачем:** первый аудит сделан на `8ab58cf`, а с тех пор компилятор вырос вдвое: `match`, `with`, записи, вариадики, pipe, модули. Часть областей первый аудит сам пометил как непокрытые (`AUDIT_REPORT.md` §9). **Выход:** новый отчёт аудита на свежем `main`; fuzz и `-race` гоняются в CI; у каждого примера в `examples/` есть эталонный вывод; все оставшиеся «срез:» классифицированы.

| T-NN | Название | depends_on | Тип | Источник |
|---|---|---|---|---|
| T-110 | Повторный аудит на свежем `main` | T-101, T-103, T-106, T-108 | test-infra | audit |
| T-111 | CI: ночной fuzz 3×60s | — | test-infra | audit |
| T-112 | CI: `go test -race` | — | test-infra | audit |
| T-113 | E2E: эталонный вывод для `examples/` и канонические программы | — | test-infra | audit |
| T-114 | Ревизия оставшихся «срез:» в компиляторе | — | test-infra | audit |
| T-115 | Лексер: `"""` → `multiline strings are not implemented` (§3.5) | — | full-fix | audit |
| T-116 | Docs: тест-фреймворк в спеке (по решению T-109) | T-109 | docs | spec-gap |
| T-117 | Docs: пример §13.2 — снять TODO и пометить как `brig` | T-108 | docs | spec-gap |

## Задачи

### T-110 · Повторный аудит на свежем `main`
<!-- meta
priority: P1
type: test-infra
effort: L
model: opus
wave: 8-hardening
depends_on: T-101, T-103, T-106, T-108
findings: —
-->
- **Файлы:** новый `AUDIT_REPORT-2.md` в корне (структура — как у `AUDIT_REPORT.md`: шапка с коммитом, findings по слоям с ID, сводная таблица, «Verification needed», «Что не покрыто»); код не меняется
- **Тест-якорь:** — ; пробы оформляются как тесты в задачах-фиксах, не здесь
- **DoD:**
  - шапка называет коммит `main`, на котором проведён аудит;
  - покрыты разделы, которые первый аудит пропустил (§9): мини-блоки офсайда §D.6/D.7, полный term order `runtime.Compare` и сортировка смешанных типов, REPL и переиспользование `Scheduler`, арность нативов прелюдии, `Test.*`, `Serialize`, `FormatDecimal`, sema для `match`/`with`/record (K-8), парсер типов `type.go`;
  - покрыт код, появившийся после `8ab58cf`: Wave 6 и Wave 7;
  - у каждого finding есть ID, уровень, файл:строка и пометка `[confirmed]` (с командой воспроизведения) или `[inferred]`;
  - для каждого `[confirmed]` finding в отчёте есть черновой блок задачи для `tasks/wave-10.md` (meta, Файлы, Тест-якорь, DoD, НЕ делать);
  - `rg -n '^\| ' AUDIT_REPORT-2.md` содержит сводную таблицу.
- **НЕ делать:** чинить findings в этой задаче; править `AUDIT_REPORT.md` (исторический отчёт); заводить issues до ревью отчёта человеком; повторять findings первого аудита, закрытые в волнах 0–6, без нового воспроизведения.

### T-111 · CI: ночной fuzz 3×60s
<!-- meta
priority: P2
type: test-infra
effort: S
model: sonnet
wave: 8-hardening
depends_on: —
findings: — (`AUDIT_REPORT.md` §9: `make fuzz` 3×60s ни разу не запускался)
-->
- **Файлы:** `.github/workflows/fuzz.yml` (создать); `Makefile:123` (`fuzz`); `internal/lexer`, `internal/parser`, `internal/ast` (`testdata/fuzz/` — только корпус падений)
- **Тест-якорь:** `make fuzz` (существующая цель)
- **DoD:**
  - workflow запускается по `schedule` (раз в сутки) и вручную (`workflow_dispatch`), гоняет `make fuzz`;
  - при падении найденный вход сохраняется артефактом job'а;
  - локальный прогон `make fuzz` — вывод трёх таргетов (PASS или найденные падения) в body PR; каждое падение — отдельный issue, не фикс в этом PR;
  - `ci.yml` (`make all` на PR) не меняется.
- **НЕ делать:** чинить найденные падения здесь; добавлять fuzz-таргеты для компилятора и VM (это отдельный issue, если нужен); увеличивать время `make all`.

### T-112 · CI: `go test -race`
<!-- meta
priority: P2
type: test-infra
effort: S
model: sonnet
wave: 8-hardening
depends_on: —
findings: — (`make test-race` есть, в CI не запускается)
-->
- **Файлы:** `.github/workflows/ci.yml` (отдельный job); `Makefile:29` (`test-race`)
- **Тест-якорь:** `make test-race` (существующая цель)
- **DoD:**
  - в `ci.yml` есть job `race`, параллельный `all`, который запускает `make test-race` на PR в `main`;
  - локальный `make test-race` → 0 (вывод в body PR); если есть гонки — PR не делается, заводится issue с выводом, задача в Blocked;
  - job `all` не меняется.
- **НЕ делать:** чинить гонки здесь; менять scheduler; отключать тесты под `-race`.

### T-113 · E2E: эталонный вывод для `examples/` и канонические программы
<!-- meta
priority: P2
type: test-infra
effort: M
model: sonnet
wave: 8-hardening
depends_on: —
findings: — (`make run-examples` проверяет только exit 0, но не вывод; в `examples/` 8 программ)
-->
- **Файлы:** `Makefile:83` (`run-examples`); `examples/*.brig`, `examples/*.out` (создать); новые примеры: `examples/actors_pingpong.brig`, `examples/watch_down.brig`, `examples/json_pipeline.brig`, `examples/multiclause_guard.brig`
- **Тест-якорь:** `make run-examples` сравнивает stdout каждого `examples/X.brig` с `examples/X.out`; цель `make update-examples` перезаписывает `.out`
- **DoD:**
  - у каждого `examples/*.brig` есть `.out`; расхождение → `make run-examples` падает с diff;
  - четыре новых примера написаны по спеке (§12, §10.5, §11.5 JSON, §6.1) и детерминированы: 20 прогонов подряд дают одинаковый вывод (`for i in $(seq 20); do …; done | sort | uniq -c` — одна строка на пример);
  - `.out` сгенерированы `make update-examples` и просмотрены глазами (отдельный коммит `test(examples): add expected output [T-113]`);
  - `make all` → 0.
- **НЕ делать:** менять язык или прелюдию, чтобы пример заработал (нужна фича — issue); примеры с таймингами, зависящими от часов; трогать `check-examples`.

### T-114 · Ревизия оставшихся «срез:» в компиляторе
<!-- meta
priority: P2
type: test-infra
effort: S
model: sonnet
wave: 8-hardening
depends_on: —
findings: —
extra_labels: verification
-->
- **Файлы:** `internal/compiler/compiler.go` — строки с `fmt.Errorf("срез:` (на `8cd9dbc`: `:613` параметр-паттерн в лямбде, `:1005` стейтмент, `:1011`/`:2269` связывания, `:1099` regex, `:1135` выражение, `:1347` унарный оператор, `:1377` оператор, `:1606` спред не последним, `:1612` спред-вызов локальной fn с захватом, `:1749` элемент мапы, `:1854` `Module.member`, `:2784`/`:2786` паттерн)
- **Тест-якорь:** — ; для каждого сайта — проба (минимальная программа) и её вывод
- **DoD:**
  - в issue — таблица по каждому сайту: достижим ли он из программы, которую пропускают парсер и sema (проба + вывод); к чему относится: Must-баг / Should (номер задачи Wave 9) / честное ограничение MVP / недостижимый код;
  - для каждого Must-бага и недостижимого сайта заведён issue (или добавлен черновой блок в `tasks/wave-10.md`, пока доска недоступна);
  - код не меняется.
- **НЕ делать:** чинить сайты здесь; менять текст ошибок; трогать regex (T-134).

### T-115 · Лексер: `"""` → `multiline strings are not implemented` (§3.5)
<!-- meta
priority: P3
type: full-fix
effort: S
model: sonnet
wave: 8-hardening
depends_on: —
findings: — (проба роадмапа: `s = """` → `unclosed string literal`; §3.5 и §B требуют `multiline strings are not implemented`)
-->
- **Файлы:** `internal/lexer/lexer.go` (разбор `STRING`)
- **Тест-якорь:** создать `TestLexTripleQuoteNotImplemented` в `internal/lexer/lexer_test.go`
- **DoD:**
  - `"""` в любой позиции, где начинается строка, → ошибка лексера с текстом `multiline strings are not implemented` и `line:col` открывающей кавычки;
  - `""` (пустая строка) и `"\""` лексятся как раньше;
  - `make test-lexer` → 0; `go test ./internal/lexer -run '^$' -fuzz FuzzLex -fuzztime 10s` → PASS; `make all` → 0.
- **НЕ делать:** реализовывать многострочные строки (Should, T-121); менять escape-последовательности; трогать `b"..."`/`rx"..."`.

### T-116 · Docs: тест-фреймворк в спеке (по решению T-109)
<!-- meta
priority: P2
type: docs
effort: S
model: sonnet
wave: 8-hardening
depends_on: T-109
findings: —
-->
- **Файлы:** `docs/01-language-design.md` (§11.5 или новый подраздел — по решению T-109), Part III changelog; `internal/vm/prelude_test_fw.go` (только чтение)
- **Тест-якорь:** `make check-examples`
- **DoD:**
  - в спеке описан API тест-фреймворка ровно в объёме решения T-109: функции, арность, что возвращает `Test.run()`, связь с exit-кодом CLI;
  - примеры в новом разделе — блоки `brig`, их гоняет `check-examples`: `make check-examples` → `failed 0`;
  - запись в changelog Part III.
- **НЕ делать:** менять код `Test.*` (если решение требует — отдельная задача); описывать горячую перезагрузку (вторая часть T-109 — отдельный issue, если решение B); менять §16.

### T-117 · Docs: пример §13.2 — снять TODO и пометить как `brig`
<!-- meta
priority: P3
type: docs
effort: S
model: sonnet
wave: 8-hardening
depends_on: T-108
findings: — (`docs/01-language-design.md:1176`: `TODO(issue #2)`, пример Behavior помечен `text`, потому что лексер не умел мини-блоки)
-->
- **Файлы:** `docs/01-language-design.md:1176-1196` (§13.2); тексты §2.4/§D.6 — только если решение T-107 их меняет и отдельной docs-задачи под это нет
- **Тест-якорь:** `make check-examples`
- **DoD:**
  - `TODO(issue #2)` удалён; пример помечен `brig` (или `brig module`) и отформатирован по правилу T-107;
  - `make check-examples` → `failed 0`, число проверенных блоков выросло на 1 (было/стало — в body PR).
- **НЕ делать:** реализовывать `Behavior`/`spawn_behavior` (T-126); менять смысл примера; трогать остальные разделы §13.
