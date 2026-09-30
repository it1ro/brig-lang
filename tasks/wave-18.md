# Wave 18 — рантайм: коллекции, ящик, связи (третий аудит)

[← карта плана](README.md)

**Откуда:** третий аудит ([AUDIT_REPORT-3.md](../AUDIT_REPORT-3.md)),
слой X; DD T-250…T-252 ([decisions.md](decisions.md)) и T-104
([#173](https://github.com/it1ro/brig-lang/issues/173)).

**Вход:**
- Wave 16 закрыта: `make bench-scaling` (T-247) меряет асимптотику, корпус
  с приложениями аудита (T-249) проверяет, что API не сломан;
- DD T-250 (вместе с T-104), T-251, T-252 решены;
- волна 17 не обязательна: задачи 18 от неё не зависят, кроме общего
  правила «по одной» для `scheduler.go` и `runtime/value.go`.

**Зачем:** главный blocker аудита — квадратичные коллекции (X-1): на
них не пишутся ни аккумуляторы, ни стейт акторов. Второе — надёжность
акторов: тихие потери сверх HWM 64 (X-2) и сироты супервизора (X-3).
Порядок волн («язык, затем рантайм») сохранён, но X-1 — blocker: если DD
T-250 решён раньше, T-270 и T-271 можно брать до конца Wave 17 (правят
`runtime`, а не `compiler.go`).

**Выход:**
- `make bench-scaling` зелёный, job в CI без `continue-on-error`;
- fan-in 1 000 воркеров доставляет все ответы или отправитель видит
  потерю (по T-251);
- после `exit(sup, :kill)` детей супервизора нет (по T-252);
- `runtime.Serialize`/`MFA` удалены.

## Порядок и параллельность

`internal/runtime/value.go` правят T-271, T-272, T-273, T-277 — по
одной, в порядке таблицы. `internal/vm/scheduler.go` правят T-274 и
T-275 — по одной. `internal/compiler/compiler.go` — только T-271 (если
вариант DD меняет опкоды `LIST`/спреда).

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-270 | Docs: сложность операций коллекций — по T-250 | T-250 | sonnet | low | T-274, T-275 |
| 2 | T-271 | `List`: O(1) `[x, ..xs]` и разбор `[h, ..t]` | T-270, T-247 | opus | large | T-274, T-275 |
| 3 | T-272 | `Map` и `Set`: HAMT | T-271 | opus | large | T-274, T-275 |
| 4 | T-273 | `Vector`: 32-арный trie | T-272 | opus | large | T-274, T-275 |
| 5 | T-274 | Ящик: HWM и видимость потерь — по T-251 | T-251 | opus | medium | T-270…T-273 |
| 6 | T-275 | `link`/`spawn_linked` — по T-252 | T-252, T-274 | opus | medium | T-270…T-273 |
| 7 | T-276 | Вызов функции: профиль и две горячие точки | T-247 | opus | medium | T-274, T-275 |
| 8 | T-277 | Удалить `runtime.Serialize` и `runtime.MFA` | — | sonnet | low | всё, кроме T-271…T-273 |

## Задачи

### T-270 · Docs: сложность операций коллекций — по решению T-250
<!-- meta
priority: P1
type: docs
effort: low
model: sonnet
wave: 18
depends_on: T-250
findings: X-1 (AUDIT_REPORT-3)
-->
- **Файлы:** `docs/01-language-design.md` §4.2–§4.6, §15.1
- **Тест-якорь:** — ; проверка — `make check-examples`
- **DoD:**
  - §4.2–§4.6: таблица сложности `len`, индекс, `[x, ..xs]`, разбор `[h, ..t]`, `Map.put/get/remove`, `Vec.push/set/get`, `set`, спред — по варианту T-250;
  - §4.4: обещание trie совпадает с решением T-250 (или снято при варианте B/C);
  - `make check-examples` → `failed 0`; `make all` → 0.
- **НЕ делать:** писать код; обещать константы (только O-оценки); менять API коллекций.

### T-271 · `List`: O(1) `[x, ..xs]` и разбор `[h, ..t]`
<!-- meta
priority: P0
type: full-fix
effort: large
model: opus
wave: 18
depends_on: T-270, T-247
findings: X-1 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/runtime/value.go` (представление `List`, конструкторы, `Inspect`, равенство, `Compare`), `internal/vm` (опкоды списков, спред, `LISTSPREAD`, матчинг `[h, ..t]`), `internal/vm/prelude.go` (`map`, `filter`, `fold`, `list`, `len`), `internal/runtime/json.go`, `internal/compiler/compiler.go` (только если меняются опкоды)
- **Тест-якорь:** `BenchmarkScaling/list_prepend` (T-247): t(8k)/t(1k) ≤ 10; существующие тесты `runtime`, `vm`, `compiler`.
- **DoD:**
  - `make bench-scaling` — `list_prepend` зелёный; проба `perf1.brig` (приложение B отчёта) — 16 000 элементов < 100 мс;
  - `len` — O(1) или задокументировано в T-270;
  - `make fuzz` → 0; `make test-race` → 0; `BRIG_VERIFY=1 go test ./...` → 0; `make corpus` → ok; `make all` → 0;
  - `make bench` (T-152): регрессия > 5 % только в бенчмарках, где T-270 записал смену сложности, и она объяснена в PR.
- **НЕ делать:** `Map`, `Set`, `Vector` (T-272, T-273); менять представление `Value` сверх `List`, если DD T-104 не решён; менять API и семантику.

### T-272 · `Map` и `Set`: HAMT
<!-- meta
priority: P1
type: full-fix
effort: large
model: opus
wave: 18
depends_on: T-271
findings: X-1 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/runtime/value.go` (`Map`, `Set`, `KeyEqual`, хеш по правилам ключей §4.8), `internal/vm` (опкоды `MAP`, `MAPSPREAD`, паттерны `Map`), `internal/vm/prelude.go` (`Map.*`, `set`), `internal/runtime/json.go`, `internal/repl` (печать)
- **Тест-якорь:** `BenchmarkScaling/map_put` (T-247); `TestMapLiteralDuplicateKeys` (T-241); тесты `KeyEqual` (`1` и `1.0` — один ключ, `Decimal` и `Float` — разные).
- **DoD:**
  - `make bench-scaling` — `map_put` зелёный;
  - порядок печати `Map` детерминирован (T-212) и описан в T-270;
  - равенство и term order `Map`/`Set` (§7.4 п.10–11) — тесты зелёные;
  - `make fuzz`, `make test-race`, `BRIG_VERIFY=1 go test ./...`, `make corpus`, `make all` → 0.
- **НЕ делать:** `Vector` (T-273); изменяемую VM-таблицу (research B3); менять правила равенства ключей.

### T-273 · `Vector`: 32-арный trie
<!-- meta
priority: P2
type: full-fix
effort: large
model: opus
wave: 18
depends_on: T-272
findings: X-1 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/runtime/value.go` (`Vector`), `internal/vm` (опкоды `VECTOR`, `VECSPREAD`, индекс), `internal/vm/prelude.go` (`Vec.*`)
- **Тест-якорь:** `BenchmarkScaling/vec_push` (T-247).
- **DoD:**
  - `make bench-scaling` — все три операции зелёные; job в `bench.yml` — без `continue-on-error`;
  - §4.4 (O(1) amortized append, O(log₃₂ n) чтение) выполняется;
  - `make fuzz`, `make test-race`, `BRIG_VERIFY=1 go test ./...`, `make all` → 0.
- **НЕ делать:** паттерны `Vector` с `..` (§4.4); новые функции `Vec.*`; менять `List`.

### T-274 · Ящик: HWM и видимость потерь — по решению T-251
<!-- meta
priority: P1
type: full-fix
effort: medium
model: opus
wave: 18
depends_on: T-251
findings: X-2, S-20 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/vm/scheduler.go` (`defaultHWM`, лимиты `spawn`), `internal/sema` (info на отброшенный `send`), `docs/01-language-design.md` §0.8, §12.2, §12.3, §12.10, `stdlib/*.brig` (отброшенные `send`, если info их отметит)
- **Тест-якорь:** создать `TestFanInDeliversAll` (`internal/vm`): 1 000 воркеров отвечают родителю — все ответы доставлены при варианте A T-251; `TestSpawnMailboxHwmLimit`; `TestDiscardedSendInfo` (`internal/sema`).
- **DoD:**
  - порог по умолчанию, поле лимитов `spawn` и текст §0.8 — по T-251;
  - `send(pid, msg)` стейтментом → info `<file>:<line>:<col>: result of send is discarded; Error(:busy) will be lost`; `_ = send(…)` — без info;
  - проба `a2.brig` (приложение B отчёта) с N = 1 000 → `sum 1000`;
  - `make all` → 0; `make test-race` → 0.
- **НЕ делать:** менять G2 (`:down` вне HWM); менять порог портов; вводить блокирующий `send`.

### T-275 · `link`/`spawn_linked` — по решению T-252
<!-- meta
priority: P1
type: full-fix
effort: medium
model: opus
wave: 18
depends_on: T-252, T-274
findings: X-3, G-26 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/vm/scheduler.go` (`link`, `spawn_linked`, смерть актора), `internal/vm/*_test.go`, `stdlib/supervisor.brig` и `stdlib/supervisor_test.brig`, `docs/01-language-design.md` §12.2, §12.7, §11.5 (строка `link`)
- **Тест-якорь:** создать `TestSupervisorKillLeavesNoOrphans` (`stdlib/supervisor_test.brig` или `internal/vm`): проба X-3 → после `exit(sup, :kill)` `Actor.list()` — только вызывающий.
- **DoD:**
  - поведение `link`/`spawn_linked` — по T-252; при варианте A — ребёнок получает `exit` с причиной из T-252, его `ensure` выполняются, наблюдатели ребёнка получают `:down`;
  - тег ошибки `link(42)` — `(:type_error, (:link, 42))` (G-26);
  - `brig test stdlib` → 0 failed; `make all` → 0; `make test-race` → 0.
- **НЕ делать:** `trap_exit`; двунаправленную эскалацию ошибок; менять `watch` и `spawn_watched`.

### T-276 · Вызов функции: профиль и две горячие точки
<!-- meta
priority: P2
type: full-fix
effort: medium
model: opus
wave: 18
depends_on: T-247
findings: X-4 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/vm` (цикл интерпретатора, `CALL`/`TAILCALL`, кадры), `internal/vm/bench_test.go`
- **Тест-якорь:** `BenchmarkCall`, `BenchmarkTailCall` (T-152).
- **DoD:**
  - в PR — `go test -cpuprofile` на `BenchmarkCall` и `perf2.brig` (приложение B), две самые дорогие точки названы и исправлены;
  - `benchstat`: `BenchmarkCall` и `BenchmarkTailCall` — −25 % времени или лучше, allocs/op не растут;
  - `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** менять представление `Value` (T-104); JIT и новые опкоды; менять семантику редукций (K-4).

### T-277 · Удалить `runtime.Serialize` и `runtime.MFA`
<!-- meta
priority: P3
type: full-fix
effort: low
model: sonnet
wave: 18
depends_on: —
findings: G-25 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/runtime/value.go` (`Serialize`, `serializeValue`, `MFA`), `internal/runtime/serialize_test.go`
- **Тест-якорь:** `go build ./...` и `go test ./internal/runtime`.
- **DoD:**
  - функции и их тест удалены; `rg -n 'Serialize\(|MFA\(' --glob '*.go'` пуст;
  - `make all` → 0.
- **НЕ делать:** вводить `Term` (research Q-term, горизонт); менять §14.8; править `architecture.md` (T-240).
