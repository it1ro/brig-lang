# Brig — текущее состояние и дорожная карта

Рабочий документ для ориентирования. Не нормативный; при расхождении с
`docs/01-language-design.md` (v0.4.7) — источник истины спека, а не этот файл.

Обновлять при закрытии спринтов и значимых подпунктов.

---

## Что уже работает

| Слой                 | Статус | Что именно                                                                 |
| -------------------- | ------ | -------------------------------------------------------------------------- |
| `internal/lexer`     | ✅     | Токены A3, offside A5, все негативные кейсы, fuzz                          |
| `internal/parser`    | ✅     | Recursive descent по `brig.ebnf`, module/repl, golden, негативные          |
| `internal/ast`       | ✅     | Узлы, visitor, `Equal`, форматтер с round-trip, fuzz                       |
| `internal/sema`      | ✅     | Контекстный анализ §F.3 (trap, pipe, variadic, rebinding, shadowing-info)  |
| `internal/compiler`  | ✅     | Регистровый байткод, Range/Index, Vec/Map/Bytes/Json/Test dispatch         |
| `internal/vm`        | ✅     | Регистровая ВМ, TCO (`TAILCALL`), trap/ensure, акторы, scheduler, `Verify` |
| `internal/prelude`   | ✅     | `print/log/…` + Range/Set/Vec/Map/Bytes/Decimal/Json/Test                  |
| `internal/repl`      | ✅     | Persistent REPL (§11.4, N12)                                               |
| `cmd/brig`           | ✅     | `check`, `run`, `run --dump-bytecode`, `repl`; `BRIG_VERIFY=1`             |
| `cmd/check-examples` | ✅     | 65/65 блоков дизайн-дока                                                   |
| CI / Makefile        | ✅     | `check-smallint` в `all`; CI workflow → `make all`                         |

**Сделано в акторах (§12):** spawn / spawn_linked / send / self / make_ref /
watch / unwatch / mailbox_size / recv (+ else/after) / HWM / `:down` с
приоритетом / scheduler loop / callSync / tryUnwindRaise.

**Сделано в обработке ошибок (§10):** `raise`, `trap` (инлайн и блочный),
`ensure` с LIFO-семантикой, TCO-ограничение внутри активного `ensure`,
авто-raise `:division_by_zero` / `:recv_clause` / `:function_clause`.

**Сделано в TCO (§15.3):** миллион хвостовых вызовов в `TestTailRecursion`,
хвостовая рекурсия сквозь `recv` в `TestTailRecursionThroughRecv`.
Хвостовость распознаётся компилятором точно (`dest.tail`), эвристика
`isTailCall` удалена.

**Сделано в Sprint 5:** `Range` (§4.3), `Set` (§4.6), `Vec`/`Map` как
модули (§4.4/§4.5), `Bytes` (§3.2), `Decimal` (§3.1).

**Сделано в Sprint 5.5:** `Json.encode`/`Json.decode` (§4.7, Must),
тест-фреймворк `Test.describe`/`Test.it`/`Test.run` + assertions (§16, Must).

**Сделано в Sprint 6:** sema (контекстный анализ §F.3), persistent REPL
(`internal/repl/`, `CompileReplLine`, `RunMainWithArgs`).

**Сделано в Sprint 7:** регистровая VM (S7.1–S7.6). `Instr uint32`, 256
регистров на кадр, `CALL`/`TAILCALL`, `MATCHLOCAL` + JMP, `TRAPBEGIN`
`errReg`, bump-аллокатор, полный дизассемблер `--dump-bytecode` с
`line:col`, bytecode-goldens (`testdata/bytecode/*.txt`), `vm.Verify`
(линейный dataflow-проход). D-1..D-5 закрыты. Полный дизайн —
`docs/02-register-based-virtual-machine.md`.

---

## Спринт 4.5 — завершён

- [x] `Value.Int` → приватный `intBig` + `AsBig()` (small-int nil-deref)
- [x] `intDiv/rem/pow/neg` — small-int fast-path
- [x] `OpRecvTimer` — small-int `after`
- [x] Форматтер: `0 .A` вместо `0.A`
- [x] Sealed interfaces для `LiteralExpr`/`BytesExpr`/`RegexExpr`/`DecimalExpr`
- [x] `make changelog`

---

## Спринт 5 — завершён

Всё помечено **Must** в §16 дизайна. Без этого многие golden-файлы парсились,
но не исполнялись.

### 5.1 `Range` как значение первого класса (§4.3)

- [x] `runtime.KindRange`, `Value.RangeStart/RangeEnd int64`
- [x] `runtime.Equal` для Range (структурное)
- [x] Term order: сразу после `Bool`, перед атомами (§7.4)
- [x] `ast.RangeExpr` — accessor
- [x] `compiler.compileExpr` — `case ast.RangeExpr` → `RANGE`
- [x] `RANGE` в `opcodes.go`, обработка в `stepFrame`
- [x] `list(1 to 10)` → `[1, …, 10]` в `prelude`
- [x] Убывающий вычисляемый range → `raise((:range_error, (start, end)))`
- [x] Убывающий литеральный (`1 to 0`) → ошибка парсинга
- [x] `TestRangeMaterialize`, `TestRangeEquality`, `TestRangeError`
- [x] `examples/range.brig`

