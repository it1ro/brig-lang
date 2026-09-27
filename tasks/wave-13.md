# Wave 13 — Should §16 и политика на Brig

[← карта плана](README.md)

**Откуда:**
- Should-задачи из PR #167 (там это была Wave 9) — перенумерованы;
- решения research: `Supervisor` и `Server` — на Brig поверх механизмов
  VM (02, «Supervisor — механизм в ядре, политика в библиотеке»); L16,
  L17.

**Вход:**
- для T-170 — механизмы Wave 12 (T-163, T-165) и stdlib на Brig (T-146);
- для T-171 — мини-блоки (T-138) и пример §13.2 (T-151);
- остальные задачи от Wave 12 не зависят.

**Зачем:** первая настоящая библиотечная политика на Brig — `Supervisor`
и `Server.call`. Это самый сильный тест контура «язык ↔ библиотеки»:
если на них неудобно писать, это сразу видно по коду stdlib, а не по
жалобам пользователей.

**Выход:**
- пример §13.1 исполняется;
- `Server.call` синхронен для вызывающего и не трогает его ящик;
- `corpus/lookout/lib/lookout/application.brig` и `monitors/*` доходят до
  уровня `check`; то, что ждёт HTTP и SQL, помечено `needs` горизонта.

## Порядок и параллельность

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-170 | `Supervisor` и `Server` на Brig | T-146, T-163, T-164, T-165 | opus | large | T-172, T-174…T-177 |
| 2 | T-171 | `Behavior` и `spawn_behavior` (§13.2) | T-138, T-151 | opus | medium | T-172, T-174…T-177 |
| 3 | T-172 | Docs: схема TCO сквозь `ensure` в doc 02 | — | opus | medium | всё |
| 4 | T-173 | TCO сквозь `ensure`: реализация | T-172 | opus | large | T-174…T-177 |
| 5 | T-174 | DD: `trace(pid)` и логер (§17 п.9) | — | human | low | всё |
| 6 | T-175 | `Range` в JSON по решению T-121 п.7 | T-121 | sonnet | low | всё |
| 7 | T-176 | `Json.at`, `Map.get_or` (L16) | T-146 | sonnet | low | всё |
| 8 | T-177 | sema: info-диагностика `_ <-` с `Result` (L17) | T-146 | sonnet | low | всё |

## Задачи

