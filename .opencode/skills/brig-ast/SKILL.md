---
name: brig-ast
description: Use when working with the Brig AST in internal/ast/ — node types, visitor, equality, pretty-print, or serialization. Not for VM/bytecode work even when it consumes AST.
---

# AST Brig

Пакет `internal/ast/`. AST — первичный артефакт парсера; VM строится на нём
(или на bytecode из него). **Источник истины по грамматике — `brig.ebnf` (A1)**;
по семантике — `01-language-design.md`. Этот скилл не дублирует их, а описывает
инварианты и грабли пакета.

## Когда применять / не применять

**Применять:** правки `node.go`/`visitor.go`/`equal.go`/`pretty.go`, добавление
узлов, работа с `Position()`, golden-тесты AST, round-trip.

**Не применять:** работа с VM, байткодом, типами-чекером или парсером — там
свои скиллы. AST здесь только как вход/выход, детали не важны.

## Структура пакета

| Файл               | Назначение                                                                                                                              |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------- |
| `node.go`          | Интерфейсы `Expr`, `Stmt`, `Pattern`, `TypeDecl`; маркеры (`exprNode()`, `stmtNode()`, `patternNode()`); `Position()` → `file:line:col` |
| `visitor.go`       | `Visitor` / `Walk` для обходов (resolve, type check, compile)                                                                           |
| `equal.go`         | Структурное равенство AST (golden-тесты, round-trip)                                                                                    |
| `pretty.go`        | Pretty-print (дебаг, `check-examples` diff)                                                                                             |
| `ast_test.go`      | Round-trip, равенство, сериализация                                                                                                     |
| `testdata/golden/` | Эталонные JSON-снапшоты                                                                                                                 |

## Инварианты AST

Это критично. Ломать нельзя без обновления спецификации.

1. **`Position()` есть у каждого узла.** Потеря = диагностики без
   `file:line:col`. Проверяется в тестах round-trip.
2. **`()` ≠ `(x)` ≠ `(x,)`** — три разных узла:
    - `()` — Unit
    - `(x)` — группировка
    - `(x,)` — 1-кортеж
      То же для типов: `(Int)` — группировка, `(Int,)` — 1-кортеж типа.
3. **`Ok(1)` — встроенный вариант, не вызов.** Варианты без аргументов
   (`None`, `Red`) — значения (N1); с аргументами — функция. `Red` ≠ `Red{...}`
   (вариант vs номинальная запись) — в AST разные формы.
4. **Спреды `..` / `..name`** — только List-паттерн, только последним,
   только один раз (§3). В map/record-паттернах в brig недопустимы.
5. **Контекстные ограничения парсера сохраняются в AST.** Пример: `trap`
   как `Expr` имеет ограничения (КР-003), поэтому родитель несёт флаг
   `TrapOk` — не терять при рефакторинге.
6. **Иммутабельность.** AST строится без side-эффектов; обходы не мутируют
   узлы, а возвращают новые.

## Контекст языка (не проверяется в пакете)

- Встроенные имена: `Option`, `Result`, `Some`, `None`, `Ok`, `Error`, `Int`,
  `List<T>`, ... Затенение — **info-диагностика, не ошибка** (§2.9, §2.13).
- `nil` не существует; отсутствие значения — `Option` / `()` (принцип #4).
- Различение форм `type_body` по первому токену (§2.10):
  `{` + `Name(...)` → вариант; `{` + `name:` → запись; `=` → алиас.
- `as_pattern` — §2.12.

## Пример: добавление нового узла

Правильная форма (на примере уже существующего `TupleExpr`):

```go
// node.go
type TupleExpr struct {
    Pos    Position
    Elems  []Expr
    IsUnit bool // true для (), false для (x,) и (x, y, ...)
}
func (*TupleExpr) exprNode() {}
func (n *TupleExpr) Position() Position { return n.Pos }
```

При добавлении узла **обязательно**:

1. Реализовать маркер (`exprNode()`/`stmtNode()`/`patternNode()`) и `Position()`.
2. Добавить case в `equal.go` — иначе равенство слепое.
3. Добавить case в `visitor.go` (`Walk`) — иначе обходы пропускают узел.
4. Добавить case в `pretty.go` — иначе дебаг-вывод неполный.
5. Добавить golden-тест и round-trip тест.

## Сериализация

- `ast.Marshal(node)` → JSON. Используется в `testdata/golden/`.
- S-expression-форма (`Sexpr`, как в splink) — для быстрого диффа в тестах
  равенства. Пишется рядом с JSON при `-update`.
- Обновление golden:

    ```sh
    go test ./internal/ast/ -update
    git diff testdata/golden/   # глазами проверить — не «принять всё»
    ```

## Проверка

```sh
go test ./internal/ast/                    # всё
go test ./internal/ast/ -run RoundTrip     # только round-trip
go test ./internal/ast/ -run Golden        # только golden
go test ./internal/ast/ -update            # перезаписать golden
make generate-check                        # если есть кодогенерация
golangci-lint run ./internal/ast/...       # линтер
```

Перед PR: `go test ./... -race` — обходы могут быть параллельными.

## Частые ошибки

- **Новый узел пропущен в `equal.go` или `visitor.go`** — сравнения слепые,
  обходы не заходят в поддерево. Симптом: тесты проходят, а resolve/compile
  тихо не работает.
- **`(x)` и `(x,)` слиты в один узел** — ломает round-trip и pretty-print.
- **`Ok(1)` хранится как вызов** — ломает семантику §2.9 и type check.
- **Потерян `Position()`** — диагностики без `file:line:col`.
- **`trap` без флага `TrapOk` в родителе** — теряется КР-003; парсер потом
  не сможет применить контекстное ограничение.
- **Обновление golden без просмотра diff** — эталоны «догоняют» баги.

## Ссылки

- `brig.ebnf` (A1) — грамматика. Если здесь что-то противоречит ей — права она.
- `01-language-design.md` — семантика (§2.9, §2.10, §2.12, §2.13, §3, #4, #13).
- КР-003 — контекстные ограничения `trap` (см. раздел в design-doc).
