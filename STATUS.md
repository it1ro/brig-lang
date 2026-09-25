# Brig — текущее состояние и дорожная карта

Рабочий документ для ориентирования. Не нормативный; при расхождении с
`docs/01-language-design.md` (v0.4.7) — источник истины спека, а не этот файл.

Обновлять при закрытии спринтов и значимых подпунктов.

---

## Что уже работает

| Слой                 | Статус | Что именно                                                                    |
| -------------------- | ------ | ----------------------------------------------------------------------------- |
| `internal/lexer`     | ✅     | Токены A3, offside A5, все негативные кейсы, fuzz                             |
| `internal/parser`    | ✅     | Recursive descent по `brig.ebnf`, module/repl, golden, негативные             |
| `internal/ast`       | ✅     | Узлы, visitor, `Equal`, форматтер с round-trip, fuzz                          |
| `internal/compiler`  | ⚠️     | Подмножество выражений (см. пробелы ниже)                                     |
| `internal/vm`        | ⚠️     | Стековая, TCO, trap/ensure, акторы, scheduler loop                            |
| `internal/prelude`   | ⚠️     | `print/log/map/filter/find/all/any/fold/to_*/len/list` + `Ok/Error/Some/None` |
| `cmd/brig`           | ⚠️     | `check`, `run`, `run --dump-bytecode`, `repl` (только токены)                 |
| `cmd/check-examples` | ✅     | 65/65 блоков дизайн-дока                                                      |
| CI / Makefile        | ✅     | `check-smallint` включён в `all` и `ci-quick`                                 |

**Сделано в акторах (§12):** spawn / spawn_linked / send / self / make_ref /
watch / unwatch / mailbox_size / recv (+ else/after) / HWM / `:down` с
приоритетом / scheduler loop / callSync / tryUnwindRaise.

**Сделано в обработке ошибок (§10):** `raise`, `trap` (инлайн и блочный),
`ensure` с LIFO-семантикой, TCO-ограничение внутри активного `ensure`,
авто-raise `:division_by_zero` / `:recv_clause` / `:function_clause`.

**Сделано в TCO (§15.3):** миллион хвостовых вызовов в `TestTailRecursion`,
хвостовая рекурсия сквозь `recv` в `TestTailRecursionThroughRecv`.

---

## Спринт 4.5 — завершение текущего цикла (сейчас)

Три фикса применены, коммиты не сделаны.

- [x] `Value.Int` → приватный `intBig` + `AsBig()` (small-int nil-deref)
- [x] `intDiv/rem/pow/neg` — small-int fast-path
- [x] `OpRecvTimer` — small-int `after`
- [x] Форматтер: `0 .A` вместо `0.A`
- [x] Sealed interfaces для `LiteralExpr`/`BytesExpr`/`RegexExpr`/`DecimalExpr`

**Cleanup перед коммитом:**

- [ ] Убрать комментарии-маркеры `// internal/compiler/compiler_test.go` из новых тестов
- [ ] То же в `internal/vm/scheduler_test.go`
- [ ] Вернуть docstrings у `IntBig` / `AsBig` в `runtime/value.go`
- [ ] `numToFloat` → `v.AsBig()` вместо `v.intBig`
- [ ] `make fmt ci-quick`
- [ ] `make test-race` (ни разу не гонялся)
- [ ] `make fuzz` (3×60s)
- [ ] Коммиты:
  - `fix(runtime,vm): encapsulate small-int representation, fix nil-deref in arith and recv`
  - `fix(ast): insert space between decimal int literal and member access`
  - `fix(ast,compiler): seal literal accessor interfaces to disambiguate type switch`
- [x] `make changelog` (обновлён)

---

## Спринт 5 — Прелюдия и первые классы значений (~3–4 дня)

Всё помечено **Must** в §16 дизайна. Без этого многие golden-файлы парсятся,
но не исполняются.

### 5.1 `Range` как значение первого класса (§4.3)

- [ ] `runtime.KindRange`, `Value.Range{Start, End int64}` или через `Value` (для big-int границ)
- [ ] `runtime.Equal` для Range (структурное)
- [ ] Term order: сразу после `Bool`, перед атомами (§7.4)
- [ ] `ast.RangeExpr` — accessor (`Start()`, `End()`)
- [ ] `compiler.compileExpr` — `case ast.RangeExpr` → `OpRange`
- [ ] `OpRange` в `opcodes.go`, обработка в `stepFrame`
- [ ] `list(1 to 10)` → `[1, 2, ..., 10]` в `prelude`
- [ ] Убывающий вычисляемый range → `raise((:range_error, (start, end)))`
- [ ] Убывающий литеральный (`1 to 0`) → ошибка парсинга (уже реализовано?)
- [ ] Тест: `TestRangeMaterialize`, `TestRangeEquality`, `TestRangeError`
- [ ] `examples/range.brig` — завести и прогнать через `make run-examples`

### 5.2 `Set` конструктор (§4.6)

- [ ] `runtime.KindSet`, `Value.Set []Value`
- [ ] `runtime.Equal` для Set (по набору элементов)
- [ ] Term order: размер, затем по отсортированным элементам
- [ ] `prelude.def("set", -1, ...)` — вариадический
- [ ] Тест: `TestSetBasics` (уникальность, равенство)

### 5.3 `Vec` и `Map` как модули (§4.4, §4.5)

Сейчас `Vec.push(v, 4)` парсится как вызов по глобальному имени `Vec.push`,
которого нет. Нужно:

