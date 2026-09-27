# Wave 12 — рантайм-механизмы

[← карта плана](README.md)

**Откуда:**
- research, фаза 2 (`web-mvp-research/02-runtime.md` R1, R3–R8, R12, R13;
  `11-memory.md` M1–M2);
- решения Q-await, Q-reg, Q-exit (07).

**Вход:**
- Wave 8 закрыта: T-122 отнёс эти решения к отдельному пакету спеки,
  он здесь (T-160…T-162);
- для T-166 — T-102 (#171, куча таймеров).

**Зачем:** без этих механизмов не пишутся `Supervisor`, `Server.call`,
любой синхронный клиент (главная находка research — R12) и вообще
любой I/O. Всё это — механизмы VM, а политика (супервизоры, сервер)
пишется на Brig в Wave 13. Спека правится до кода: примитивы акторов —
нормативная часть §12 и §15.

**Выход:**
- `exit`, `spawn_watched`, `register`/`whereis`, `await`/`reply`,
  `Timer`, `Time`, `Global` работают и описаны в спеке;
- run-loop ждёт внешние события и не завершается, пока есть живые
  подписки;
- первый порт `Signal` доставляет SIGTERM актору;
- `corpus/lookout/lib/main.brig` и чекер доходят до уровня `check`.

## Порядок и параллельность

Docs-задачи T-160…T-162 правят `docs/01-language-design.md` §12 и §15 —
по одной. Код T-163…T-169 почти весь в `internal/vm/scheduler.go` — по
одной, кроме T-167 (`Global`, отдельный файл) и T-169.

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-160 | Спека: `exit`, `spawn_watched`, реестр, `await`/`reply`, бюджет хода | T-122 | opus | medium | — |
| 2 | T-161 | Спека: `Timer`, `Time`, `Global` | T-122 | sonnet | low | T-163…T-165 |
| 3 | T-162 | Спека: внешние события, G5, условие завершения | T-122 | opus | medium | T-163…T-165 |
| 4 | T-163 | `exit(pid, reason)` и `spawn_watched` | T-160 | opus | medium | T-167 |
| 5 | T-164 | Реестр имён `register`/`whereis` | T-160 | sonnet | medium | T-167 |
| 6 | T-165 | `await(ref, timeout)` и `reply` — ref-слоты | T-102, T-160 | opus | large | T-167 |
| 7 | T-166 | `Timer.send_after`/`cancel`, `Time.monotonic_ms`/`now` | T-102, T-161 | sonnet | medium | T-167 |
| 8 | T-167 | `Global.put`/`get` | T-161 | sonnet | low | всё |
| 9 | T-168 | Внешние события в run-loop и порт `Signal` | T-162, T-163 | opus | large | — |
| 10 | T-169 | Бюджет хода и `Actor.info` (M1, M2) | T-163 | opus | medium | T-167 |

## Задачи

### T-160 · Спека: `exit`, `spawn_watched`, реестр, `await`/`reply`, бюджет хода
<!-- meta
priority: P1
type: docs
effort: medium
model: opus
wave: 12
depends_on: T-122
findings: R-1 (research R3, R4, R5, R8/M1, R12; Q-exit, Q-reg, Q-await)
extra_labels: edit-spec
-->
- **Файлы:** `docs/01-language-design.md` §12.2, §12.6 (таблица примитивов), §10.4 (`:resource_limit`), §16 (строка «`recv!`/selective receive» — пояснить, что `await` по ref не является selective receive, с обоснованием из research R12), §7.5 (запрет в pipe — дополнить новыми примитивами); `docs/02-register-based-virtual-machine.md` (новые опкоды или нативы — только список и контракт)
- **Тест-якорь:** `make check-examples` (примеры — `brig pending(T-16x)`), тест T-118 (новые имена в allowlist с номерами задач)
- **DoD:**
  - `exit(pid, reason)`: асинхронно; жертва завершается в ближайшей точке редукции или ожидания; `ensure` жертвы выполняются, кроме `:kill`; `trap` его не ловит; наблюдатели получают `(:down, ref, reason)`;
  - `spawn_watched(f) -> (pid, ref)` атомарен;
  - `register(name, pid)`: имя — любой терм; повторная регистрация занятого имени → `Error(:already_registered)`; при смерти имя снимается. `whereis(name) -> Option<Pid>`; `send` по имени нет;
  - `await(ref, timeout) -> Result<V, :timeout>` и `reply(pid, ref, v)`: сообщение по ref не попадает в ящик и не учитывается в HWM; поздний ответ отбрасывается;
  - бюджет хода и авто-завершение `(:resource_limit, …)` — формат и где задаётся (child spec — в Wave 13);
  - Part III запись; `make check-examples` → `failed 0`.
- **НЕ делать:** описывать `Supervisor`/`Server` (Wave 13); порты кроме упоминания в R1 (T-162); менять G1–G4 сверх расширения на `await`.

### T-161 · Спека: `Timer`, `Time`, `Global`
<!-- meta
priority: P2
type: docs
effort: low
model: sonnet
wave: 12
depends_on: T-122
findings: R-1 (research R6, R7, R13/B3)
extra_labels: edit-spec
-->
- **Файлы:** `docs/01-language-design.md` §12.4 (`after` — таймаут пустого ящика; для периодики — `Timer`), §15.2 G4 (распространяется на `Timer`), §11.5 или новый подраздел stdlib рантайма (`Timer`, `Time`, `Global`), §15.4 (время — источник недетерминизма)
- **Тест-якорь:** `make check-examples`
- **DoD:**
  - `Timer.send_after(ms, pid, msg) -> Ref`, `Timer.cancel(ref) -> Bool`; порядок срабатывания — G4 (deadline, порядок взвода);
  - `Time.monotonic_ms() -> Int`, `Time.now()` — UTC-инстант (тип — `Int` мс до `Instant` из горизонта, решение записано);
  - `Global.put(name, value)`, `Global.get(name) -> Option`: значение иммутабельно, меняется только привязка;
  - Part III запись; `make check-examples` → `failed 0`.
- **НЕ делать:** `Instant`/`Date`/`Duration` целиком (горизонт); фейковые часы (горизонт, R9).

### T-162 · Спека: внешние события, G5, условие завершения
<!-- meta
priority: P1
type: docs
effort: medium
model: opus
wave: 12
depends_on: T-122
findings: R-1 (research R1, R14)
extra_labels: edit-spec
-->
- **Файлы:** `docs/01-language-design.md` §15.2 (новая гарантия G5), §15.4 (I/O — источник недетерминизма), §12 (понятие порта: владелец, смерть владельца закрывает порт); `docs/02-register-based-virtual-machine.md` (inject-очередь, ожидание `select` вместо выхода, VM без ОС-зависимостей — R14)
- **Тест-якорь:** `make check-examples`
- **DoD:**
  - G5: события порта доставляются владельцу в порядке их возникновения на ресурсе;
  - программа завершается, когда `main` завершился и нет живых портов (или вызван `Sys.halt`); «все заблокированы, портов нет» — прежнее поведение;
  - native-вызовы не блокируют run-loop; всё, что ждёт ОС, — порт и сообщение;
  - порт: opaque-значение, identity-равенство, место в term order после `Ref`, не сериализуется;
  - Part III запись; `make check-examples` → `failed 0`.
- **НЕ делать:** описывать конкретные порты кроме `Signal`; HTTP, TCP, файлы (горизонт); N:M.

### T-163 · `exit(pid, reason)` и `spawn_watched`
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 12
depends_on: T-160
findings: — (research R3, R4)
-->
- **Файлы:** `internal/vm/scheduler.go` (завершение чужого актора, выполнение `ensure` жертвы), `internal/vm/opcodes.go` или прелюдия; `internal/compiler` (регистрация примитивов, запрет в pipe); `internal/sema` (pipe-запрет)
- **Тест-якорь:** создать `TestExitRunsEnsure`, `TestExitKillSkipsEnsure`, `TestExitNotTrappable`, `TestExitDownReason`, `TestSpawnWatchedAtomic`
- **DoD:**
  - всё поведение из T-160 проверено тестами; `exit` себя — как `raise` без возможности `trap`;
  - `exit` мёртвому или несуществующему pid → `Ok(())`, без ошибки;
  - `BRIG_VERIFY=1 go test ./...` → 0; `make test-race` → 0; `make all` → 0.
- **НЕ делать:** `Supervisor` (T-170); связь `link` с автоматическим `exit` (§12.2: `link` — наблюдение).

### T-164 · Реестр имён `register`/`whereis`
<!-- meta
priority: P1
type: feature
effort: medium
model: sonnet
wave: 12
depends_on: T-160
findings: — (research R5, Q-reg)
-->
- **Файлы:** `internal/vm/scheduler.go` (таблица имён, снятие при смерти); прелюдия; `internal/runtime` (терм как ключ — `KeyEqual`)
- **Тест-якорь:** создать `TestRegisterWhereis`, `TestRegisterTermKey`, `TestRegisterAlreadyRegistered`, `TestRegisterRemovedOnDeath`
- **DoD:**
  - поведение из T-160; ключ `(:checker, 42)` работает; после смерти `whereis` → `None` в той же редукции, что и `:down`;
  - `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** `send` по имени; `Registry`/`PubSub` (stdlib, горизонт).

### T-165 · `await(ref, timeout)` и `reply` — ref-слоты
<!-- meta
priority: P0
type: feature
effort: large
model: opus
wave: 12
depends_on: T-102, T-160
findings: — (research R12, Q-await — главная находка research)
-->
- **Файлы:** `internal/vm/scheduler.go` (слоты по ref у актора, пробуждение по ответу или таймауту — через кучу таймеров T-102), `internal/vm/opcodes.go` (если нужен опкод блокирующего ожидания по образцу `RECVTAKE`), `internal/vm/verify.go` (новые рёбра), `internal/compiler`
- **Тест-якорь:** создать `TestAwaitReplyBypassesMailbox`, `TestAwaitTimeout`, `TestAwaitLateReplyDropped`, `TestAwaitDoesNotConsumeOtherMessages`, `TestAwaitHwmUnaffected`
- **DoD:**
  - `ref = make_ref(); send(srv, (:get, self(), ref)); await(ref, 1000)` получает `reply(from, ref, v)`, при этом `:tick`, пришедший до ответа, остаётся в ящике и берётся следующим `recv`;
  - таймаут → `Error(:timeout)`; поздний ответ не попадает ни в слот, ни в ящик;
  - слот не учитывается в HWM и не виден `mailbox_size`;
  - `vm.Verify` знает новые рёбра; `BRIG_VERIFY=1 go test ./...` → 0; `make test-race` → 0; `make all` → 0.
- **НЕ делать:** `Server.call` (T-170); селективный `recv` по паттерну; ожидание нескольких ref сразу (если нужно — DD).

### T-166 · `Timer.send_after`/`cancel`, `Time.monotonic_ms`/`now`
<!-- meta
priority: P1
type: feature
effort: medium
model: sonnet
wave: 12
depends_on: T-102, T-161
findings: — (research R6, R7)
-->
- **Файлы:** `internal/vm/scheduler.go` (таймеры отправки на куче из T-102); прелюдия `Timer`, `Time`
- **Тест-якорь:** создать `TestTimerSendAfterOrder`, `TestTimerCancel`, `TestTimeMonotonic`
- **DoD:**
  - таймеры с одинаковым deadline срабатывают в порядке взвода (G4), общий порядок с `recv … after` — единый;
  - `Timer.cancel` до срабатывания → `true`, после → `false`;
  - 100k взведённых таймеров не замедляют срабатывание одного (тот же критерий, что у #171);
  - `make all` → 0.
- **НЕ делать:** фейковые часы; периодические таймеры (`send_interval` — через повторный `send_after`).

### T-167 · `Global.put`/`get`
<!-- meta
priority: P2
type: feature
effort: low
model: sonnet
wave: 12
depends_on: T-161
findings: — (research R13, B3)
-->
- **Файлы:** `internal/vm` (новый `global.go`; таблица на уровне рантайма, не актора); прелюдия `Global`
- **Тест-якорь:** создать `TestGlobalPutGet`, `TestGlobalVisibleAcrossActors`, `TestGlobalOverwrite`
- **DoD:**
  - `Global.put(:config, v)` в одном акторе, `Global.get(:config)` → `Some(v)` в другом; отсутствующий → `None`;
  - `make test-race` → 0; `make all` → 0.
- **НЕ делать:** изменяемые таблицы (аналог ETS, B3: только через DD); подписку на изменения.

### T-168 · Внешние события в run-loop и порт `Signal`
<!-- meta
priority: P1
type: feature
effort: large
model: opus
wave: 12
depends_on: T-162, T-163
findings: — (research R1, R2 — первый порт с минимальным риском)
-->
- **Файлы:** `internal/vm/scheduler.go` (inject-очередь, потокобезопасная; ожидание `select { таймер | inject }` вместо выхода, когда ready пуст; условие завершения по T-162); новый `internal/vm/port.go` (вид `Port`, владелец); `internal/runtime` (`KindPort`, term order, `Inspect`); `cmd/brig` (подписка на сигналы ОС)
- **Тест-якорь:** создать `TestRunLoopWaitsForInject`, `TestRunLoopExitsWithoutPorts`, `TestSignalDeliveredToOwner`, `TestPortClosedOnOwnerDeath`
- **DoD:**
  - `Signal.subscribe([:sigterm])` → актор получает `(:signal, :sigterm)` при `kill -TERM`; e2e-тест через подпроцесс `brig run`;
  - без подписок программа завершается как раньше (все существующие тесты зелёные); с подпиской — ждёт, пока `main` не завершится или подписка не снята;
  - ядро VM не импортирует `os/signal` (реализация порта — отдельный файл за интерфейсом, R14);
  - `make test-race` → 0; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** `Tcp`, `File`, `Proc`, HTTP (горизонт); N:M; `Sys.halt` сверх T-162.

### T-169 · Бюджет хода и `Actor.info` (M1, M2)
<!-- meta
priority: P2
type: feature
effort: medium
model: opus
wave: 12
depends_on: T-163
findings: — (research 11 M1, M2; Z5 из `23-heap-measurements.md` ждёт этой задачи)
-->
- **Файлы:** `internal/vm/scheduler.go` (счётчики редукций и выделенных байт с начала хода, сброс на `recv`); прелюдия `Actor.info`, опция лимитов при `spawn` (форма — по T-160)
- **Тест-якорь:** создать `TestTurnReductionLimitExits`, `TestTurnBudgetResetsOnRecv`, `TestActorInfo`
- **DoD:**
  - актор с бесконечным циклом и лимитом редукций завершается `(:resource_limit, (:turn_reductions, used, limit))`, наблюдатель получает `:down`;
  - `Actor.info(pid)` → запись `{ reductions, mailbox, … }` по T-160;
  - без лимитов накладные расходы в пределах порога T-152 (вывод `benchstat` — в body PR);
  - `make all` → 0.
- **НЕ делать:** учёт байт точнее Go-аллокатора (оценка допустима, это записано в T-160); `GOMEMLIMIT` и сторож (M3, горизонт).
