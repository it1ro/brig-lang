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

**Окна регистров — со стека актора** (T-103 #172): `f.regs` выдаёт
`Actor.regs` (`regStack`, сегменты не переезжают, cap окна == размер
окна), `popFrame` обнуляет окно и оставляет `*Frame` в `a.frames` за len
для переиспользования. Кадры создавать только через `pushCall`/
`pushNative`, снимать — только `popFrame`; окно не должно утекать за
кадр: захваты, `ErrRaise`, аргументы натива — копии
(`TestFrameWindowReuse`). `nativeStep.args` — буфер cont, его копируют
`enterCall` и `runSync`. Вызов байткод-функции не аллоцирует —
якорь `TestCallFrameAllocs`. Заблокированный актор отдаёт запас (`shed`).

`trapHandler.stackLen` **не существует** в этой реализации — вместо него
`errReg`: промежуточные значения выше живых регистров компилятор просто
не читает после обработчика, восстанавливать их не нужно.

## Соглашение K-1..K-8 (наблюдаемая семантика, не трогать без веской причины)

- **K-1.** Порядок вычисления: callee раньше аргументов; операнды/аргументы
  слева направо; в `%{}` — сначала ключ, потом значение; `and`/`or` —
  short-circuit.
- **K-2.** Строгий Bool (DD #41, вариант A; T-81 #106). `JMPIFNOT`/`JMPIF`
  на не-`Bool` поднимают ловимый `*ErrRaise`
  `(:type_error, (:expected_bool, v))` (`notBoolErr`, форма как у
  `assert`) — для условия `if`, обоих операндов `and`/`or` и guard
  (`fn`/`recv`). `and`/`or` всегда возвращают `Bool`. Правый операнд
  проверяет холостой `JMPIF/JMPIFNOT acc, +0` после его вычисления
  (`compileAndOr`).
- **K-3.** `trap` ловит только `*ErrRaise` (см. `Frame.catch`). DD #42
  (A-F4) решён вариантом A: все `:type_error` ловимы, фатальны для актора
  только внутренние инварианты VM (`internal:`). Форма —
  `(:type_error, (op, val))` через `typeErr` (`vm.go`): арифметика
  (`arithErr`/`decArithErr`, `val = (a, b)`), `neg`, `not`, сравнения
  (ошибка `runtime.Compare` → `(:compare, (a, b))`), вызов не-функции
  (`resolveCallee`/`vm.Call` → `(:call, fn)`; Function/Closure без тела —
  `internal:`), не-Bool в условии (`notBoolErr`) — T-83 #108, якорь
  `internal/compiler/type_error_test.go`. Прелюдия, индексация/range/
  запись/поле, спред, акторные примитивы (`send`/`watch`/`unwatch`/
  `mailbox_size`/`after`) — тоже `typeErr` (T-96 #150, якорь
  `internal/compiler/type_error_prelude_test.go`); арность (байткод и
  native, `Json.encode`) — ловимый `(:function_clause, [args])`
  (`functionClause`, форма §6.1 как у мультиклоза). Фатальны остались
  только `internal:` и `deadlock`. Новый `:type_error` — только через
  `typeErr`, новый `:function_clause` — через `functionClause`; в
  `stepFrame` ошибку сначала отдавать в `f.catch`, затем `fail`.
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
  run-loop (`runSlice`, `scheduler.go`), не горутины на актора: реализация
  кооперативная, через `reds`-счётчик редукций. Решено: C
  (#40) — спека §15.2 фиксирует только гарантии G1–G4 (FIFO в паре,
  `:down` впереди и вне HWM, fairness, порядок таймеров), модель потоков —
  деталь реализации; эталон — этот run-loop, эволюция — N:M, не
  goroutine-per-actor. `TestVerifyAF1SingleGoroutineScheduler` —
  spawn 100 акторов даёт рост `NumGoroutine` ≪ 100. Не переписывать
  scheduler на goroutine-per-actor.
- **Режим сессии REPL (T-205, §11.4).** `StartSession` заводит один
  долгоживущий актор (`sessionPid`, изначально `-1`) и одну горутину
  `sessionLoop`. Вводы — очередь `Submit`: кадр функции ставится на этот
  актор, по `RETURN` или непойманному raise актор не завершается и не
  шлёт `:down` (ящик и pid те же). Между вводами цикл не возвращает
  deadlock: ждёт таймер, сообщение, событие порта или следующий ввод и крутит
  заспавненных акторов. `Interrupt` взводит флаг; цикл смотрит его на
  границе слайса (`defaultReductions`) и при ожидании — ввод снимается
  за ≤ 1 слайс, кадры сбрасываются без `ensure`, это не `*ErrRaise`
  (`ErrInterrupted`). Фоновых акторов прерывание не трогает. `brig <file>`
  для модуля и `brig test` остаются на `runMain` и эту горутину не
  поднимают. Script-файл (§11.3) исполняется актором сессии: инструкции
  по порядку, значения не печатаются.
  Хелперы консоли (`time`, `load` script-файла) исполняют код через
  `CallNested` поверх кадров текущего ввода; `recv` там не ошибка:
  `awaitNested` крутит остальные акторы и таймеры, пока актор сессии не
  разбудят, `Interrupt` снимает ожидание (T-214). Натив на горутине цикла
  не сдаёт работу в `jobs` (deadlock): глобалы меняет `RedefineHere`, а
  `Redefine`/`Sync` — только для вызова извне цикла. Горутину цикла по
  `runtime.Stack` не определять.
  `link` по §12.2 — наблюдение, не эскалация: падение связанного актора
  кладёт `:down` в ящик и не обрывает текущий ввод.
- **`exit(pid, reason)` (§12.7, T-163, `exit.go`).** Опкод `EXIT`: чужому
  pid — `Scheduler.Exit` ставит `Actor.exit` (первая причина выигрывает,
  `:kill` поверх начатого unwind его перезапускает без `ensure`) и через
  `hurry` будит жертву/ставит в начало `ready`; себе — `stepExit`, сразу.
  Сигнал обрабатывается в начале итерации `runSlice` (точка редукции):
  `unwindExit` снимает кадры, пропуская handlers `trap`, до handler'а с
  `ensure` (`TRAPENSURE` — тело trap с `ensure` и защита каждого `ensure`),
  прыгает в него и запоминает (кадр, глубина handlers); `ENSEND` в конце
  ensure-блока в этой точке отдаёт `stepExit` — unwind продолжается.
  Ensure нет — `exitDone`: `:down` с причиной как есть, `a.err = *ErrExit`.
  Непойманная ошибка во время unwind причину не меняет. `callSync` и
  `CallNested` ensure не исполняют (ErrExit сразу). `spawn_watched` —
  `SPAWN` с C=2, `Watch` в том же шаге, результат `(pid, ref)`.
- **Порты и внешние события (§12.12, §15.2, T-168, `port.go`).** Значение
  `runtime.KindPort` с `*runtime.PortHandle` (ID, Owner, Closed): равенство
  и term order по ID, после `Ref`. Таблица открытых — `Scheduler.ports`,
  у актора — `Actor.ports`. Владелец для нативов `Signal.subscribe`/
  `Port.close` — `s.active` (ставит `runSlice`, вложенный слайс
  возвращает прежний; `callSync` — актор pid -1, subscribe там —
  `internal:`). События — `injectQueue` (mutex + `ready`-канал): ресурс
  в своей goroutine зовёт `push`, run-loop — `drainInject` в начале
  итерации `runMain`/`sessionLoop`/`awaitNested`/`YieldUntil`; закрытый
  порт — событие выброшено, иначе в конец ящика мимо HWM +
  `wakeIfBlocked`. Ожидание при пустой ready — `waitEvent`
  (`select { таймер | inject.ready }`) и `inject.ready` в `waitSession`/
  `waitYield`. Выход `runMain`: `main` Done и `len(s.ports) == 0`;
  `main` Failed; `s.halt`. Ни таймеров, ни портов — `deadlock`. Смерть
  актора закрывает его порты в `notifyWatchers` (рядом с `dropNames`);
  `runMain`/`endSession` закрывают все. `Sys.halt` — натив возвращает
  `*ErrHalt` (не raise, `catch` его не ловит), `runSlice` в `stepFailed`
  (`halting`) ставит `s.halt` без unwind — `ensure` не исполняются;
  сессия (`haltSession`) отдаёт `ErrHalt` текущему и всем следующим
  вводам (`loopErr`). ОС — только за интерфейсом `SignalHub`
  (`vm.SetSignals`), реализация на `os/signal` — `cmd/brig/signal.go`;
  в REPL `:sigint` не доставляется. Ядро не импортирует `os/signal`,
  `os/exec`, `net` и не открывает файлы (`os.Open*`/`Create`/…) — якорь
  `TestVMCoreNoOSPorts`. Событие порта — `(tag, port, ...)`: порт второй,
  `Signal` шлёт `(:signal, port, name)`.
- **Потоковые порты (§12.12, T-228, `stream.go`).** Общий протокол в
  `Port`: `request` (pull, флаг `streamPort.armed`, одно событие на запрос),
  `write` (iodata сплющивается на run-loop, счётчик `pending`, порог
  `streamHWM` — `Error(:busy)` и `promiseReady`), `give` (меняет
  `PortHandle.Owner`; событие берёт владельца в `drainInject`, поэтому
  запрос переходит с портом). Ресурс — интерфейс `Stream` (Read/Write/
  Close(done)), события — `StreamEvent` через inject-очередь; `Written` —
  служебное, уменьшает `pending`. `:port_eof`/`:port_error` закрывают
  порт. Вид `File` — `FileHub` (`vm.SetFiles`), реализация на `os` —
  `cmd/brig/file.go`, in-memory — `stream_test.go`; без реализации —
  `noStream` (`:enotsup`). Закрытие потокового порта — `Close(done)` через
  `Scheduler.closing`; `finishPorts` (выход `runMain`, `endSession`) ждёт
  дозаписи. Ожидание держат только `openPort.live()` порты: потоковый без
  запроса и без `pending` молчит — при пустой ready `runMain` выходит
  (main Done) или даёт `deadlock`. Новый потоковый вид — свой `*Hub` и
  `newStreamPort`, без правки протокола.
- **`HttpServer` (§12.12, T-229, `http.go`).** Слушатель — потоковый порт
  с ресурсом `listenerStream` (Read = `HTTPListener.Accept`); ответ
  ресурса — `HTTPEvent` в `injectEvent.http`, `drainInject` → `httpEvent`
  создаёт порт запроса (`newRequestPort`, владелец — владелец слушателя в
  момент разбора) и зовёт `HTTPRequest.Start`. Событие закрытого
  слушателя отбрасывается: не начатый (без `Start`) запрос закрывает `503`
  сам ресурс. Порт запроса — `streamPort.duplex` (`:port_eof` его не
  закрывает, `armed` снимается) с `req`; `write` включает только
  `HttpServer.respond` (не для 204/304), флаг `responded`. Закрытие не
  через `Port.close` (`closeActorPorts`, `closeAllPorts`) идёт через
  `abortPort` → `openPort.abort` (HTTP: 500 или обрыв; у `File` abort нет —
  дописывает). Реализация на `net/http` — `cmd/brig/http.go` (горутина
  обработчика исполняет команды из очереди, лимиты — `defaultHTTPLimits`),
  in-memory — `http_test.go` (`memHTTP`). Ядро не импортирует `net`,
  `net/http` (`TestVMCoreNoOSPorts`).
- **Реестр имён (§12.8, T-164, `registry.go`).** Опкоды `REGISTER`/
  `UNREGISTER`/`WHEREIS`; `Scheduler.names` — список `(name, pid)` с
  `runtime.KeyEqual`. Имена снимает `notifyWatchers` (`dropNames`) — в той же
  редукции, что и `:down`, поэтому после `:down` `whereis` → `None`.
- **`Global.put`/`get` (§12.11, T-167, `global.go`).** Нативы; таблица
  `Scheduler.globalTable` — список `(name, value)` с `runtime.KeyEqual`,
  на планировщике, не на акторе. `put` заменяет привязку, удаления нет;
  значение неизменяемо, ранее прочитанное `get` от нового `put` не меняется.
  Имя, не равное себе (`NaN` и контейнер с ним), — ловимый
  `(:type_error, (:put|:get, name))`.
- **`Telemetry` (§12.14, T-222, `telemetry.go`).** Реестр подписок —
  `Scheduler.tele`, не `Global`. `emit` без подписок — проверка аргументов
  без аллокаций и без кадра. С подписками — кадр `teleRun`: обработчики
  по порядку `attach`, снимок на входе в `emit`; непойманный `raise`
  снимает подписку и вкладывает `[:telemetry, :handler, :failed]`,
  `exit` кадр не ловит. `[:vm, :spawn]` и `[:vm, :mailbox, :hwm]` от
  `send` — в кадре излучателя (`dropResult`, `callDst` не затирается).
  Смерть и потерянный `Timer.send_after` — очередь `teleQ` служебного
  актора (`telePid`, `initial_fn` `<telemetry>`), без событий на его
  собственную смерть. Без совпавшей подписки измерения не собираются.
- **`await`/`reply` (§12.9, T-165, `await.go`).** Слот ответа —
  `runtime.RefSlot` за указателем `Value.Slot` в самом ref: его создаёт
  только `make_ref` (`makeRef`, `Owner` = pid), у ref из `watch` слота нет.
  Таблицы слотов у актора нет — ref без `await` память не держит. Опкоды
  `AWAIT A B C` (ждёт — возвращает `stepBlock` без `ip++` и исполняется
  снова, как `RECVTAKE`) и `REPLY A B` (аргументы `B..B+2`). Таймер `await`
  — тот же `recvDeadline`/куча; `Actor.awaiting` — слот, которого ждут;
  `wakeIfBlocked` такого актора не будит (почта и `:down` копятся), будят
  только `Reply`, `wakeExpired` и `hurry` (exit). `clearTimer` сбрасывает
  и `awaiting` — прерванное ожидание (exit, interrupt REPL) слот не
  закрывает. Слот закрывают только Ok и `Error(:timeout)` в `await`.
- **Бюджет хода и `Actor.info` (§12.10, T-169, `budget.go`).** Счётчики в
  `Actor.budget`: за ход (`turnReds`/`turnAlloc`) и прошлые ходы
  (`past*`); `newTurn` зовёт только `RECVTAKE` (сообщение или ветка
  `after`), `await` ход не завершает. Редукцию считает `burn` → `reduce`
  (равенство с лимитом: нулевой лимит = нет лимита, конструкторам `Actor`
  ничего инициализировать не надо). Байты — `charge`: поверхностный
  `sizeEstimate` результата конструирующих опкодов и нативов; новый
  опкод, строящий значение, тоже зовёт `charge`. Превышение —
  `signalExit` с `(:resource_limit, (kind, used, limit))`, дальше как
  `exit`. Лимиты — `SPAWN` с битом `C&SpawnLimits`, запись в `R[B+1]`
  (`parseLimits`, ошибка до создания актора). `Actor.info` — натив,
  модуль `Actor` в списках встроенных (compiler, sema, loader).
- **Хелперы observer (T-223, T-224)** живут в `internal/repl`, не в VM:
  `tree`/`info`/`top`/`observe`. Дерево — `internal/actorview` из снимка и
  `Supervisor.which_children` (`CallNested`). `initial_fn` супервизора —
  `Supervisor.start$lambda$0$` (`actorview.SupervisorInitialFn`, та же
  строка, что `Observer.supervisor?`). `observe()` на TTY не держит цикл:
  `YieldUntil` крутит остальных и `Snapshot`, TUI и его опросчик — на
  другой горутине; `Wake` будит цикл из опросчика. Лента падений —
  натив арности 3 в `Telemetry.attach`, без правок VM. `ActorInfoValue` —
  тот же `Actor.info` для натива на горутине цикла.
- **Интроспекция (§12.13, T-221, `introspect.go`).** `Actor.list`,
  `Actor.info` и Go-API `Snapshot` строятся одной `describe`: жив —
  `liveActor` (mainPid после выхода в таблице, но не жив); `status` из
  `actorStatus` + `awaiting`; `name` — первая запись `s.names` с этим pid;
  `watchers`/`watching` — из `target.watchers` (`watchEdges`, по ref, только
  живые концы), `Actor.watching` для этого не годится (повторный `watch`
  его перетирает). `initial_fn` — имя кадра при `Spawn`, у актора сессии
  `<repl>`.   `Snapshot()` — запрос в `s.snaps`, его обслуживают цикл сессии
  между слайсами (`serveSnapshots`), `waitSession`, `awaitNested` и
  `YieldUntil`; натив на горутине цикла зовёт `SnapshotHere`. Без сессии
  `Snapshot` — ошибка. `ErrSessionClosed` — сессию закрыли во время ввода.
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
  Таймеры живут в min-куче `Scheduler.timers` по `(deadline, seq)`
  (T-102 #171, T-166 #283): `recv … after`, `await` и `Timer.send_after`
  — одна очередь (G4). `nextDeadline`/`wakeExpired` не обходят
  `s.actors`. Взвод recv/await — только `armTimer`, снятие — только
  `clearTimer` (удаляет из кучи по `Actor.timerPos`; зовётся в
  `RECVTAKE` и `reapActor`); не писать `a.recvDeadline` напрямую.
  `Timer.send_after` — `armSend` (запись VM в `Scheduler.sends`,
  переживает актора); срабатывание — `Send` без отправителя (мёртвый
  pid и HWM — сообщение теряется); `Timer.cancel` снимает запись, пока
  она в куче. Якоря `TestTimerCostIndependentOfIdleActors`,
  `TestTimerRemovedWhenMessageArrivesFirst`, `TestTimerSendAfterOrder`,
  `TestTimerCancel`, `TestTimeMonotonic`, `TestTimerSendAfterCost`.
- Равенство: Int×Float сравниваются точно (T-85 #110, решение #43:
  `runtime.Equal`/`Compare`, быстрый путь для |Int| <= 2^53, иначе
  `big.Float`; `Inf` по знаку). `NaN != NaN`, `<`/`>`/`<=`/`>=` с NaN —
  false (`runtime.IsNaNOperand` в `LT..GE`); `Compare` ставит NaN после
  всех чисел (только для порядка sort/Set). `PatLiteral` и ключи
  Map-паттерна используют `runtime.MatchEqual` (тот же `Kind` и точное
  значение: `1` не матчит `1.0` и `dec"1"`; T-84 #109). Decimal×Float
  (T-86 #111): в `==`/`!=`/`<`… — ловимый `:type_error` (`checkMixedEq`/
  `checkMixedCmp`, только пара верхнего уровня); в ключах Map/Set
  (`INDEX`, `set`, `Map.put/get/remove`) — `runtime.KeyEqual`: Decimal
  равен только Decimal (в т.ч. не равен Int), Int×Float по-прежнему
  точное численное; в паттернах — `MatchEqual`. Ошибки нет, просто
  разные значения. Якорь `TestDecimalFloatMixed`.

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