### T-170 · `Supervisor` и `Server` на Brig
<!-- meta
priority: P1
type: feature
effort: large
model: opus
wave: 13
depends_on: T-146, T-163, T-164, T-165
findings: — (было T-125 в PR #167; research 02 «ядро vs библиотека», 04 `Supervisor`/`Server`)
-->
- **Файлы:** `stdlib/supervisor.brig`, `stdlib/server.brig` (создать); `docs/01-language-design.md` §13.1 — только если форма child spec расходится с примером (стоп и комментарий в issue, правка — отдельной docs-задачей)
- **Тест-якорь:** доктесты и тесты `brig test stdlib/`: `supervisor_one_for_one_restart`, `supervisor_transient_normal_exit`, `supervisor_max_restarts_escalates`, `supervisor_stop_order`, `server_call_reply`, `server_call_timeout`
- **DoD:**
  - пример §13.1 (`:one_for_one`, `max_restarts`, `within`, `children` с `restart: :permanent`/`:transient`) запускается;
  - `:permanent` перезапускается после любого выхода, `:transient` — после аварийного; больше `max_restarts` за `within` мс — супервизор завершается аварийно; окно — по `Time.monotonic_ms`;
  - `Supervisor.stop(sup, reason, { timeout })` гасит детей в обратном порядке через `exit` (T-163);
  - `Server.call(pid, req, timeout)` шлёт `(:call, from, req)` и ждёт через `await` (T-165); `Server.reply(from, v)`; чужие сообщения вызывающего не трогаются (тест как в T-165);
  - тесты детерминированы; `brig test stdlib/` → 0; `make corpus` → 0 (метки `needs: T-170` сняты); `make all` → 0.
- **НЕ делать:** стратегии кроме `:one_for_one` (issue, если нужно); `start_dynamic` (горизонт вместе с `Registry`); `Behavior` (T-171); Go-реализацию супервизора.

### T-171 · `Behavior` и `spawn_behavior` (§13.2)
<!-- meta
priority: P3
type: feature
effort: medium
model: opus
wave: 13
depends_on: T-138, T-151
findings: — (было T-126 в PR #167)
-->
- **Файлы:** `stdlib/behavior.brig` (тип и цикл диспетча на Brig) или примитив VM — выбор в body PR (research склоняется к Brig); `internal/compiler` — только регистрация, если примитив
- **Тест-якорь:** создать `behavior_dispatch` (пример §13.2), `behavior_unknown_tag`
- **DoD:**
  - пример §13.2 (kv с `:get`/`:put`) исполняется; `pending` в спеке снят;
  - неизвестный тег → поведение, записанное в body PR (если спека молчит — стоп, комментарий в issue);
  - `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** валидировать колбэки (вне MVP, §13.2); добавлять `spawn_behavior` в прелюдию.

### T-172 · Docs: схема TCO сквозь `ensure` (cleanup-регистр) в doc 02
<!-- meta
priority: P3
type: docs
effort: medium
model: opus
wave: 13
depends_on: —
findings: — (было T-123 в PR #167; doc 02:306)
-->
- **Файлы:** `docs/02-register-based-virtual-machine.md` (раздел о trap/ensure и TCO, строка 306; соглашения K-*); `docs/01-language-design.md` §10.3, §15.3 — только ссылка на doc 02
- **Тест-якорь:** — (docs); пример байткода «до/после»
- **DoD:**
  - в doc 02 есть раздел «TCO сквозь `ensure`»: где хранятся отложенные ensure при `TAILCALL`, порядок (LIFO сохраняется), что видит `vm.Verify`, какие опкоды или флаги меняются (явным списком);
  - перечислены инварианты для тест-якорей T-173: рекурсия глубиной 10⁶ внутри `trap` с `ensure` не растит стек; ensure выполняются ровно один раз и LIFO;
  - строка 306 ссылается на новый раздел.
- **НЕ делать:** менять код; менять норматив §10.3/§15.3; TCO для тела `trap` без `ensure`.

### T-173 · TCO сквозь `ensure`: реализация
<!-- meta
priority: P3
type: feature
effort: large
model: opus
wave: 13
depends_on: T-172
findings: — (было T-124 в PR #167)
-->
- **Файлы:** по схеме T-172: `internal/compiler/compiler.go` (`compileTrapWithEnsure`, `compileTrapBodyWithEnsures`, проверка `trapDepth` у `TAILCALL`); `internal/vm/vm.go`, `internal/vm/verify.go`
- **Тест-якорь:** создать `TestTailCallThroughEnsureDeepRecursion` (10⁶), `TestEnsureOrderWithTailCall`; обновить `TestAuditNoTailCallInsideTrap` по схеме
- **DoD:**
  - оба теста зелёные; стек при 10⁶ не растёт (по числу кадров);
  - ensure — ровно один раз, LIFO, в том числе при `raise` из хвостового вызова;
  - `make update-bytecode` — diff просмотрен; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** отступать от схемы T-172 без правки doc 02; TCO для тела `trap` без `ensure`; менять семантику `ensure`.

### T-174 · DD: `trace(pid)` и логер (§17 п.9)
<!-- meta
priority: P3
type: design-decision
effort: low
model: human
wave: 13
depends_on: —
findings: — (было T-122 в PR #167; `call` снят: решён research — `Server.call` в T-170)
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §15.2 (`trace(pid)`), §16, §17 п.9; `internal/vm/prelude.go` (`log`); research 16 (`Telemetry`)
- **Тест-якорь:** — (решение)
- **Вопрос:** как устроены `trace(pid)` и логер, если research решил строить наблюдаемость на шине `Telemetry` (16)?
- **Варианты:** **A:** `trace(pid)` — подписка на события `[:vm, :actor, …]` в `Telemetry`; логер — `Log` на Brig со структурой key-value (research 04), `log` прелюдии становится `Log.info`. **B:** `trace(pid)` печатает сообщения актора в stderr (как было в PR #167); `Telemetry` отдельно. **C:** отложить `trace(pid)` до `brig observe` (горизонт).
- **Рекомендация аудита:** A: один механизм вместо двух (research 16, «второго механизма рядом с `Telemetry` нет»).
- **DoD:** вариант записан; для «делается» — блок задачи в следующей волне. Issue закрыт.
- **НЕ делать:** писать код; менять `log` до решения.

### T-175 · `Range` в JSON по решению T-121 п.7
<!-- meta
priority: P3
type: full-fix
effort: low
model: sonnet
wave: 13
depends_on: T-121
findings: — (Part II N10; остаток T-121 из PR #167)
-->
- **Файлы:** `internal/runtime/json.go`; `docs/01-language-design.md` Part II N10 и §16 — строка «сериализация `Range`» (по решению)
- **Тест-якорь:** создать `TestJsonRangePolicy`
- **DoD:**
  - поведение `Json.encode(1 to 3)` — по T-121 п.7; при варианте «оставить ошибку» задача закрывается как won't-fix правкой N10 и §16 (Should → «не делается», `Term` сериализует `Range`);
  - `make all` → 0.
- **НЕ делать:** `Term` (горизонт); `Json.decode` в `Range`.

### T-176 · `Json.at` и `Map.get_or` (L16)
<!-- meta
priority: P2
type: feature
effort: low
model: sonnet
wave: 13
depends_on: T-146
findings: — (research L16)
-->
- **Файлы:** `stdlib/json.brig` (или Go-native в `prelude_json.go` — выбор в body PR), `stdlib/map.brig` / `prelude.go`; таблица прелюдии §11.5 (см. правило про docs в `CONTRIBUTING.md` после T-110)
- **Тест-якорь:** доктесты: `Json.at(%{"a" => [1, %{"b" => 2}]}, ["a", 1, "b"]) == Some(2)`, `Map.get_or(%{}, :k, 0) == 0`
- **DoD:**
  - `Json.at(v, path) -> Option`: ключи `Str` для мап, `Int` для списков; нет пути → `None`;
  - `Map.get_or(m, k, default)`; тест T-118 знает имена; `make all` → 0.
- **НЕ делать:** оператор `?.` (research: отвергнут); `Json.decode_as` (горизонт).

### T-177 · sema: info-диагностика `_ <-` с `Result` (L17)
<!-- meta
priority: P3
type: feature
effort: low
model: sonnet
wave: 13
depends_on: T-146
findings: — (research L17)
-->
- **Файлы:** `internal/sema/sema.go` (bind `_ <- call` в `with`); таблица функций, возвращающих `Result` (прелюдия, stdlib — по аннотации `-> Result<…>`)
- **Тест-якорь:** создать `TestSemaInfoUnderscoreResultBind`
- **DoD:**
  - `_ <- Repo.exec(...)`, где функция по таблице или аннотации возвращает `Result`, → `info: file:line:col: result of X is ignored; use Ok(_) <-`; exit-код не меняется;
  - `_ <- print(a)` диагностики не даёт;
  - `make all` → 0.
- **НЕ делать:** делать это ошибкой; выводить типы; проверять аннотации как контракты.
