# Brig

<p align="center"><img src="docs/brig-logo.png" height="100" style="border-radius: 12px;" alt="Brig logo"/></p>

Референсный интерпретатор Brig.

Brig — язык с отступами, неизменяемыми значениями, акторами и оптимизацией хвостовых вызовов. Переменные нельзя переприсваивать. Дизайн и спецификация — в `docs/01-language-design.md`.

---

## Что такое Brig

Один абзац вместо списка фич:

> Brig использует отступы вместо блоков, гарантирует неизменяемость значений, работает на виртуальной машине с байткодом и оптимизирует хвостовые вызовы вне активного `ensure`. В нём есть акторы: порождение, `recv`, `watch`, порог перегрузки. Вместо `nil` — `Option` и `Result`. Функции могут иметь несколько вариантов, есть сопоставление с образцом. Модули — пространства имён без состояния. Акторы — рантайм. Один бинарник, общий heap, без внешних вызовов в первой версии.

Принципы — §0 в дизайн-документе. Фичи по приоритетам — §16.

---

## Быстрый старт

```sh
git clone <repo> brig && cd brig

make build            # bin/brig + bin/check-examples
make test             # тесты
make ci-quick         # fmt-check + vet + быстрые тесты + все examples
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
| `make ci-quick`       | быстрая проверка без полного линтера (для pre-commit)  |
| `make test-race`      | race-detector — важен для акторов и замыканий          |
| `make check-examples` | прогон всех ```brig-блоков дизайн-дока через парсер    |
| `make fuzz`           | 3 фаззера по 60s: лексер, парсер, round-trip           |
| `make update-golden`  | пересборка `testdata/golden/*.{ast,round.brig}`        |
| `make changelog`      | генерация `CHANGELOG.md` (нужен `git-cliff`)           |

---

## Структура

Один проход `lexer → parser → ast → compiler → vm`. Никаких циклов, никаких «вышестоящих» импортов. Диаграмма зависимостей — в `docs/architecture.md`.

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
  repl/                 # REPL с сохранением состояния
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

`docs/01-language-design.md` — главный документ: Part I (дизайн), Part II (формальная спецификация, EBNF, лексер, offside, диагностика), Part III (changelog). Дизайн-код живёт там же.

---

## Что важно знать о реализации

- **Стек, а не регистры.** Спецификация §15.1 требует регистровую ВМ. Текущая реализация — стековая: так быстрее стабилизировать семантику. Миграция — отдельный подэтап. См. `docs/architecture.md`.
- **Аннотации типов — только документация.** §14.4: их не проверяют ни статически, ни в рантайме. Runtime-контракты — Should.
- **`internal/prelude/doc.go` пуст.** Прелюдия лежит в `internal/vm/prelude.go`, чтобы не плодить цикл `vm → prelude → vm`.

---

## Лицензия MIT

#### Author: Ilmir Karimov
