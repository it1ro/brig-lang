---
name: brig-parser
description: Use when working on the Brig parser: recursive descent from brig.ebnf, context checks (trap position, feature flags), or parse-time diagnostics. Spec: Part II A.1, A6 in 01-language-design.md.
---

# Парсер Brig

Пакет `internal/parser/`. Исполнительная грамматика — `brig.ebnf` (**A1**);
нормативный источник — §16 Part II `01-language-design.md`. При расхождении правится
§16, затем регенерируется `brig.ebnf` и парсер.

## Устройство

- `parser.go` — префиксный/инфиксный recursive descent по приоритетам §5.0.
- `context_check.go` — семантические контекстные проверки (КР-003, Р-008).
- `ebnf.go` — (опционально) сгенерированный/проверяемый код из `brig.ebnf`.
- `parser_test.go` — позитивные/негативные тесты.

## Приоритеты (продиктованы §5.0)

```
2  or                 left
3  and                left
4  == != < > <= >=    non-assoc
5  |>                 left
6  to                 non-assoc
7  + -                left
8  * / div rem        left
9  **                 right
10 унарный - , not    prefix
11 . () []            left
```

- НЕ операторы: `= : -> .. as =>`. `=` — стейтмент (let_bind), `->` — lowest,
  right-assoc, `..`/`as` — маркеры, `=>` — только в `map_literal`.
- `a |> f == c` → `(a |> f) == c`; `1 to 10 |> list` → `(1 to 10) |> list`.
- `a == b == c` и `1 to 2 to 3` — **ошибка парсинга** (non-assoc).
- Пробел перед `(` незначим: `f (a, b)` ≡ `f(a, b)`.
  Скобки после идентификатора — всегда вызов; кортеж — двойные скобки `f((a, b))`.

## Грамматические обязательства (A1)

- `program ::= module_decl? import_decl* top_decl*`; top-level: только
  `module`/`import`/`alias`/`type`/`fn` в режиме `module`; top-level
  `let`/`expr` — ошибка парсинга. В режиме `repl` — разрешены (§9).
- `lambda_short ::= lower_ident "->" expr` — границы тела: до `,` `)` `]` `}`
  NEWLINE вне скобок; до `,`/закрывающей внутри (СУ-001).
- `lambda_full` — `fn (params) -> ...`; форма `fn -> expr` **убрана** (B2):
  пустая лямбда — `() -> expr`.
- `recv`: `else` перед `after`, обе на отступе `recv`, только блочная форма;
  более одного `else`/`after` — ошибка (§10.4).
- `trap`: `trap(expr)` — синтаксический сахар для блочной формы с одним
  стейтментом (КР-002), НЕ вызов функции. Грамматика
  `body_clause (ensure_clause body_clause?)*` (§8.2, СУ-002).
- `if`: блочная и однострочная `if e then a else b`; `else` на отступе `if`.
- `with`: binds только в начале; тело — с первого statement без `<-` (§7).
- Типы: параметризация только `<...>`, `(Int)` — группировка, `(Int,)` —
  1-кортеж типа; `Result[...]` в позиции типа — **запрещено** (B4, A2).

## Контекстные проверки (context checks)

- **КР-003:** `trap` грамматически в `primary_expr`, но **семантически** только
  как RHS `let_bind` или отдельный `expr_stmt`. `trap` в аргументе вызова или
  элементе литерала — ошибка контекстной проверки.
- **Р-008:** `regex_lit` и `decimal_lit` — feature-flagged (Should). Если фича
  не реализована — парсер выдаёт `feature not implemented` (не ошибку парсинга).
- **Pipe (§5.4):** в RHS `|>` запрещены акторные примитивы прелюдии —
  `send`, `spawn`, `spawn_linked`, `link`, `watch`, `unwatch`, `self`,
  `make_ref`, `mailbox_size`. Статическая проверка на этапе разбора.
  Ошибка — при разборе; разрешено `f`, `f(a)`, `M.f(a)`, `obj.method(a)`.
- `..` без операнда допустим только в паттерне (М-004); `f(..)` без операнда
  в args — ошибка.

## Проверка

```sh
go test ./internal/parser/
go test ./... -run=Examples
python3 /tmp/opencode/msg_filter.py  # не здесь — это git-скилл, см. git-conventional
```

Прогон примеров из дизайна — через `tools/check-examples` (**A2**, скилл
`brig-test`).

## Частые ошибки

- Инфикс с non-assoc связан как left — `1 to 2 to 3` парсится.
- `trap` в `args` пропущен без контекстной проверки (КР-003).
- `(x, y)` трактуется как список аргументов, а не кортеж — двойные скобки.
- `regex_lit` отсутствует в `literal` (Р-008 — синхронизация §16.5 и A1).
- Парсер и `brig.ebnf` разошлись — правится §16, регенерируется EBNF (A6).