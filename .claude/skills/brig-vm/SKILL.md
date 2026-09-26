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
  шаг unwind. Вызов native не тратит редукцию.
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
  однопоточная, кооперативная, через `reds`-счётчик редукций. Спека §15.2
  и `architecture.md:148` говорят «1 актор = 1 goroutine» — расхождение
  не задокументировано. Это открытый design decision #40 (A-F1);
  T-13 (#11) подтвердил: `TestVerifyAF1SingleGoroutineScheduler` —
  spawn 100 акторов даёт рост `NumGoroutine` ≪ 100. Модель
  планировщика не менять.
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
- `callSync` — синхронный вызов вне обычного scheduler-цикла (используется
  прелюдией через `runtime.Caller`); он **не может** заходить в `recv` на
  пустом ящике — это должно фейлиться явной ошибкой
  (`"recv in synchronous call context"`), не зависать. При `stepFailed`
  вызывает `tryUnwindRaise` так же, как `runSlice` — `trap` в колбэке
  прелюдии ловит raise из вложенного кадра (`map(fn (x) -> trap(g(x)), xs)`).
- Таймеры (I-F14, **confirmed** T-15 #13): большой `ms` в `RECVTIMER`
  молча переполняет `time.Duration` (`MaxInt64` → −1ms; якорь
  `TestVerifyIF14HugeTimerMs`); `wakeExpired` обходит `map` — порядок в
  `ready` недетерминирован (§15.4; 20× `uniq -c` даёт >1 строки). Фикс —
  Wave 3 follow-ups из T-15; модель scheduler не менять (A-F1, T-90).
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

**CFG MATCHLOCAL/RECVTAKE (I-F1, T-36 #25):** `successors` моделирует
`MATCHLOCAL → {ip+1, ip+2}` и `RECVTAKE` с `sBx≠0 → {ip+1, ip+1+sBx}`
(`sBx==0` — только `{ip+1}`, block без after). На success-ребре
`MATCHLOCAL` (ip+2) `verifyDefiniteAssignment` помечает слоты
`Patterns[Bx].Slots()` определёнными; fail-ребро (ip+1, JMP) — нет.
Якоря: `TestVerifyMatchLocalBranchUndefinedReg`,
`TestVerifyRecvAfterUndefinedReg`, `TestCompiledPatternSlots`.

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
