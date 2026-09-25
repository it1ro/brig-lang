# Brig

<p align="center"><img src="docs/brig-logo.png" height="100" alt="Brig logo"/></p>

Референсный интерпретатор языка программирования Brig на Go.

Brig — иммутабельный, offside-ориентированный язык с акторами, TCO и
единственным присваиванием. Дизайн и формальная спецификация — в
`docs/01-language-design.md`; она **нормативна**.

---

## Что такое Brig

Один абзац вместо списка фич:

> Язык сочетает indentation-driven синтаксис, иммутабельные значения как
> языковую гарантию, стековую байткод-ВМ с гарантированным TCO вне активного
> `ensure`, акторную модель в духе Erlang/Elixir (спавн, `recv`, `watch`,
> HWM), встроенные алгебраические варианты (`Option`/`Result` вместо `nil`),
> мультиклозные функции и паттерн-матчинг. Модули — неймспейсы без
> состояния; акторы — рантайм. Один бинарник, общий heap, ноль FFI в MVP.

Полный список принципов — §0 в дизайн-документе. Полный список фич по
приоритетам — §16 там же.

---

## Быстрый старт

```sh
git clone <repo> brig && cd brig

make build            # bin/brig + bin/check-examples
make test             # go test ./...
make ci-quick         # fmt-check + vet + focused tests + все examples
make all              # полный прогон: check-smallint, fmt, vet, test, lint, build
```

Запуск программы:

```sh
make run FILE=examples/hello.brig
# или
./bin/brig run examples/hello.brig
./bin/brig run --dump-bytecode examples/trap.brig
./bin/brig check examples/actors.brig
./bin/brig repl
```

Все цели Makefile — в `Makefile`. Кратко о ключевых:

| Цель                  | Что делает                                             |
| --------------------- | ------------------------------------------------------ |
| `make all`            | полный локальный прогон всего, что должно быть зелёным |
| `make ci-quick`       | быстрая проверка без `golangci-lint` (для pre-commit)  |
| `make test-race`      | race-detector — важен для акторов и замыканий          |
| `make check-examples` | прогон всех ```brig-блоков дизайн-дока через парсер    |
| `make fuzz`           | 3 фаззера по 60s: лексер, парсер, round-trip           |
| `make update-golden`  | пересборка `testdata/golden/*.{ast,round.brig}`        |
| `make changelog`      | генерация `CHANGELOG.md` (требует `git-cliff`)         |

---

## Структура

Один проход `lexer → parser → ast → compiler → vm`. Никаких циклов,
никаких «вышестоящих» импортов — диаграмма зависимостей в
`docs/architecture.md`.

```
cmd/                    # точки входа: brig, check-examples
internal/
  lexer/                # токены + offside
  parser/               # recursive descent → AST
  ast/                  # узлы, форматтер, Equal, visitor
  sema/                 # контекстный анализ (§F.3)
  compiler/             # AST → байткод
  vm/                   # стековая ВМ + scheduler акторов + прелюдия
  runtime/              # Value, Kind, Equal, Serialize
  repl/                 # persistent REPL
  examples/             # A2-инструмент: fenced-блоки из docs
docs/                   # спецификация, архитектура
examples/               # .brig-программы, гоняются через make run-examples
testdata/               # golden, negative, fuzz-сиды
```

---

## Источники истины

README — обзор. Если он противоречит чему-то ниже — верь тому, что ниже.

| Что искать                  | Где смотреть                 |
| --------------------------- | ---------------------------- |
| Спецификация языка (v0.4.7) | `docs/01-language-design.md` |
| Архитектура и слои          | `docs/architecture.md`       |
| Текущий статус и роадмап    | `STATUS.md`                  |
| История изменений           | `CHANGELOG.md`               |
| Все команды и цели          | `Makefile`                   |
| Модуль Go и версия Go       | `go.mod`                     |

`docs/01-language-design.md` — единый нормативный документ: Part I (дизайн),
Part II (формальная спецификация, EBNF, лексер, offside, диагностика), Part
III (changelog). Дизайн-код (design decisions) живёт там же.

---

## Замечания к реализации

- **Стек, а не регистры.** Спецификация §15.1 требует регистровую ВМ
  (BEAM/Lua-style). Текущая реализация — стековая: вертикальный срез для
  быстрой стабилизации семантики. Миграция — отдельный подэтап. См.
  `docs/architecture.md`.
- **Аннотации типов — documentation-only.** §14.4: не проверяются ни
  статически, ни в рантайме. Runtime-контракты — Should.
- **`internal/prelude/doc.go` пуст.** Фактически прелюдия — в
  `internal/vm/prelude.go`, чтобы не плодить цикл `vm → prelude → vm`.

---

## Лицензия MIT

#### Author: Ilmir Karimov