- [ ] В `compileCall`: если callee — `*memberExpr` с `*variableExpr` слева
      (`Vec.push`, `Map.put`), диспатчить по имени `"Vec.push"` как глобал
- [ ] Зарегистрировать в prelude: `Vec.push`, `Vec.set`, `Vec.get`, `Vec.len`,
      `Map.put`, `Map.get`, `Map.remove`, `Map.keys`
- [ ] Immutability: все возвращают новое значение, не мутируют
- [ ] `m["a"]` → `Option` (сейчас `indexExpr` не реализован в компиляторе)
- [ ] Тест: `TestVecPush`, `TestMapGetPut`, `TestMapIndex`

### 5.4 `Bytes`, `Regex`, `Decimal` (§3.2–§3.3, feature-flagged)

- [ ] `runtime.KindBytes`, `Value.Bytes []byte`
- [ ] `compiler.compileExpr` — `case ast.BytesExpr` → `OpBytes`
- [ ] Аналогично Regex (можно отложить — нужно движок регулярных выражений)
- [ ] Decimal — big.Rat или собственная реализация

**Приоритет:** Range > Vec/Map > Set > Bytes > Regex/Decimal.

---

## Спринт 6 — Контекстный анализ и REPL (~2–3 дня)

### 6.1 Контекстный анализ (§F.3)

Слой между parser и compiler. Сейчас его нет вообще.

- [ ] Новый пакет `internal/sema` (или `internal/context`)
- [ ] `trap` в позиции аргумента или элемента литерала → ошибка
- [ ] `..` не в конце списка/паттерна → ошибка
- [ ] Несколько `..` в одной коллекции → ошибка
- [ ] Pipe-запрет акторных примитивов (`send |> f` → ошибка)
- [ ] Variadic-параметр не последний → ошибка
- [ ] Shadowing прелюдии → `info`-диагностика (не блокирует компиляцию)
- [ ] Wire в `cmd/brig` — прогонять перед compile

### 6.2 REPL persistent (§11.4, N12)

Сейчас `brig repl` печатает токены. Нужен настоящий:

- [ ] Persistent VM-инстанс между строками
- [ ] Каждая строка — новая top-level область
- [ ] Замыкания захватывают лексический снимок строки (N12)
- [ ] Многострочный ввод по offside
- [ ] `> x = 5` / `> f = () -> x` / `> x = 10` / `> f()` → `5`

---

## Спринт 7 — Регистровая VM (~1–2 недели)

Из `architecture.md`:

> Осознанное отступление от §15.1: текущая ВМ стековая, не регистровая.
> Миграция — отдельный подэтап после стабилизации семантики.

**Не начинать до закрытия Спринта 5**, иначе семантика изменится и придётся
дважды переделывать опкоды под `OpRange`/`OpSet`/`OpVec*`.

- [ ] Переписать `opcodes.go` и `chunk.go` под регистровую модель
- [ ] `stepFrame` целиком переписать
- [ ] Сохранить TCO (`isTailCall` смотрит на другой layout)
- [ ] Сохранить trap/ensure семантику (`stackLen` → `frameLen`)
- [ ] Сохранить `recv`/`after` (blocking + timer)
- [ ] `--dump-bytecode` переписать под новые инструкции
- [ ] Все существующие тесты должны пройти без изменений в них

---

## Технический долг

### Не реализованные опкоды

- [ ] `OpCloseUpvalue` — `scheduler.go`, `fail("internal: opcode CLOSEUPVAL not implemented")`
- [ ] `OpDefineLocalFn` — там же

Сейчас не стреляет (компилятор использует `OpMakeClosure` + глобалы), но
висят в `opcodes.go`. Либо реализовать, либо удалить.

### `maxLocals = 64`

`vm.go:15`. Жёсткий потолок на число локалов в функции. `trap` с несколькими
`ensure` + `recv` с паттернами могут его исчерпать. `declareLocal` не проверяет
границу, `OpSetLocal` падает с `internal: local N out of range`.

- [ ] Проверить поведение при переполнении
- [ ] Либо увеличить до 256, либо динамические locals, либо внятная ошибка

### Расхождение версий

- [ ] `brig.ebnf` в корне — v0.4.5, `docs/01-language-design.md` — v0.4.7
- [ ] Обновить `brig.ebnf` из Part II §A
- [ ] `docs/architecture.md` — обновить чеклист «Статус Трека B»
      (акторы сделаны, но `[ ]`)

### CI

- [ ] Проверить `.github/workflows/`: вызывает ли `make all` или `make build test`
- [ ] Если второе — переключить на `make all`, иначе `check-smallint` не сработает

---

## Что НЕ делать

- **Не начинать регистровую VM параллельно с прелюдией.** Два больших
  переписывания одновременно — гарантированный конфликт.
- **Не трогать `Bytes`/`Regex`/`Decimal` до закрытия Range и Set.** Они
  `Should`, не блокируют Must-фичи.
- **Не добавлять правила в `check-smallint` до зелёного `make all`.** Сначала
  стабилизировать, потом расширять.
- **Не переписывать `check-examples`.** 65/65 зелёный, документация
  синхронизирована.

---

## Полезные ссылки

- `docs/01-language-design.md` — единый источник истины (v0.4.7)
- `docs/architecture.md` — слои, зависимости, статус трека
- `Makefile` — цели `ci-quick`, `test-race`, `fuzz`, `check-smallint`
- `brig.ebnf` — **устаревшая** v0.4.5, требует синхронизации

---

_Последнее обновление: после закрытия small-int / formatter / sealed-interfaces._
