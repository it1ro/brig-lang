---
name: brig-vm
description: Use when working on the Brig VM/interpreter — register bytecode, Instr encoding, TCO via TAILCALL, trap/ensure frames, actor scheduler, vm.Verify, or memory model. Spec: §13 (execution), §8.2–8.4 (effects), §10 (actors), docs/02-register-based-virtual-machine.md, principles #4/#7/#11/#13.
---

# VM Brig

Пакет `internal/vm/`. Референсный интерпретатор Brig реализуется **на Go**
(§13). Дизайн-инвариант: **регистровая** байткод-VM (§15.1 Must), общий heap
(§13, принцип #13). Дизайн — `docs/02-register-based-virtual-machine.md`.
Этот скилл описывает инварианты и грабли VM, а не дублирует §5.2–5.3, §8,
§10, §13.

## Когда применять / не применять

**Применять:** правки `internal/vm/{chunk,opcodes,pattern,scheduler,vm,verify,prelude*}.go`;
инструкции `Instr`, кодирование ABC/ABx/AsBx; `stepFrame`; TCO через
`TAILCALL`; `TRAPBEGIN`/`TRAPEND`; `RECVTAKE`; планировщик акторов; mailbox;
HWM; авто-raise; `vm.Verify`.

**Не применять:** сами значения и коллекции — скилл `brig-runtime`;
парсер, AST, лексер, CLI — свои скиллы. VM — потребитель значений и
байткода; компилятор — скилл `brig-parser`/компиляторные тесты; здесь
только исполнение.

## Структура

| Файл                       | Назначение                                                                           |
| -------------------------- | ------------------------------------------------------------------------------------ |
| `internal/vm/opcodes.go`   | `OpCode` (49 опкодов), `opNames`                                                     |
| `internal/vm/chunk.go`     | `Instr uint32`, `ABC/ABx/AsBx`, `Chunk`, `SrcPos`, `Function.Disassemble`            |
| `internal/vm/pattern.go`   | `CompiledPattern`, `MatchPattern`, `FormatCompiledPattern`                           |
| `internal/vm/scheduler.go` | `Frame`, `Scheduler`, `Actor`, `stepFrame`, `runSlice`, `callSync`, `tryUnwindRaise` |
| `internal/vm/verify.go`    | `Verify`, `RegUse` — линейный dataflow-проход                                        |
| `internal/vm/vm.go`        | `VM`, `New`, `RunMain`, арифметика, `checkMixedEq`/`checkMixedCmp`                   |
| `internal/vm/prelude*.go`  | Прелюдия (`print`, коллекции, `Json`, `Test`)                                        |
| `internal/vm/regs_test.go` | Кодирование `Instr`, `PatchJump`                                                     |

Bytecode-goldens — `testdata/bytecode/*.txt` (см. скилл `brig-test`).

## Инварианты

Это критично. Ломать нельзя без обновления спецификации.

1. **Иммутабельность значений (#13).** VM не мутирует значения. Все
   «изменяющие» операции над коллекциями возвращают новое значение
   (детали — скилл `brig-runtime`). VM обязана сохранять структурный sharing.
2. **TCO через явный `TAILCALL` (#11) — везде, кроме активного trap-региона.**
   Компилятор эмитит `TAILCALL` в хвостовых позициях (`dest.tail`);
   `stepFrame` при `TAILCALL` проверяет `len(f.handlers) == 0` и иначе
   завершает актор внутренней ошибкой. Эвристика `isTailCall` из стековой
   эпохи **удалена** — не возвращать.
3. **`nil` нет (#4).** Отсутствие значения — `Option`/`Result`/`()`. VM **не
   вводит внутренний nil**, видимый пользователю.
4. **Функции — по identity (#7).** Равенство по адресу, нестабильно между
   запусками (§2.11). То же для `Pid`/`Ref`.
5. **Авто-raise — фиксированный список имён (§8.3).** Ни одна авто-raise
   не придумывается на месте; список закрыт:
    - `(:division_by_zero, ())`
    - `(:index_out_of_bounds, (idx, len))`
    - `(:function_clause, args)`
    - `(:case_clause, val)`
    - `(:recv_clause, msg)`
    - `(:guard_failed, ...)`
    - `(:range_error, (start, end))`
    - `(:invalid_utf8, b)`
6. **`trap` и `ensure` — LIFO.** `trap` → `Ok(v)`/`Error(e)`; `ensure`
   LIFO при любом выходе. **Ошибка в `ensure` замещает исходную** (последняя
   побеждает, §8.2). Реализация — `Frame.handlers` с `errReg`, без
   `stackLen`.
7. **`send` при HWM — `Error(:busy)`, не `Ok(())`** (принцип #8, §10.3).
8. **`:down` — приоритетное системное сообщение.** Доставляется **в начало
   очереди**, временно превышая HWM. При аномальном переполнении
   контрольных сообщений — падение `(:system_mailbox_overflow, count)`
   (§10.2). Не обрабатывать `:down` как обычное user-сообщение.
9. **Автоматической эскалации ошибок между акторами нет.** Наблюдатель
   узнаёт только через `:down` (после `watch`). Скрытых stash-механизмов на
   уровне языка нет.
10. **`link` ≡ `watch` без ref (MVP).** Наблюдение, не автоматическая
    смерть. `spawn_linked(fn)` ≡ `spawn(fn)` + `link` со стороны родителя
    (§10.2).
11. **Переполнение ящика — контейнированное падение актора**, наблюдаемое
    через `watch` (#8). Не «тихий сброс» и не «рост без предела».
12. **Инструкция — 4 байта, `Instr uint32`.** Форматы ABC/ABx/AsBx.
    `Chunk.Code` — `[]Instr`, `ip` — индекс инструкции (не байтовое
    смещение). Лимит — 256 регистров на кадр (`vm.MaxRegs`).
13. **`Chunk.NumRegs` фиксирован на функцию** — high-water mark
    регистрового аллокатора компилятора. `Frame.regs` создаётся ровно
    такого размера.
14. **K-5: native получает свежий слайс аргументов.** Передавать слайс на
    окно регистров нельзя (`runtime.List(args...)` его удерживает).

## Регистровая модель (§1–§9 в `docs/02-register-based-virtual-machine.md`)

- **Соглашение о вызовах.** `CALL A B C`: callee в `R[A]`, аргументы в
  `R[A+1..A+B]`, результат в `R[C]`. `TAILCALL A B` — замена кадра.
- **`MATCHLOCAL A Bx`** — сопоставление `R[A]` с `Patterns[Bx]`; **всегда
  пара с следующей `JMP` (fail-переход)**. `Verify` проверяет пару.
- **`TRAPBEGIN A sBx`** — handler `{ip: ip+1+sBx, errReg: A}`. `raise`
  кладёт значение в `regs[errReg]` и переходит на handler.
- **`RECVTAKE A sBx`** — реализует K-6: сначала `downMsgs`, потом `mailbox`;
  дедлайн в `Actor.recvDeadline`; `stepBlock` при пустом и без таймаута.
- **`SPAWN A B C`** — `C == 1` — linked (с `watch` со стороны родителя).

## `vm.Verify` (`internal/vm/verify.go`)

Линейный верификатор, включается `compiler.Verify = true` (в тестах через
`verify_on_test.go`) или `BRIG_VERIFY=1` в CLI. Проверяет:

- регистры и окна в `< NumRegs`;
- цели переходов внутри кода;
- за каждым `MATCHLOCAL` идёт `JMP`;
- `TAILCALL` вне активных trap-регионов (линейный счётчик `TRAPBEGIN`/`TRAPEND`);
- нет падения с конца кода;
- **definite assignment** — прямой анализ «регистр определён на всех путях».
  `TRAPBEGIN` добавляет handler-ребро `ip+1+sBx` с `errReg = A`; линейное
  ребро `errReg` не определяет. Это соответствует семантике: значение
  попадает в `errReg` через raise-механизм `Frame.catch`/`tryUnwindRaise`.

`vm.RegUse(i Instr) (reads, writes []int)` — общая таблица чтения/записи;
её же использует компилятор в `emit` для инварианта I-4.

## Арифметика и сравнение (§5.2–5.3)

- **`/` всегда `Float`.** Никогда не возвращает `Int`, даже при делении без
  остатка.
- **`div` — целочисленное к нулю; `rem` — знак за делимым.**
  `-7 rem 3 == -1`, `7 rem -3 == 1`.
- **Переполнение `Int` невозможно** — произвольная точность.
- **Деление на ноль** — `raise((:division_by_zero, ()))`.
- **`Int`/`Float` — один числовой домен сравнения.**
  - `Decimal × Float` → `(:type_error, ...)`.
  - `Decimal × Int` — по значению.
- **Term order — полный список из 16 пунктов (§5.3)**, включая `Range` по
  `(start, end)` и варианты `None < Some`, `Ok < Error`. При добавлении
  нового вида значения — синхронизировать §5.3 и `Compare` в `vm.go` (см.
  `brig-runtime`).
- **Убывающий вычисляемый range `1 to 0`** — `raise((:range_error, (start, end)))`
  (§2.5). Литеральная форма — ошибка **парсинга**, не VM.

## Операции над значениями

- **`v[i]` вне границ** для `Vec`/`Bytes`/`Str` —
  `raise((:index_out_of_bounds, (i, len)))` (§5.2).
  - `Str` индексируется **по кодпоинту**, `Bytes` — по байту.
- **`m["a"]` возвращает `Option`** (Some/None), не nil (§2.7).
- **`send(pid, msg)` → `Result<(), Atom>`:**
  - `Ok(())` — успешно.
  - `Error(:busy)` — при HWM (принцип #8, §10.3).
- **`mailbox_size()`** — размер пользовательской очереди.

## Пример: TCO в регистровой VM

Правильная форма (упрощённо, для навигации по коду):

```go
// scheduler.go — case TAILCALL
if len(f.handlers) != 0 {
    return fail(errors.New("internal: TAILCALL under active trap"))
}
if native { /* вызвать native, return stepDone */ }
c, err := resolveCallee(cv)
// nregs = c.chunk.NumRegs
if nregs <= cap(f.regs) {
    regs = f.regs[:nregs]
    bindArgs(regs, c.chunk, args)   // memmove-безопасно при перекрытии
    clear(regs[c.chunk.NumParams:cap(regs)])
} else {
    regs = make([]runtime.Value, nregs)
    bindArgs(regs, c.chunk, args)
}
f.regs, f.chunk, f.name, f.captures, f.ip =
    regs, c.chunk, c.name, c.captures, 0
// f.callDst НЕ трогаем — результат уйдёт туда же
return stepContinue
```

При правке TCO/`ensure` **обязательно** проверять:

1. Хвостовой вызов вне `ensure` — кадр не растёт (глубина стека O(1));
   `TestTailRecursion` (10⁶ вызовов) проходит за секунды.
2. Хвостовой вызов внутри активного trap — невозможен; `Verify` и VM
   ловят это как ошибку.
3. Ошибка внутри `ensure` — замещает исходную (последняя побеждает);
   `TestTrapEnsureLifo*`, `TestEnsureAllRunOnFailure`.
4. `trap` + `ensure` вместе — порядок LIFO сохраняется.

## Акторы (§10)

- **Модули** — неймспейсы без состояния.
- **Акторы** — процессы с PID, состоянием и ящиком (принцип #5).

| Операция           | Семантика                                 |
| ------------------ | ----------------------------------------- |
| `link(pid)`        | ≡ `watch(pid)` без ref — наблюдение (MVP) |
| `watch(pid)`       | наблюдение; `:down` придёт при смерти     |
| `spawn(fn)`        | новый актор, без связи                    |
| `spawn_linked(fn)` | `spawn(fn)` + `link` со стороны родителя  |
| `send(pid, msg)`   | `Ok(())` / `Error(:busy)` при HWM         |

- **Автоматической эскалации ошибок между акторами нет.** Наблюдатель
  узнаёт только через `:down`.
- **Переполнение ящика — контейнированное падение актора**, наблюдаемое
  через `watch` (#8).

## Проверка

```sh
go test ./internal/vm/ -race
go test ./internal/vm/ -run 'TestInstr|TestPatchJump'    # кодирование Instr
go test ./internal/vm/ -run TestScheduler                # акторы
go test ./internal/compiler/ -v                          # D-1..D-5, TCO
go test ./... -run TermOrder
BRIG_VERIFY=1 go run ./cmd/brig run examples/fib.brig
go run ./cmd/brig run --dump-bytecode examples/fib.brig  # детерминированный вывод
make update-bytecode                                     # после правок компилятора
go vet ./internal/vm/...
```

Негативные кейсы **обязательны**: `1 to 0`, `v[len]`, деление на ноль,
несовпадение клоза, `send` при HWM, `TAILCALL` в trap-регионе.

Перед PR: `go test ./... -race` — планировщик и mailbox работают
параллельно.

## Частые ошибки

- **TCO-хвост внутри активного trap-региона** — `Verify` и VM ловят это
  как `internal: TAILCALL under active trap`. Компилятор не должен
  эмитить `TAILCALL` при `trapDepth > 0`.
- **Возврат эвристики `isTailCall`** — запрещено; `TAILCALL` эмитит
  компилятор (`dest.tail`), распознавание в VM ломает всё.
- **`/` вернул `Int` при делении без остатка** — `/` всегда `Float`.
  Симптом: `10 / 2 == 5` даёт `Int`, а не `Float`; golden-тест типа падает.
- **`send` при HWM вернул `Ok(())` вместо `Error(:busy)`** — нарушает
  принцип #8. Симптом: потерянные сообщения без диагностики; тест
  переполнения ящика падает.
- **`:down` обработан как обычное user-сообщение** — должен быть
  приоритетным (в начало очереди, временно превышая HWM). Симптом:
  наблюдатель узнаёт о смерти актора с задержкой; при переполнении —
  `:system_mailbox_overflow`.
- **Term order нарушен для `Range`/вариантов** — ломает `sort` смешанных
  списков. Симптом: `[None, Some(1), Ok(2)]` сортируется не как в дизайне.
- **`1 to 0` обработан как пустой range** — должно быть
  `(:range_error, (start, end))`. Симптом: тихий пустой результат вместо
  ошибки; golden-тест семантики падает.
- **`Str` индексируется по байту, а не по кодпоинту** — ломает не-ASCII.
  Симптом: `"日本語"[1]` даёт половину руны; VM падает или возвращает мусор.
- **`m["a"]` возвращает `nil` вместо `None`** — нарушает #4. Симптом:
  `Option.match` не срабатывает; pattern-matching на `Some`/`None` ломается.
- **Кадр не сохраняется в `ensure` при ошибке** — ошибка в `ensure` не
  замещает исходную. Симптом: пользователь видит исходную ошибку, а
  диагностика `ensure` теряется (§8.2).
- **Новый вид значения не добавлен в `Compare`/`Equal`** — паникует или
  сортируется в конец. Симптом: `sort` на смешанном списке с новым типом
  падает в VM (см. также `brig-runtime`).
- **Авто-raise с придуманным именем** — «на месте» вместо фиксированного
  списка §8.3. Симптом: потребитель не может поймать ошибку по известному
  имени; тест авто-raise не проходит.
- **`MATCHLOCAL` без следующей `JMP`** — `Verify` падает с
  `MATCHLOCAL at N: next op is X, want JMP`. Симптом: компилятор эмитит
  `MATCHLOCAL` в непарном виде.
- **Регистр `errReg` в `TRAPBEGIN` не входит в def-assignment** — `Verify`
  ругается `MAKEERROR reads undefined register rN`. Исправлено в S7.6
  (handler-ребро с `errReg = A`); при регрессии смотреть
  `verifyDefiniteAssignment`.

## Ссылки

- `docs/02-register-based-virtual-machine.md` — дизайн регистровой VM:
  - §1 — формат `Instr`, кодирование, лимиты.
  - §2 — модель кадра.
  - §3 — соглашение о вызовах.
  - §4 — TCO и `TAILCALL`.
  - §5 — `trap`/`ensure` (схемы с флагом, D-5).
  - §6 — акторные опкоды.
  - §7 — регистровый аллокатор компилятора (I-1..I-4).
  - §9 — дизассемблер.
  - §12 — риски и `Verify`.
- `01-language-design.md`:
  - §2.5 — `Range` (убывающий — ошибка).
  - §2.7 — Map и `Option`.
  - §2.11 — таблица видов значений.
  - §5.2 — арифметика и индексация.
  - §5.3 — term order (16 пунктов).
  - §8.2 — `trap` / `ensure` (LIFO, замещение).
  - §8.3 — фиксированный список авто-raise.
  - §8.4 — TCO и `ensure`.
  - §10 — акторы.
  - §10.2 — `link`/`watch`/`:down`, `:system_mailbox_overflow`.
  - §10.3 — HWM и `send`.
  - §13 — исполнение на Go, heap.
  - §15.1 — регистровая VM — Must.
  - Принципы: #4, #5, #7, #8, #11, #13.
- Скилл `brig-runtime` — значения и коллекции (что именно VM исполняет).
- Скилл `brig-parser` — литеральная форма `1 to 0` — ошибка парсинга.
- Скилл `brig-test` — property-тесты TCO, акторов; `make update-bytecode`.
- Скилл `brig-cli` — exit codes, `BRIG_VERIFY=1`, `--dump-bytecode`.
