---
name: brig-parser-ast
description: >
  Правила для internal/parser/* и internal/ast/* — recursive descent
  парсер по brig.ebnf, построение и форматирование AST, round-trip
  (parse → Format → parse). Использовать при добавлении синтаксиса,
  правке грамматики, паттернов, или при падении round-trip/golden тестов.
---

# Brig — парсер и AST (`internal/parser`, `internal/ast`)

Нормативный источник: `brig.ebnf` + `docs/01-language-design.md` Part II A.
Парсер — один код с флагом `Mode` (`ModeModule` / `ModeRepl`), не два
разных парсера.

## Инварианты грамматики

- **Приоритеты операторов** (§7.1) — от слабого к сильному: `or` (2) →
  `and` (3) → сравнения (4, non-assoc) → `|>` (5) → `to` (6, non-assoc) →
  `+ -` (7) → `* / div rem` (8) → унарный `- not` (9) → `**` (10,
  right-assoc) → постфиксы (11). Цепочки сравнений и `to` — ошибка
  парсинга (`parseCmp`, `parseRange`), не разрешать даже «для удобства».
- **Пробел перед `(` незначим** — `f (a, b)` ≡ `f(a, b)`. Кортеж одним
  аргументом требует двойных скобок `f((a, b))`.
- **Спред `..`**: в паттернах — только последним элементом списка, не
  более одного раза (проверка частично в парсере, частично в `sema`). В
  конструировании — где угодно, сколько угодно. `f(..)` без операнда —
  ошибка парсинга (`..` требует выражения).
- **`trap`**: инлайн `trap(expr)` — синтаксический сахар над блочной
  формой с одним `trap_item`. Блочная форма: `ensure` — это `trap_item`
  внутри `INDENT`-блока `trap`, **не** клауза после `DEDENT` (в отличие от
  v0.4.6). См. `parseTrap` в `expr.go`.
- **`recv`**: `else`/`after` — клаузы на том же отступе, что сам `recv`,
  порядок фиксирован (`else` перед `after`), не более одной каждой. `else`
  требует `LOWER_IDENT` имени (нет `else _ ->`). **Guard ветки** живёт в
  `RecvBranchArg.Guard`; `Format` печатает `when <guard>`. Компилятор
  fail-fast, пока guard не компилируется (S-F3, T-02 #2; компиляция T-52
  #35). Отвергать guard в парсере нельзя: doc 01 (строка ~1084) содержит
  `when has_pending(...)`, `check-examples` упадёт.
- **Guard** (`when <or_expr>`): разбирается через `parseOr()`, не
  `parseExpr()` — иначе `tryLambda` съедает `ident ->` в формах
  `fn f(x) when x -> 1` и `n when ok -> …` (S-F4 закрыт T-20 #14).
- **`if`-сахар**: `if...then...else` — только целиком, с обеими ветками.
  Блочная форма `if`/`else` — на одном отступе (якорь — токен `if`).
- **Record vs constructor**: `Red` (без `{`) — значение-конструктор
  варианта; `Red{...}` — record-литерал. Не путать при парсинге
  `UPPER_IDENT`.

## Известные расхождения с `brig.ebnf` (каждое — свой issue)

| Что | Сейчас | Issue |
|---|---|---|
| Параметры и guard `fn` | `Params []ast.Pattern`, `Guard ast.Expr` (T-50 #33). Variadic — `SpreadPattern` (`..name`). Мультиклозы, guard и паттерны параметров компилирует `compileClauses` (T-51) | ✓ T-51 (#34) |
| `ensure` | Только `ensure expr`; блочная форма и гибрид `ensure expr`+блок — ошибка парсинга (S-F5 закрыт T-03 #3). Реализация блочной формы — out of scope | — |
| `stmt_list` | NEWLINE между стейтментами обязателен (S-F6 закрыт T-21 #15): после `parseStmt` — NEWLINE/DEDENT/EOF/`until` | — |

S-F1 (интерполяция): `SplitInterp` + `InterpExpr(parts, exprs)` — закрыт
T-53 (#36). Plain-строка без `\(...` остаётся `LiteralExpr`. Компиляция
concat/`to_str` — ✓ T-54 (#37).

## AST-инкапсуляция (`internal/ast`)

- Внешние пакеты видят только экспортируемые интерфейсы-аксессоры из
  `accessors.go` (`ast.BinaryExpr`, `ast.IfExpr`, `ast.TrapExpr` и т.д.),
  не конкретные приватные типы (`*binaryExpr`). Добавляя новый узел —
  всегда парная пара: приватный тип в `expr.go`/`stmt.go`/`pattern.go` +
  публичный интерфейс-аксессор в `accessors.go` + конструктор в
  `construct.go`.
- `Equal(a, b Node)` (в `equal.go`) — структурное равенство БЕЗ учёта
  `Pos`/`End`. При добавлении нового поля узла — не забыть обновить
  `equalNodes` и, при необходимости, `format.go`/`pretty.go`/`visitor.go`
  синхронно, иначе round-trip/golden тесты будут ложно зелёными или
  ломаться непредсказуемо.
- `Format(n Node)` должен быть идемпотентным: `format(parse(format(x))) ==
  format(x)` — есть тест `TestFormatIdempotent`. Любая правка форматтера
  обязана сохранить это свойство. Guard `fn` — `ast.Expr`; `Format` печатает
  `when <guard>` через `exprString` без лишних скобок (S-F12 / T-20, T-50).
- **`ast.Pretty` и `ast.Walk` выбирают интерфейс по порядку case.** Decl,
  Pattern и Type стоят раньше `Expr`, потому что у них есть
  `IsExpression()` и иначе `case Expr` перехватывает узел. `Expr` раньше
  `Stmt`: `*BlockStmt` реализует оба, рендер блока живёт в `prettyExpr`.
  У `*letBind`, `*exprStmt`, `*localFnDecl` метода `IsExpression` нет —
  иначе тело `fn` печатается пустым `(block )`. Не возвращать этот метод
  и не двигать `case Expr` выше Decl/Pattern/Type.
- Round-trip на `ast.Equal` не видит того, что парсер выбросил в обоих
  проходах (guard в `recv` до T-02). Зелёный round-trip ≠ «узел сохранён».
- `*tuplePattern` из одного элемента печатается с висячей запятой
  `(x,)` — без неё `(x)` перепарсится как grouping/identPat и потеряет
  узел при round-trip (см. FIX-B в changelog v0.4.7). Аналогичная ловушка
  возможна для новых узлов с «прозрачными» синтаксическими формами.

## Чек-лист при добавлении синтаксиса

1. Обновить `brig.ebnf` **и** соответствующий раздел
   `docs/01-language-design.md` Part I/II — грамматика должна оставаться
   единым источником истины.
2. Добавить парсинг в `internal/parser/*.go`.
3. Добавить приватный AST-тип + публичный аксессор + конструктор.
4. Обновить `format.go` (печать) и `pretty.go` (S-expression debug-вывод)
   и `visitor.go` (обход) — если этого не сделать, `Walk`/`sema`/
   `compiler` будут молча пропускать новый узел.
5. Обновить `equal.go` для round-trip тестов.
6. Добавить golden-кейс в `testdata/golden/*.brig` +
   `make update-golden`, diff прочитать и закоммитить отдельно.
   `*.ast` печатает декларации и тела: пустой `(program )` допустим только
   у программы без decl и stmt.
7. Прогнать `go test ./internal/ast/... ./internal/parser/... -run
   RoundTrip` и полный `make test-roundtrip test-parser`.
8. Если новая конструкция должна попасть в компилятор — сразу сообщить,
   что до правки `internal/compiler` она будет падать с «срез: неподдерживаемое
   выражение %T» (осознанный fail-fast в `compileExpr`).

## Частые ошибки

- Забыть, что `parseStmtList` скипает `NEWLINE` **в начале** итерации
  (FIX в v0.4.7, `parser/stmt.go`) — если переписывать цикл, важно
  сохранить эту устойчивость к висячим `NEWLINE` после вложенных блоков.
- Guard `fn` — `ast.Expr` в клозе (T-50); `Format` печатает `when <expr>`
  идемпотентно. Строковых helper'ов `normalizeGuardString` /
  `stripOuterParens` больше нет.
- `modeByMeta` в `internal/examples/examples.go` использует регексп с `\b`,
  чтобы `invalid_foo` не матчился как `invalid` — не убирать границу
  слова при правке меток fenced-блоков в докстрингах.