### 5.2 `Set` конструктор (§4.6)

- [x] `runtime.KindSet`, `Value.Set []Value`
- [x] `runtime.Equal` для Set (по набору элементов)
- [x] Term order: размер, затем по отсортированным элементам
- [x] `prelude.def("set", -1, ...)` — вариадический
- [x] `TestSetBasics`

### 5.3 `Vec` и `Map` как модули (§4.4, §4.5)

- [x] `isPreludeModule` dispatch в `compileCall`
- [x] `Vec.push/set/get/len`, `Map.put/get/remove/keys`
- [x] Immutability — все возвращают новое значение, не мутируют
- [x] `m["a"]` → `Option` (`INDEX` в компиляторе)
- [x] `TestVecPush`, `TestVecSet`, `TestVecGet`, `TestVecLen`
- [x] `TestMapGetPut`, `TestMapRemove`, `TestMapKeys`, `TestMapIndex`

### 5.4 `Bytes`, `Decimal`, `Regex` (§3.1–§3.3, feature-flagged)

- [x] `runtime.KindBytes`, `Value.Bytes []byte`
- [x] `compileBytesLiteral`, escape-декодирование (§C.3)
- [x] `Bytes.to_str` / `Str.to_bytes`
- [x] `runtime.KindDecimal`, `Value.Dec *big.Rat` (§3.1)
- [x] `internal/runtime/decimal.go`: `ParseDecimal`, `FormatDecimal`
- [x] `compileDecimalLiteral`, `parseLiteralValue` для Decimal-паттернов
- [x] Арифметика `+ - * / neg **`, `div`/`rem`/`intDiv` → `:type_error`
- [x] `checkMixedEq` (==/!=), `checkMixedCmp` (< > <= >=) — `Decimal×Float`
- [x] Decimal-правила Equal/Compare по значению (trailing zeros нормализуются)
- [x] `TestDecimal*`
- [ ] `Regex` — отложен (Should, feature-flagged; нужен движок)

---

## Спринт 5.5 — завершён

### 5.5.1 `Json.encode` / `Json.decode` (§4.7, Must из §16)

- [x] `runtime.JSONEncode` / `runtime.JSONDecode` в `internal/runtime/json.go`
- [x] Трансформации: `Unit ↔ null`, `Some(x)`/`Ok(x)` прозрачно,
      `Error(e) → {"error": …}`, `Bytes → {"$bytes": "base64"}`,
      варианты → `{"tag": …, "args": […]}`,
      `Function/Closure/Pid/Ref` → ошибка
- [x] `Json.encode` / `Json.decode` в прелюдии
- [x] `Json` в `isPreludeModule`
- [x] `internal/runtime/json_test.go`
- [x] `internal/compiler/json_test.go`
- [x] `examples/json.brig`

### 5.5.2 Тест-фреймворк (§16, Must)

- [x] `internal/vm/prelude_test_fw.go`
- [x] `Test.describe(name)` — задаёт текущую группу
- [x] `Test.it(name, () -> …)` — регистрирует тест
- [x] `Test.run()` — прогон, возвращает `Int(failed)`
- [x] `Test.assert_eq` / `assert_ne` / `assert` / `fail`
- [x] Поля `vm.tests` и `vm.currentGroup` в структуре `VM`
- [x] `Test` в `isPreludeModule`
- [x] `internal/compiler/test_framework_test.go`
- [x] `examples/test_demo.brig`

---

## Спринт 6 — завершён

### 6.1 Контекстный анализ (§F.3)

Слой между parser и compiler.

- [x] Пакет `internal/sema`
- [x] `trap` в позиции аргумента или элемента литерала → ошибка
- [x] Pipe-запрет акторных примитивов (`send |> f` → ошибка)
- [x] Variadic-параметр не последний → ошибка
- [x] Rebinding внутри одной лексической области → ошибка
- [x] Локальные `fn` не в начале тела → ошибка
- [x] Shadowing прелюдии → `info`-диагностика (не блокирует компиляцию)
- [x] Wire в `cmd/brig` — прогон перед compile
- [x] `internal/sema/sema_test.go`

### 6.2 REPL persistent (§11.4, N12)

- [x] Persistent VM-инстанс между строками (`internal/repl/repl.go`)
- [x] Каждая строка — новая top-level область
- [x] Замыкания захватывают лексический снимок строки (N12)
- [x] `CompileReplLine` в компиляторе
- [x] `RunMainWithArgs` в VM
- [x] `> x = 5` / `> f = () -> x` / `> x = 10` / `> f()` → `5`
- [x] Многострочный ввод при незакрытых скобках

---

## Спринт 7 — завершён

Регистровая VM по `docs/02-register-based-virtual-machine.md`.

- [x] S7.1: `Instr uint32`, `ABC/ABx/AsBx`, `Chunk[]Instr`, `MATCHLOCAL` +
      JMP, `$N`→`rN` в паттернах, `vm/regs_test.go`
