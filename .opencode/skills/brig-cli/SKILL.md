---
name: brig-cli
description: Use when working on the brig CLI and REPL — cmd/brig, cmd/check-examples, REPL semantics (§10.7), exit codes, BRIG_VERIFY=1, --dump-bytecode, diagnostics output, and tooling. Spec: §10.7, §9 (Main entry point), A2, docs/02-register-based-virtual-machine.md §9.
---

# CLI и REPL Brig

Бинарь `brig` — референсный интерпретатор. Go-модуль
`github.com/itiro/brig-lang`. **Источник истины по REPL — §10.7**, по
точке входа `run` — §9, по `check-examples` — A2, по `--dump-bytecode` —
`docs/02-register-based-virtual-machine.md` §9. Этот скилл описывает
инварианты, форматы вывода и грабли, а не дублирует спецификацию.

## Когда применять / не применять

**Применять:** правки `cmd/brig/*`, `cmd/check-examples/*`, поведение REPL,
форматы диагностик, exit codes, env-переменные (`BRIG_VERIFY`),
`--dump-bytecode`, авто-raise-сообщения, прелюдии `print`/`eprint`/`log`.

**Не применять:** изменение лексера/парсера/VM/типов — для этого свои
скиллы (`brig-ast`, `brig-lexer`, `brig-vm`, ...). Здесь CLI — только
потребитель их API.

## Карта команд

```
brig run <file.brig>              # выполнить модуль: Main + fn main() (§9)
brig run --dump-bytecode <file>   # дамп регистрового байткода (детерминирован)
brig check <file.brig>            # распарсить + проверить, не исполняя
brig repl                         # интерактивный режим (§10.7)
brig help / brig version
```

`brig check` использует те же правила, что `tools/check-examples` (A2),
но на одном файле. Exit 0 — успех парсинга и семантической проверки.

### `brig run --dump-bytecode`

Флаг `--dump-bytecode` (алиас `-d`) печатает `Function.Disassemble` для
**всех** функций модуля в алфавитном порядке имён (`sort.Strings`). Это
единственный детерминированный порядок; итерация по `map` не годится для
bytecode-goldens (см. скилл `brig-test`).

**Формат заголовка** (см. §9 дизайна):

```
== <name> arity=<Function.Arity> params=<NumParams>[ variadic] regs=<NumRegs> consts=<len(Constants)> patterns=<len(Patterns)> ==
```

**Формат строки инструкции:**

```
%04d %3d:%-3d %-12s <operands>
```

- `%04d` — индекс инструкции (0-based).
- `%3d:%-3d` — `line:col` (1-based), с `%-12s` для мнемоники.
- Регистры печатаются как `rN`, константы как `kN`, паттерны как `pN`.

Пример для `fn fib(n) -> if n < 2 then n else fib(n - 1) + fib(n - 2)`:

```
== fib arity=1 params=1 regs=6 consts=3 patterns=0 ==
0000   2:12  LOADK       r3 k0 ; 2
0001   2:10  LT          r2 r0 r3
0002   2:5   JMPIFNOT    r2 -> 0004
0003   2:19  RETURN      r0
...
```

Разбор формата — в `internal/vm/chunk.go` (`Function.Disassemble`,
`Chunk.disInstr`). Строки не парсятся кодом, только глазами и golden-тестами.

### Переменные окружения

- **`BRIG_VERIFY=1`** — перед `RunMain` прогнать `vm.Verify` по всем
  функциям модуля в алфавитном порядке имён. При ошибке:

    ```
    brig run: verify <fn>: <error>
    ```

    Выход с кодом `3` (внутренняя ошибка). По умолчанию выключено; включать
    при отладке компилятора или правок `vm.Verify`.

## Инварианты CLI и REPL

Это критично. Ломать нельзя без обновления спецификации.

1. **Точка входа `run` — `module Main` + `fn main()` (§9).** Файл без
   `fn main()` — ошибка, не «ничего не делать».
