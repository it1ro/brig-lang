---
name: brig-vm
description: >
  Правила для internal/vm/* — регистровая машина, планировщик акторов
  (scheduler.go), опкоды, арифметика (vm.go), vm.Verify. Использовать при
  добавлении опкодов, правке акторной модели (spawn/send/recv/watch),
  отладке зависаний scheduler'а или падений vm.Verify.
---

# Brig — виртуальная машина (`internal/vm`)

Нормативный источник: `docs/02-register-based-virtual-machine.md` §2,
§6, §10 (соответствие старым опкодам), §12 (риски/verify).

## Кадр (`Frame`, §2)

`regs []runtime.Value` — единственное хранилище значений, стека операндов
нет. Размер фиксирован на функцию (`Chunk.NumRegs`), динамический между
функциями. `callDst` — регистр **в кадре вызывающего**, куда положить
результат вызванного кадра (пишет `CALL`, читает `runSlice`/`stepDone`).

`trapHandler.stackLen` **не существует** в этой реализации — вместо него
`errReg`: промежуточные значения выше живых регистров компилятор просто
не читает после обработчика, восстанавливать их не нужно.

## Соглашение K-1..K-8 (наблюдаемая семантика, не трогать без веской причины)

- **K-1.** Порядок вычисления: callee раньше аргументов; операнды/аргументы
  слева направо; в `%{}` — сначала ключ, потом значение; `and`/`or` —
  short-circuit.
- **K-2.** `JMPIFNOT`/`JMPIF` прыгают **только** на строгий `Bool(false)`/
  `Bool(true)`. Не-`Bool` не прыгает (падает вниз) — поэтому `and`/`or`
  возвращают значение операнда, а не обязательно `Bool`. Это противоречит
  спеке (§7.2, §8.1, §16 «строгий Bool», «Не надо: truthiness»):
  `if 5 then` идёт в then, `1 or 2` → 2. **Открытый design decision #41
  (A-F3)** — поведение не менять до решения.
- **K-3.** `trap` ловит только `*ErrRaise` (см. `Frame.catch`). Ошибки
  арности, `arithErr`, `runtime.Compare`, `not` не-Bool — обычный `error`,
  фатальны для актора, `trap` их не видит. Но `decArithErr` возвращает
  ловимый `ErrRaise`, а спека §10.4 числит `:type_error` среди авто-raise:
  `trap(dec"1"+"a")` → `Error(...)`, а `trap(1+"a")` убивает актор.
  **Открытый design decision #42 (A-F4)** — не унифицировать до решения.
- **K-4.** Редукция — это `CALL`/`TAILCALL` в байткод-функцию, `RETURN`,
  шаг unwind. Вызов native не тратит редукцию; колбэк возобновляемого
  натива (`map`/`filter`/…, см. ниже) — обычный `CALL` в байткод, тратит.
- **K-5.** Native получает свежий слайс аргументов, не окно регистров
  (см. `brig-compiler`, но проверка дублируется и здесь на уровне
  `CALL`/`TAILCALL` в `scheduler.go`).
- **K-6.** `recv`: сначала `downMsgs`, потом `mailbox`. Дедлайн (`after`)
  живёт в `Actor.recvDeadline`, сбрасывается при взятии любого сообщения.
- **K-7.** Замыкание всегда `KindClosure` даже с 0 захватов; захваты —
  снимок по значению.
- **K-8.** См. `brig-compiler` — то, что вне рамок текущей реализации.

## Акторная модель (`scheduler.go`, §6)

- **1 актор = 1 запись `*Actor` + список `frames`.** Планировщик — явный
  run-loop (`runSlice`, `scheduler.go:313-428`), не горутины: реализация
  однопоточная, кооперативная, через `reds`-счётчик редукций. Решено: C
  (#40) — спека §15.2 фиксирует только гарантии G1–G4 (FIFO в паре,
  `:down` впереди и вне HWM, fairness, порядок таймеров), модель потоков —
  деталь реализации; эталон — этот run-loop, эволюция — N:M, не
  goroutine-per-actor. `TestVerifyAF1SingleGoroutineScheduler` —
  spawn 100 акторов даёт рост `NumGoroutine` ≪ 100. Не переписывать
  scheduler на goroutine-per-actor.
- **Мёртвые акторы удаляются из `s.actors`** при `actorDone`/`actorFailed`
  через `reapActor` (кроме `mainPid`; I-F9, T-40 #29). Поэтому `watch` на
  завершившийся pid даёт немедленный `:down` с `:noproc`, `send` →
  `Ok(())`, `mailbox_size` = 0 — так же, как для pid, который никогда не
  существовал.
- **Причина `:down` несёт значение raise** (I-F10, T-40 #29):
  `downRaiseReason` берёт `val` из `errors.As(a.err, &rerr)` — `fail()`
  обнуляет `a.result = Unit`, поэтому `a.result` в reason использовать
  нельзя. Наблюдатель получает `(:down, ref, (:raise, val))`.
- `Send` возвращает `Result<(), Atom>`: `Error(:busy)` при переполнении
  HWM (`defaultHWM = 64`). `:down`-сообщения (`sendDown`) идут в отдельную
  очередь `downMsgs` и **не подчиняются HWM** — приоритет над обычными.
- `Watch`/`Unwatch`/`notifyWatchers` — карты `watchers`/`watching` по
  `ref`; при добавлении новой акторной операции сверяться с уже
  существующей символикой `pid`/`ref` (простые `int`, не UUID).
- `tryUnwindRaise` — всплытие `*ErrRaise` вверх по кадрам актора при
  отсутствии активного handler'а в текущем кадре; если ни один родительский
  кадр не поймал — актор падает (`actorFailed`), наблюдатели получают
  `(:down, ref, (:raise, val))`.
- **Возобновляемые нативы** (G3 fairness, T-58 #144): `map`, `filter`,
  `find`, `all`, `any`, `fold` зарегистрированы `defResumable` в
  `VM.resumable`. `CALL` из байткода (`enterCall`) кладёт на стек актора
  кадр натива (`Frame.cont != nil`, `chunk == nil`, результат колбэка — в
  `regs[0]`), `TAILCALL` превращает текущий кадр в такой. `stepNative`
  пушит байткод-колбэк обычным кадром — редукции тратятся, актор
  вытесняется, `recv` в колбэке блокирует актор как в обычной функции.
  Raise колбэка всплывает сквозь кадр натива (handlers у него нет);
  `attachTrace` кадры натива пропускает. Новый HOF прелюдии с колбэком —
  тоже через `defResumable`, не через `c.Call`, иначе он снова держит
  run-loop. Якорь `TestFairnessPreludeCallback`.
- `callSync` — синхронный вызов вне scheduler-цикла через `runtime.Caller`
  (`vm.Call`: тестовый фреймворк `Test.*`, HOF, вызванный из другого
  натива, — через `runSync`); fairness там нет. Он **не может** заходить в
  `recv` на пустом ящике — это должно фейлиться явной ошибкой
  (`"recv in synchronous call context"`), не зависать. При `stepFailed`
  вызывает `tryUnwindRaise` так же, как `runSlice`.
- Таймеры (I-F14, T-15 #13): переполнение `ms`→`Duration` в `RECVTIMER`
  закрыто T-47 (#60) — `recvTimerDuration` отвергает ms вне
  `[MinInt64/1e6, MaxInt64/1e6]` и big.Int вне int64 как
  `(:type_error, (:after, ...))`; якорь `TestVerifyIF14HugeTimerMs`.
  `wakeExpired` будит истёкших в порядке `(recvDeadline, timerSeq)`
  (T-48 #61; `timerSeq` взводится в `RECVTIMER` из `s.nextSeq`) — не
  возвращать обход `map` напрямую в `ready` (§15.4); якорь
  `TestWakeExpiredDeterministicOrder`. Модель scheduler: решено C (#40).
- Равенство: `PatLiteral` использует `runtime.Equal` (паттерн `1` матчит
  `1.0`), Int×Float сравниваются через float64, Decimal×Float по-разному в
  `==` и в `INDEX`/`Map`/паттернах. Открытый design decision #43 (I-F8) —
  не трогать до решения.

## Опкоды (`opcodes.go`, `chunk.go`)

Инструкция — `uint32`, форматы ABC/ABx/AsBx (`Instr`). При добавлении
нового опкода:

1. Добавить константу в `opcodes.go` + запись в `opNames`.
2. Реализовать case в `stepFrame` (`scheduler.go`).
3. Добавить дизассемблирование в `Chunk.disInstr` (`chunk.go`) — иначе
   `--dump-bytecode` и bytecode-goldens сломаются молча (напечатают `?%d`).
4. Добавить запись в `vm.RegUse` (`verify.go`) — reads/writes регистров.
   Без этого `compiler.emit`'s инвариант I-4 и `vm.Verify` не смогут
   проверить новую инструкцию и либо упадут с «unknown opcode», либо
   тихо пропустят проверку.
5. Обновить таблицу соответствия §10 в дизайн-документе, если опкод
   заменяет/дополняет исторический.

## `vm.Verify` (`verify.go`)

Линейный верификатор: регистры/окна `< NumRegs`, цели переходов внутри
кода, за `MATCHLOCAL` обязана идти `JMP`, `TAILCALL` вне активных
`TRAPBEGIN..TRAPEND` регионов (счётчик глубины), нет падения с конца кода
(`RETURN`/`JMP`/`TAILCALL`/`RAISE` — единственные легальные терминаторы),
definite assignment (dataflow: регистр определён на всех путях выполнения
к точке чтения, включая ветку `TRAPBEGIN`-обработчика). Включается
`compiler.Verify = true` (в тестах — всегда) или `BRIG_VERIFY=1` в CLI.

**CFG MATCHLOCAL/RECVTAKE (I-F1, T-36 #25, T-55 #92):** все рёбра
с состоянием definite assignment строит одна функция `edges`
(`verify.go`): `MATCHLOCAL → {ip+1, ip+2}`, слоты
`Patterns[Bx].Slots()` определены только на success-ребре ip+2;
`RECVTAKE` с `sBx≠0 → {ip+1, ip+1+sBx}`, `R[A]` определён только на
ребре сообщения ip+1 (timeout-ребро VM не пишет); `TRAPBEGIN` —
обработчик с `errReg`. Новый опкод с несколькими исходами — добавлять
туда же. `verifyMatchLocal` структурно (и в недостижимом коде)
проверяет: JMP на ip+1, ip+2 внутри кода, `Bx` внутри `Patterns`, слоты
в `[0, NumRegs)`. `Slots()` обязан совпадать с записями
`MatchPattern` — новый `PatternKind` добавлять в оба и в
`TestCompiledPatternSlotsMatchesMatchPattern`.

**Не отключать `Verify` в тестах компилятора/VM** — это единственная
защита, ловящая рассинхрон между `emit`, `RegUse` и реальной семантикой
опкода до рантайма.

## Чек-лист перед коммитом правки VM

1. Прогнать `make test-vm` (узкий), затем полный `go test ./internal/vm/...`.
2. Для новых акторных примитивов — тест на нормальный путь + тест на
  поведение при отсутствии адресата (`send` к мёртвому pid → `Ok(())`,
  `watch` мёртвого → немедленный `:down` с `:noproc`) — и для pid,
  который не существовал, и для завершившегося актора (`TestWatchDeadActorGetsDown`,
  `TestSendToDeadActor`).
3. Для новых опкодов — обязательно прогнать
   `go test ./internal/compiler/... -run TestBytecodeGolden`; если формат
   дизассемблера/байткод изменился намеренно — `make update-bytecode` и
   явно указать в коммите, что изменилось и почему.
4. Проверить `make test-race` — акторы и замыкания особенно чувствительны
   к гонкам при малейших изменениях в разделяемом состоянии `Scheduler`.
5. Если правка касается редукций/дедлоков — прогнать
   `TestSchedulerDeadlock` и вручную прикинуть, не ввели ли вы новый
   способ бесконечно ждать (`nextDeadline`/`wakeExpired` логика таймеров).