- [x] S7.2: `compiler.go` — bump-аллокатор (`nextReg`, `releaseToMark`),
      `compileExpr(e, dest)`, `compileIf`/`compileTrap`/`compileRecv`/
      `compileLambda`/`compileCall`, `compilePattern`
- [x] S7.3: `scheduler.go` — `Frame`, `resolveCallee`, `bindArgs`,
      `frameFromFn`, ядро `stepFrame`, `TAILCALL`
- [x] S7.4: акторные опкоды — `SPAWN/SEND/SELF/MAKEREF/WATCH/UNWATCH/
    MAILBOXSIZE/RECVTIMER/RECVTAKE/MATCHLOCAL/YIELD`; правки
      `runSlice`/`callSync`/`tryUnwindRaise`
- [x] S7.5: дизассемблер `Function.Disassemble` с arity, `sort.Strings` в
      `cmd/brig/main.go`, bytecode-goldens `testdata/bytecode/*.txt`
- [x] S7.6: D-1..D-5 regression-тесты, `vm.Verify`, `BRIG_VERIFY=1`,
      рекурсивный `resolveUpvalue` (D-4), `--dump-bytecode` детерминирован
- [x] `docs/architecture.md`: снято «Осознанное отступление от §15.1»

---

## Открытые вопросы (решаются в коде, по измерениям)

1. **Пул `regs` в `Actor`** (free-list по размеру кадра). По умолчанию
   не включать. Решить по профилю `TestTailRecursion` и `fib(27)`.
2. **`Verify` (dataflow-проход) в `brig run` по умолчанию.** Сейчас только
   тесты и `BRIG_VERIFY=1`. Решить по замеру стоимости на `examples/*.brig`.
3. **Аудит `Arity` native в прелюдии.** D-2 превращает неверно объявленную
   арность из молчаливого дефекта в ошибку. Проверить все
   `def(name, N, ...)` в `prelude*.go`.

---

## После Sprint 7 — что осталось за рамками (K-8)

Дизайн §K-8 явно выводит за рамки миграции. Это темы следующих спринтов:

- [ ] Мультиклозные `fn` — сейчас компилируется только первый клоз.
- [ ] Параметры-паттерны в `fn` (`fn f(Some(x)) -> ...`).
- [ ] Локальные `fn` не захватывают локали.
- [ ] `match` / `with` — парсятся, но не компилируются.
- [ ] `when` в `recv` — игнорируется.
- [ ] `Regex` (§3.3) — Should, ждёт решения по движку (Go `regexp`/RE2).

**Should (не реализовано):** `Supervisor`, `Behavior`, `trace(pid)`,
порты (subprocess-FFI), паттерны в параметрах полной лямбды, сериализация
`Range`.

---

## Технический долг

### Опкоды — решено

Все мёртвые опкоды стековой ВМ удалены: `OpPop`, `OpDup`, `OpGetLocal`,
`OpSetLocal`, `OpSetUpvalue`, `OpCloseUpvalue`, `OpDefineLocalFn`.
Добавлены `MOVE` и `TAILCALL`. Итого 49 опкодов.

### `maxLocals`/`MaxRegs` — решено

`vm.MaxRegs = 256` (S7.1). `allocReg` вызывает `fc.fail(...)` —
`panic(compileError{...})`, `Compile` через `recover` превращает в
`error`. CLI печатает `compile: function "f" needs more than 256 registers`
с кодом выхода 3.

### Расхождение версий — решено

- `brig.ebnf` синхронизирован с `docs/01-language-design.md` (v0.4.7)
- `docs/architecture.md` обновлён, отражает Sprint 7
- `docs/02-register-based-virtual-machine.md` — источник истины по VM

### CI — решено

`.github/workflows/ci.yml` вызывает `make all`, `check-smallint` срабатывает.

### Открытые пункты

- [ ] `internal/prelude/doc.go` — пустой зарезервированный пакет;
      фактическая прелюдия в `internal/vm/prelude*.go`. Оставить как есть
      (документировано в обоих файлах).

---

## Что НЕ делать

- **Не начинать следующую большую миграцию параллельно с расширением
  прелюдии.** Одно переписывание за раз.
- **Не трогать `Regex` до отдельного решения по движку.** Should, не
  блокирует Must.
- **Не добавлять правила в `check-smallint` без необходимости.** Сначала
  стабилизировать, потом расширять.
- **Не переписывать `check-examples`.** 65/65 зелёный, документация
  синхронизирована.
- **Не менять AST-формы в рамках S7.x.** Миграция — только байткод и VM;
  AST, parser, sema, compiler — фиксированы.

---

## Полезные ссылки

- `docs/01-language-design.md` — единый источник истины (v0.4.7)
- `docs/02-register-based-virtual-machine.md` — дизайн регистровой VM
- `docs/architecture.md` — слои, зависимости, статус
- `Makefile` — цели `ci-quick`, `test-race`, `fuzz`, `update-bytecode`
- `brig.ebnf` — синхронизирован с v0.4.7

---

_Последнее обновление: после закрытия Sprint 7 (регистровая VM).
Следующий шаг — темы из K-8 (мультиклозы, match/with, параметры-паттерны)._