2. **REPL: каждая строка — новая top-level область (принцип #12).**
   Имя связывается один раз в лексической области; в REPL каждая строка —
   новая область.
3. **REPL: замыкания — лексический снимок (N12).** Замыкание захватывает
   связывание своей строки. Позднее `x = 10` в следующей строке не влияет
   на ранее созданное замыкание. Реализация: захваченные REPL-переменные
   **копируются по значению**, а не по имени.
4. **Exit codes фиксированы:**
    - `0` — ok
    - `1` — ошибка парсинга / семантической проверки
    - `2` — runtime uncaught raise
    - `3` — внутренняя ошибка (panic, I/O, провал `BRIG_VERIFY=1`)
      Не путать 1 и 2 — это частая ошибка.
5. **Формат диагностик единый:** `file:line:col — <message>`.
   Для runtime — `raised (:function_clause, args)` + стек-подсказка;
   тексты авто-raise берутся из фиксированного списка §8.3, не сочиняются.
6. **`print` / `eprint` / `log`** — прелюдии (§9.1):
    - `print` → stdout
    - `eprint` → stderr
    - `log` → stdout, с префиксом `log:` (см. `internal/vm/prelude.go`)
7. **`--dump-bytecode` детерминирован.** Имена функций сортируются
   (`sort.Strings`). Формат — контракт для bytecode-goldens; менять
   заголовок или порядок вывода = сломать `TestBytecodeGolden`.

## REPL: мультистрочка

Правило: `internal/repl.IsContinuation` (см. `internal/repl/repl.go`)
определяет, нужна ли ещё строка. Продолжение запрашивается, если:

- незакрытая `(` / `[` / `{` / `%[` / `%{`;
- незакрытая интерполяция `\(` внутри строки;
- незакрытая строка/bytes/regex.

**Offside-блоки** (`fn`/`match`/`recv`/`with`/`trap`/`if` на отдельной
строке с INDENT-телом) в MVP REPL не поддерживаются — их надо писать
через файл и `brig run`. Правило «склейки строк» в REPL подчинено A5, а
не эвристике.

**Каждая строка — новая область.** `r.Eval` вызывает `CompileReplLine`
с текущим списком видимых имён; результат добавляется к `r.env`. Повторное
связывание имени — shadowing между областями, не rebinding (§11.4).

## Формат вывода

- Приглашение: `brig[<n>]>` для ввода, `...` для продолжения.
- Значения печатаются `res.Inspect()` — без префиксов; `()` не печатается.
- Диагностики sema в REPL: `<sev>: <repl>:<line>:<col>: <message>` в `os.Stderr`.
- Ошибки runtime в REPL: `! <error>`.

## Пример: CLI dispatch

Правильная форма (упрощённо, для навигации по коду):

```go
// cmd/brig/main.go
func main() {
    args := os.Args[1:]
    if len(args) == 0 {
        usage()
        os.Exit(exitParse)
    }
    switch args[0] {
    case "check":
        runCheck(args[1:])
    case "run":
        runFile(args[1:])   // флаг --dump-bytecode обрабатывается внутри
    case "repl":
        runRepl(args[1:])
    case "version", "--version", "-v":
        fmt.Printf("brig %s\n", version)
    case "help", "--help", "-h":
        usage()
    default:
        fmt.Fprintf(os.Stderr, "brig: неизвестная команда %q\n", args[0])
        usage()
        os.Exit(exitParse)
    }
}
```

При правке CLI **обязательно**:

1. Проверить exit codes — особенно `2` vs `1` на runtime raise и
   ошибках парсинга.
2. Проверить формат диагностик (`file:line:col — message`).
3. При правке флагов `run` — убедиться, что `--dump-bytecode` обрабатывается
   до sema/compile и печатает **все** функции (не только `main`).
4. `BRIG_VERIFY=1` прогоняет `vm.Verify` **после** сборки `ProgramImage`
   и **до** `--dump-bytecode`/`RunMain`.

## Структура

| Файл                         | Назначение                                                                 |
| ---------------------------- | -------------------------------------------------------------------------- |
| `cmd/brig/main.go`           | Entry point, dispatch, `runCheck`/`runFile`/`runRepl`, `BRIG_VERIFY`       |
| `cmd/brig/repl.go`           | Цикл REPL; буферизованный stdin, `repl.IsContinuation`, `repl.New`         |
| `cmd/check-examples/main.go` | Отдельный тул по A2                                                        |
| `internal/repl/repl.go`      | Persistent REPL: `Eval`, `IsContinuation`, `Bindings`, `Reset`             |
| `internal/vm/prelude*.go`    | `print`/`eprint`/`log`, коллекции, `Json`, `Test` — фиксация stdout/stderr |
| `internal/vm/chunk.go`       | `Function.Disassemble` (формат `--dump-bytecode`)                          |

## Сериализация / форматы (что тестировать)

- **Диагностики:** формат `file:line:col — message` — стабилен, `col`
  с 1, не с 0.
- **Exit codes:** покрыты табличными тестами (по одному на код).
- **`--dump-bytecode`:** детерминированный порядок имён, заголовок с
  `arity`/`params`/`regs`/`consts`/`patterns`; байткод-goldens — в
  `testdata/bytecode/` (скилл `brig-test`).
- **REPL:** строки без префиксов; `()` не печатается; N12-снимок
  замыканий (см. `internal/repl/repl.go`, `CompileReplLine`).

## Проверка

```sh
go build ./cmd/...
go test ./cmd/... -race
go test ./internal/repl/ -race

# Smoke-тесты REPL
echo 'print(1 + 1)' | go run ./cmd/brig repl   # 2
echo 'x = 1' | go run ./cmd/brig repl          # new top-level

# Dump bytecode
go run ./cmd/brig run --dump-bytecode examples/fib.brig

# Verify hook
BRIG_VERIFY=1 go run ./cmd/brig run examples/fib.brig

# Runtime raise → exit 2
go run ./cmd/brig run examples/trap.brig; echo "exit=$?"
```

Перед PR: `go test ./... -race` — REPL и parser могут иметь общие
разделяемые состояния.

## Частые ошибки

- **REPL-переменные мутируются последующими строками** — ломает N12.
  Симптом: замыкание, созданное на строке 1, видит значение из строки 3.
- **Exit code 1 для uncaught raise** — должно быть 2. Симптом: CI путает
  парсинг и runtime.
- **`brig run` без `fn main()`** — должен ругаться; симптом: тихий exit 0.
- **REPL склеивает входные строки без учёта offside** — `fn` ждёт INDENT,
  а не `\n`. Симптом: однострочный `fn f() = 1` работает, многострочный — нет.
- **Диагностика без `col` или с `col = 0`** — не совпадает с форматом
  `file:line:col`. Симптом: golden-тесты диагностик падают на первом же кейсе.
- **`--dump-bytecode` печатает в случайном порядке** — итерация по `map`.
  Симптом: bytecode-goldens флакуют; вывод двух запусков не совпадает
  байт-в-байт. Правится `sort.Strings` в `runFile`.
- **`--dump-bytecode` печатает только `main`** — должно быть **все**
  функции модуля, включая мангленные (`main$lambda$`, `outer$inner`).
  Симптом: golden-файл не покрывает вложенные функции.
- **Заголовок без `arity`** — старая форма `== name params=... ==`
  из `Chunk.Disassemble`. Правильная — `Function.Disassemble`:
  `== name arity=N params=... ==`.
- **`BRIG_VERIFY=1` не запускает `vm.Verify` до `RunMain`** — регрессии
  I-4/definite assignment не ловятся. Симптом: verify-провал не выводится,
  актор падает в рантайме с непонятной ошибкой. Смотреть `runFile`.
- **`log` уходит то в stdout, то в stderr** — источник выбора размазан по
  коду. Симптом: нестабильные интеграционные тесты вывода.
- **Замыкание захватывает имя, а не значение** — классическая ошибка
  реализации N12; ловится тестом `TestREPLClosures` (см. скилл `brig-test`).

## Ссылки

- §10.7 — REPL-семантика (каждая строка — новая область, N12).
- §9 — точка входа `run`, `Main` + `fn main()`.
- §9.1 — прелюдии `print` / `eprint` / `log`.
- §8.3 — фиксированный список авто-raise.
- A2 — `check-examples`.
- A5 — offside-правила; при конфликте с эвристикой REPL — права A5.
- Принцип #12 — имя связывается один раз в лексической области.
- `docs/02-register-based-virtual-machine.md`:
  - §9 — дизассемблер, формат `Function.Disassemble`, порядок функций.
  - §11 — план миграции, где `sort.Strings` в `cmd/brig/main.go` —
      единственная правка вне VM/compiler.
  - §12 — `BRIG_VERIFY=1` как основной инструмент отладки.
- Скилл `brig-vm` — что именно печатает `--dump-bytecode`, что проверяет
  `vm.Verify`.
- Скилл `brig-test` — bytecode-goldens, `make update-bytecode`.
- Скилл `git-conventional` — формат коммитов для правок CLI
  (`feat(cli): ...`, `fix(cli): ...`).
