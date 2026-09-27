# Wave 11 — укрепление и производительность

[← карта плана](README.md)

**Откуда:**
- замеры heap (PR #174, `web-mvp-research/23-heap-measurements.md`) и
  issues, найденные ими, — #171, #172, #173;
- research 19 (бенчмарки как метрика);
- задачи «укрепления» из PR #167, там это была Wave 8.

**Вход:**
- T-102 и T-103 от других задач не зависят. Их можно брать сразу после
  Wave 7, параллельно волнам 8–10: они правят `scheduler.go`, а не
  компилятор;
- T-150 и T-154 ждут Wave 9 и 10;
- T-153 ждёт merge PR #174 (стенд `bench-heap`).

**Зачем:** research показал, что производительность и память упираются в
три вещи:
- представление `Value` (296 Б);
- аллокацию регистров;
- таймеры.

Это нужно решить до рантайм-механизмов Wave 12: `await`, `Timer`,
100k акторов-соединений. Заодно контур «производительность → решения»
становится постоянным: бенчмарки в каждом PR.

**Выход:**
- `make bench` в CI с порогом регрессии;
- #171 и #172 закрыты, решение по #173 принято;
- Z2–Z4 повторены после них;
- третий аудит проведён на свежем `main`.

## Порядок и параллельность

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-152 | `make bench` и порог регрессий в CI | — | sonnet | medium | всё |
| 2 | T-102 | [#171](https://github.com/it1ro/brig-lang/issues/171) VM: куча таймеров | — | opus | medium | T-104, T-152 |
| 3 | T-103 | [#172](https://github.com/it1ro/brig-lang/issues/172) VM: регистры кадра без аллокации на вызов | — | opus | medium | T-104, T-152 |
| 4 | T-104 | [#173](https://github.com/it1ro/brig-lang/issues/173) DD: компактное представление `runtime.Value` | — | human | large | всё |
| 5 | T-153 | Повтор замеров Z2–Z4 после #172/#173 | T-103, T-104, T-152 | opus | medium | — |
| 6 | T-150 | Ревизия оставшихся «срез:» | T-139, T-144 | sonnet | low | T-151 |
| 7 | T-151 | Docs: пример §13.2 — снять `text`, пометить `brig` | T-138 | sonnet | low | T-150 |
| 8 | T-154 | Третий аудит на свежем `main` | T-139, T-146, T-147 | opus | large | — |
| 9 | T-155 [#209](https://github.com/it1ro/brig-lang/issues/209) | Спред `..` в List/Vector/Map и в любой позиции вызова (§5.2) | — | opus | medium | T-150 |

T-102 и T-103 обе правят `internal/vm/scheduler.go` — по одной.

## Задачи

T-102, T-103, T-104 заведены на доске: DoD — в их issues. Здесь только
место в плане. У #171–#173 стоит label `wave-7` — мейнтейнер меняет его на
`wave-11` (`README.md`, «Перед Wave 7»).

### T-150 · Ревизия оставшихся «срез:» в компиляторе
<!-- meta
priority: P2
type: test-infra
effort: low
model: sonnet
wave: 11
depends_on: T-139, T-144
findings: — (было T-114 в PR #167)
extra_labels: verification
-->
- **Файлы:** `internal/compiler/compiler.go` — все `fmt.Errorf("срез:`; `tasks/` — новые блоки по итогам
- **Тест-якорь:** — ; для каждого сайта — проба и её вывод
- **DoD:**
  - в issue — таблица по каждому сайту: достижим ли он из программы, которую пропускают парсер и sema; к чему относится (Must-баг / Should с номером задачи / честное ограничение MVP / недостижимый код);
  - для каждого Must-бага и недостижимого сайта — issue или черновой блок в следующей волне;
  - код не меняется.
- **НЕ делать:** чинить сайты; менять текст ошибок; трогать regex (T-194).

### T-151 · Docs: пример §13.2 — снять `text`, пометить `brig`
<!-- meta
priority: P3
type: docs
effort: low
model: sonnet
wave: 11
depends_on: T-138
findings: P-13 (было T-117 в PR #167)
-->
- **Файлы:** `docs/01-language-design.md` §13.2 (строки около 1176–1196 на `fefb355`)
- **Тест-якорь:** `make check-examples`
- **DoD:**
  - пример `Behavior` помечен `brig` (или `brig pending(T-171)`, пока нет `spawn_behavior` — компиляция из T-116 это покажет) и отформатирован по правилу T-124; TODO удалён;
  - `make check-examples` → `failed 0`, число проверенных блоков выросло на 1 (было/стало — в body PR).
- **НЕ делать:** реализовывать `Behavior` (T-171); менять смысл примера.

### T-152 · `make bench` и порог регрессий в CI
<!-- meta
priority: P2
type: test-infra
effort: medium
model: sonnet
wave: 11
depends_on: —
findings: F-6, R-7 (research 19)
-->
- **Файлы:** `internal/vm/*_bench_test.go` (создать): вызов, `TAILCALL`, `MATCHLOCAL`, `send`/`recv`, native `map` по 1000 элементам, аллокации на вызов; `Makefile` (`bench`); `.github/workflows/bench.yml` (создать): прогон на PR против `main` через `benchstat`
- **Тест-якорь:** `make bench`
- **DoD:**
  - `make bench` гоняет микро-бенчмарки с `-benchmem`, 10 повторов;
  - CI на PR сравнивает с базой `main` через `benchstat`; значимая регрессия > 5 % по времени или аллокациям — красный статус (порог в одном месте, в workflow);
  - бенчмарк из DoD #172 (аллокации на вызов лямбды) входит в набор;
  - `make all` не меняется (бенчмарки не в `all`).
- **НЕ делать:** макро-бенчмарки и сравнение с Rails/Phoenix (горизонт, нужен раннер); оптимизировать VM; переносить стенд `bench-heap`.

### T-153 · Повтор замеров Z2–Z4 после #172 и #173
<!-- meta
priority: P2
type: docs
effort: medium
model: opus
wave: 11
depends_on: T-103, T-104, T-152
findings: R-6 (рекомендация `23-heap-measurements.md`: «#172 → #173 → повтор Z2–Z4»)
-->
- **Файлы:** `web-mvp-research/bench-heap/` (из PR #174); `web-mvp-research/23-heap-measurements.md` (новый раздел «Повтор после #172/#173»); `web-mvp-research/11-memory.md` (ссылка)
- **Тест-якорь:** `go test -tags heapbench ./web-mvp-research/bench-heap/` → `ok`
- **DoD:**
  - таблицы Z2, Z3, Z4 с числами «до / после» на том же железе (или новое железо и повтор «до»);
  - вердикты по четырём триггерам 11 пересмотрены;
  - если T-104 = C (оставить `Value`), замер показывает только эффект #172, и это сказано явно;
  - `make all` → 0.
- **НЕ делать:** принимать решение по модели heap; менять VM.

### T-154 · Третий аудит на свежем `main`
<!-- meta
priority: P1
type: test-infra
effort: large
model: opus
wave: 11
depends_on: T-139, T-146, T-147
findings: — (было T-110 в PR #167; второй аудит сделан раньше срока — это `AUDIT_REPORT-2.md`)
-->
- **Файлы:** `AUDIT_PROMPT-3.md`, `AUDIT_REPORT-3.md` (создать по образцу второго аудита); `tasks/wave-N.md` для findings
- **Тест-якорь:** — ; пробы — тесты в задачах-фиксах
- **DoD:**
  - шапка называет коммит `main`; покрыто то, чего не покрыл второй аудит (§7 `AUDIT_REPORT-2.md`), и код волн 9–10;
  - контуры F-1…F-8 проверены на деле: какие сигналы они дали за волны 9–10 (изменения манифеста корпуса, `pending`, allowlist T-118, регрессии бенчмарков);
  - у каждого finding — ID, уровень, файл:строка, `[confirmed]`/`[inferred]`, направление решения; у каждого major+ — блок задачи;
  - `make plan-check` → 0.
- **НЕ делать:** чинить findings; править `AUDIT_REPORT.md` и `AUDIT_REPORT-2.md`; заводить issues до ревью отчёта человеком.
