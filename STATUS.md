# Brig — текущее состояние и дорожная карта

Рабочий документ для ориентирования. Не нормативный; при расхождении с
`docs/01-language-design.md` (v0.4.7) — источник истины спека, а не этот файл.

Обновлять при закрытии спринтов и значимых подпунктов.

---

## Что уже работает

| Слой                 | Статус | Что именно                                                                |
| -------------------- | ------ | ------------------------------------------------------------------------- |
| `internal/lexer`     | ✅     | Токены A3, offside A5, все негативные кейсы, fuzz                         |
| `internal/parser`    | ✅     | Recursive descent по `brig.ebnf`, module/repl, golden, негативные         |
| `internal/ast`       | ✅     | Узлы, visitor, `Equal`, форматтер с round-trip, fuzz                      |
| `internal/sema`      | ✅     | Контекстный анализ §F.3 (trap, pipe, variadic, rebinding, shadowing-info) |
| `internal/compiler`  | ✅     | Выражения, Range/Index, Vec/Map/Bytes/Json/Test dispatch, Bytes, Decimal  |
| `internal/vm`        | ✅     | Стековая, TCO, trap/ensure, акторы, scheduler loop, Json, Test            |
| `internal/prelude`   | ✅     | `print/log/…` + Range/Set/Vec/Map/Bytes/Decimal/Json/Test                 |
| `internal/repl`      | ✅     | Persistent REPL (§11.4, N12)                                              |
| `cmd/brig`           | ✅     | `check`, `run`, `run --dump-bytecode`, `repl`                             |
| `cmd/check-examples` | ✅     | 65/65 блоков дизайн-дока                                                  |
| CI / Makefile        | ✅     | `check-smallint` в `all`; CI workflow → `make all`                        |

**Сделано в акторах (§12):** spawn / spawn_linked / send / self / make_ref /
watch / unwatch / mailbox_size / recv (+ else/after) / HWM / `:down` с
приоритетом / scheduler loop / callSync / tryUnwindRaise.

**Сделано в обработке ошибок (§10):** `raise`, `trap` (инлайн и блочный),
`ensure` с LIFO-семантикой, TCO-ограничение внутри активного `ensure`,
авто-raise `:division_by_zero` / `:recv_clause` / `:function_clause`.

**Сделано в TCO (§15.3):** миллион хвостовых вызовов в `TestTailRecursion`,
хвостовая рекурсия сквозь `recv` в `TestTailRecursionThroughRecv`.

**Сделано в Sprint 5:** `Range` (§4.3), `Set` (§4.6), `Vec`/`Map` как
модули (§4.4/§4.5), `Bytes` (§3.2), `Decimal` (§3.1).

**Сделано в Sprint 5.5:** `Json.encode`/`Json.decode` (§4.7, Must),
тест-фреймворк `Test.describe`/`Test.it`/`Test.run` + assertions (§16, Must).

**Сделано в Sprint 6:** sema (контекстный анализ §F.3), persistent REPL
(`internal/repl/`, `CompileReplLine`, `RunMainWithArgs`).

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
- [x] `compiler.compileExpr` — `case ast.RangeExpr` → `OpRange`
- [x] `OpRange` в `opcodes.go`, обработка в `stepFrame`
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
- [x] `m["a"]` → `Option` (`OpIndex` в компиляторе)
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

## Спринт 7 — Регистровая VM (следующий, ~1–2 недели)

Из `docs/architecture.md`:

> Осознанное отступление от §15.1: текущая ВМ стековая, не регистровая.
> Миграция — отдельный подэтап после стабилизации семантики.

**Не начинать до зелёного `make all`** и без запушенных коммитов Sprint 5 / 5.5 / 6.

- [ ] Переписать `opcodes.go` и `chunk.go` под регистровую модель
- [ ] `stepFrame` целиком переписать
- [ ] Сохранить TCO (`isTailCall` смотрит на другой layout)
- [ ] Сохранить trap/ensure семантику (`stackLen` → `frameLen`)
- [ ] Сохранить `recv`/`after` (blocking + timer)
- [ ] `--dump-bytecode` переписать под новые инструкции
- [ ] Все существующие тесты должны пройти без изменений в них
- [ ] Снять в `docs/architecture.md` пометку про «отступление от §15.1»

---

## Технический долг

### Опкоды — решено

`OpCloseUpvalue` / `OpDefineLocalFn` удалены из `opcodes.go`
(компилятор их не эмитит).

### `maxLocals` — решено

Потолок увеличен с 64 до 256. `MaxLocals` экспортирован,
`declareLocal`/`allocTemp` паникуют с внятным сообщением при переполнении,
`OpSetLocal` даёт `internal: local N out of range` — но до этого не доходит.

### Расхождение версий — решено

- `brig.ebnf` синхронизирован с `docs/01-language-design.md` (v0.4.7)
- `docs/architecture.md` обновлён, отражает Sprint 5 / 5.5

### CI — решено

`.github/workflows/ci.yml` вызывает `make all`, `check-smallint` срабатывает.

### Открытые пункты

- [ ] `internal/prelude/doc.go` — пустой зарезервированный пакет;
      фактическая прелюдия в `internal/vm/prelude*.go`. Если требуется
      строгое соответствие README/architecture — оставить как есть
      (документировано в обоих файлах).
- [ ] `Regex` (§3.3) — Should, feature-flagged. Требует решения по
      движку (Go `regexp` / RE2 / собственная реализация) до реализации.

---

## Что НЕ делать

- **Не начинать регистровую VM параллельно с расширением прелюдии.**
  Два больших переписывания одновременно — гарантированный конфликт.
- **Не трогать `Regex` до старта Sprint 7.** `Should`, не блокирует Must.
- **Не добавлять правила в `check-smallint` до зелёного `make all`.**
  Сначала стабилизировать, потом расширять.
- **Не переписывать `check-examples`.** 65/65 зелёный, документация
  синхронизирована.
- **Не менять AST-формы в рамках регистровой VM.** Миграция — только
  байткод и VM; AST, parser, sema, compiler — фиксированы.

---

## Полезные ссылки

- `docs/01-language-design.md` — единый источник истины (v0.4.7)
- `docs/architecture.md` — слои, зависимости, статус
- `Makefile` — цели `ci-quick`, `test-race`, `fuzz`, `check-smallint`
- `brig.ebnf` — синхронизирован с v0.4.7

---

_Последнее обновление: после закрытия Sprint 5.5 (Json + Test framework)
и Sprint 6 (sema + persistent REPL). Следующий шаг — Sprint 7, регистровая VM._
