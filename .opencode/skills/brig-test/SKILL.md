---
name: brig-test
description: Use when writing or running Brig tests — check-examples (A2), property-based tests, golden files (AST и bytecode), fuzzing, CI configuration, or testdata layout.
---

# Тестирование Brig

Два уровня: (1) `tools/check-examples` — прогон **всех brig-примеров из
дизайн-документов** через парсер (принцип #10 «примеры — валидный код»);
(2) классические Go-тесты `internal/**` с golden (AST и bytecode),
property-based и fuzzing. Этот скилл описывает правила и грабли тестов, а
не дублирует A2 и `tools/check-examples`.

## Когда применять / не применять

**Применять:** написание тестов в `internal/**/*_test.go`, `testdata/`,
`tools/check-examples/`, конфигурация CI, обновление golden (AST или
bytecode), добавление fuzz-целей, property-based проверки.

**Не применять:** правки самого кода, который тестируется (парсер, лексер,
AST, VM — свои скиллы). Разметка fenced-блоков в docs — скилл `brig-docs`;
здесь только запуск `check-examples`.

## Структура

| Путь                                 | Роль                                                |
| ------------------------------------ | --------------------------------------------------- |
| `tools/check-examples/`              | Гейт A2: прогон примеров из docs                    |
| `testdata/golden/`                   | AST-goldens (pretty, round.brig) для parser         |
| `testdata/bytecode/`                 | Bytecode-goldens `*.txt` для `Function.Disassemble` |
| `internal/**/testdata/golden/`       | Эталоны per-package (если появятся)                 |
| `testdata/negative/`                 | Обязательные негативные кейсы (parser/lexer)        |
| `internal/**/testdata/fuzz/<FuzzX>/` | Минимизированные входы после падений                |
| `internal/**/*_test.go`              | Unit + табличные + property-based                   |
| `.github/workflows/ci.yml`           | Матрица Go, linux+macos                             |

## Инварианты

Это критично. Ломать нельзя без обновления спецификации.

1. **`check-examples` — гейт принципа #10.** Все fenced-блоки ` ```brig `
   в дизайн-документах обязаны парситься. Exit 0 — успех.
2. **Режим блока определяется меткой** (`module`/`repl`/`expr`/`stmt`).
   Без метки — эвристика, неоднозначность → предупреждение. Разметка — в
   `brig-docs`; здесь только результат.
3. **`check-examples` кэшируется по `brig.ebnf` + дизайн-док.**
   Если менялся только один из них — кэш сбрасывается. Иначе гейт врёт.
4. **Fuzz-контрпример коммитится вместе с фиксом.** Падение кладёт
   минимизированный вход в `testdata/fuzz/<FuzzX>/<hash>` — это регресс-тест,
   без него баг вернётся.
5. **Golden-файлы обновляются осознанно и отдельным PR.** Молчаливое
   обновление в feature-коммите скрывает регрессию. Относится и к
   `testdata/golden/*.{ast,round.brig}`, и к `testdata/bytecode/*.txt`.
6. **Property-based тесты в CI — укороченные (seed), полный прогон — локально.**
7. **Тесты уровня `internal/**` не зависят от порядка.** Параллельный прогон
   (`-race`) обязателен в CI.
8. **Fuzz-цели вне CI** (по образцу splink), но запускаются локально перед PR.
9. **Bytecode-goldens стабильны.** `Function.Disassemble` выдаёт заголовок с
   `arity`, `params`, `regs`, `consts`, `patterns`; инструкции — с `line:col`.
   Порядок констант детерминирован (по первому использованию при обходе AST),
   порядок функций в выводе — `sort.Strings` (см. `cmd/brig/main.go`).
   Изменения компилятора **и** VM требуют `make update-bytecode`.
10. **`compiler.Verify = true` в тестах** (`verify_on_test.go`). Любая
    регрессия I-4 (регистр ≥ `nextReg`) или отсутствующая `JMP` после
    `MATCHLOCAL` падает на стадии `Compile`, а не в рантайме.

## tools/check-examples (A2)

**Цель:** прогнать fenced-блоки ` ```brig ` из `docs/01-language-design.md`
через парсер; exit 0 — все распарсились.

**Метки блока:**

- `module` — только `module`/`import`/`alias`/`type`/`fn`.
- `repl` — top-level `let` и выражения; строки могут начинаться с `>`.
- `expr` — одно выражение → оборачивается в `fn main() -> <expr>`.
- `stmt` — стейтменты → оборачиваются в `fn main() -> ...`.
- `invalid` — блок обязан не парситься.

**Обработка:**

- `module` → `program`.
- `repl` → каждая строка как top-level; вывод REPL (`6`, `11`)
  отбрасывается эвристикой (без `=`, без оператора, не ключевое слово).

**Семантические проверки скрипта** (идут поверх парсинга):

- нет `trap` в позиции аргумента или элемента литерала (КР-003);
- нет `Result[...]` в позиции типа (B4);
- нет `fn -> ...` (B2);
- нет `Ok(x) ≡ (:ok, x)` в fenced-блоках — только в комментариях;
- нет `f(..)` без операнда в args (М-004);
- round-trip `parse → Format → parse ≡ parse` для module/expr/stmt.

**Выход:** `file:line:col — status — [error message]`.
**Кэш:** `brig.ebnf` + дизайн-док не менялись → пропуск.
**CI:** на каждый коммит.
**CI-таргет:** `make check-examples`.

## Property-based

Что и зачем:

| Свойство                  | Инвариант                                                   |
| ------------------------- | ----------------------------------------------------------- |
| Иммутабельность коллекций | после `Vec.set` / `Map.put` старое значение неизменно (#13) |
| Offside round-trip        | токены → перепарсинг → те же токены (идемпотентно)          |
| Сложность Vec             | append O(1), read O(log n)                                  |
| Арифметика                | `/` всегда Float; `rem` — знак за делимым                   |
| Терм-порядок              | транзитивность `<` на смешанных списках                     |

Фреймворк: `gopter` или `testing/quick` (stdlib). В CI — укороченный
seed; полный — локально.

## Golden files

### AST-goldens (`testdata/golden/`)

- pretty-print AST (`*.ast`),
- round-trip formatter (`*.round.brig`).

**Обновление:** `make update-golden` — осознанное действие, **diff в PR
читается глазами**. Обновление golden — отдельный PR, не часть feature.

### Bytecode-goldens (`testdata/bytecode/`)

- `Function.Disassemble` для фиксированного набора модулей
  (`hello`, `arith`, `fib`, `tail`, `closure`, `trap_ensure`,
  `recv_after`).
- Фиксируют: instruction numbering, `line:col` per instruction, `TAILCALL`
  placement, `trap/ensure` layout, содержимое констант.

**Обновление:** `make update-bytecode` (флаг `-update-bytecode` в
`bytecode_test.go`). Diff читается глазами: `PatchJump` пересчитывает цели,
поэтому сдвиг нумерации — норма; структура `TRAPBEGIN`/`TRAPEND`/
`MAKEOK`/`MAKEERROR` и наличие `TAILCALL` — контракт.

### Порядок обновления golden

```sh
make update-golden      # AST: parser/format
make update-bytecode    # bytecode: compiler/VM
git diff testdata/
# глазами проверить: не «принять всё»
go test ./...           # без -update — должно пройти
git add testdata/
git commit -m "test: regenerate goldens for <reason>"
```

## Fuzzing

**Цели** (вне CI):

- `FuzzParse` — `internal/parser`.
- `FuzzRoundTrip` — `internal/ast`.
- `FuzzLex` — `internal/lexer`.

**Падение** кладёт минимизированный вход в
`testdata/fuzz/<FuzzX>/<hash>` — это регресс-тест, **коммитится вместе с
фиксом**.

```sh
go test ./internal/parser/ -run=^$ -fuzz=FuzzParse     -fuzztime=60s
go test ./internal/lexer/  -run=^$ -fuzz=FuzzLex       -fuzztime=60s
go test ./internal/ast/    -run=^$ -fuzz=FuzzRoundTrip -fuzztime=60s
```

При правке парсера/лексер/pretty **обязательно** прогнать все три цели
локально — CI их не запускает.

## Пример: добавление bytecode-golden-кейса

```sh
# 1. Добавить кейс в bytecodeCases (internal/compiler/bytecode_test.go)
$EDITOR internal/compiler/bytecode_test.go

# 2. Сгенерировать (или перегенерировать)
make update-bytecode

# 3. Прочитать diff глазами
git diff testdata/bytecode/new_case.txt

# 4. Прогнать без -update
go test ./internal/compiler/ -run TestBytecodeGolden

# 5. Отдельный коммит
git add testdata/bytecode/new_case.txt internal/compiler/bytecode_test.go
git commit -m "test(compiler): bytecode golden for <case>"
```

## Проверка

```sh
# Уровень 1: примеры из docs
make check-examples

# Уровень 2: Go-тесты
go test ./... -race
go vet ./...
make lint

# Golden
make update-golden       # осознанно, diff в PR читать
make update-bytecode     # осознанно, diff в PR читать
go test ./... -run Golden  # без -update — должны пройти

# Fuzz (локально, вне CI)
go test ./internal/parser/ -run=^$ -fuzz=FuzzParse -fuzztime=60s
```

Перед PR: `go test ./... -race` обязательно. Если менялся `brig.ebnf` —
сбросить кэш `check-examples` (см. инвариант 3).

## CI-матрица

`.github/workflows/ci.yml`:

- Go 1.27 (текущая, см. `mise`).
- ОС: linux + macos.
- Шаги: `make all` (внутри: `check-smallint`, `fmt`, `vet`, `test`,
  `lint`, `build`).

## Частые ошибки

- **`check-examples` кэш не сбрасывается при изменении только `brig.ebnf`.**
  Симптом: гейт зелёный, а примеры на самом деле не парсятся. Ловится
  ручным `make check-examples --no-cache` (или аналогом) в PR.
- **Fuzz-контрпример не закоммичен** — регрессия возвращается. Симптом:
  тот же баг всплывает снова через несколько PR.
- **Golden обновлён молча в feature-коммите** — скрывает регрессию. Симптом:
  ревьюер видит огромный diff golden, не понимает, что изменилось; правится
  отдельным PR (инвариант 5).
- **`make update-bytecode` не запущен после правок компилятора** — golden
  расходится с реальным выводом; тест `TestBytecodeGolden` падает.
  Симптом: в CI красное `testdata/bytecode/*.txt`.
- **Bytecode-golden обновлён без просмотра diff** — потерян `TAILCALL`
  или изменён trap-layout. Симптом: следующий тест TCO/`ensure` падает,
  а причина — в «принятом вслепую» golden.
- **Проверяющий пример помечен ` ```brig ` без метки и неоднозначен** —
  скрипт требует явной метки. Симптом: warning в CI, который «всегда был»;
  на самом деле блок мог парситься не так, как задумано. Правка — в
  `brig-docs`.
- **Fuzz-цель запущена в CI** — флакует и замедляет пайплайн. Симптом:
  CI нестабилен, падения без связи с изменениями. Fuzz — вне CI.
- **Property-based тест не изолирован** — использует общее состояние между
  прогонами. Симптом: тест проходит локально, падает в CI или наоборот.
- **`-race` не запущен локально перед PR** — гонка в тестах или в коде
  обхода AST. Симптом: CI падает на race-детекторе, локально всё зелёное.
- **Тест зависит от порядка выполнения** (`go test` без `-p 1` не
  воспроизводит). Симптом: тест падает только в полном прогоне.
- **`compiler.Verify` выключен в тестах** — регрессии I-4/definite
  assignment не ловятся на стадии `Compile`. Симптом: баг проявляется
  в рантайме как неверный результат. `verify_on_test.go` обязателен.

## Ссылки

- `docs/01-language-design.md` Part II:
  - **A2** — `check-examples`: метки блоков, эвристика, семантические проверки.
- `docs/01-language-design.md`: принципы #10 (примеры — валидный код),
  #13 (иммутабельность).
- `docs/02-register-based-virtual-machine.md` §9 — формат дизассемблера,
  §12 — риски и `Verify`.
- **B2** — удаление `fn -> expr`.
- **B4** — запрет `Result[...]` в позиции типа.
- **КР-003** — контекстные позиции `trap`.
- **М-004** — `..` без операнда только в паттерне.
- Скилл `brig-docs` — разметка fenced-блоков (источник для `check-examples`).
- Скилл `brig-cli` — exit codes и диагностики (`file:line:col — message`).
- Скилл `brig-parser` / `brig-lexer` / `brig-vm` — код, который здесь
  тестируется.
