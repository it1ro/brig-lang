# Wave 9 — язык: остаток Must и модули

[← карта плана](README.md)

**Откуда:**
- задачи «остатка Must» из PR #167 (там они были T-101, T-103…T-106,
  T-108) — перенумерованы и согласованы с research;
- реализация решений Wave 8.

**Вход:**
- Wave 7 закрыта: корпус и компиляция примеров проверяют каждую задачу;
- DD Wave 8, от которых зависит задача, закрыты;
- спека для задачи внесена: T-127 или T-128.

**Выход:**
- примеры спеки §4.1, §6.2, §6.3, §11.1, §14.2 исполняются, их
  `pending` сняты;
- `rg -n 'срез: только простые связывания' internal/compiler` пуст;
- программа из нескольких файлов собирается и запускается;
- `brig check` ловит неизвестные имена;
- в корпусе все файлы `corpus/lang/` на уровне `run`, файлы
  `corpus/lookout/` и `corpus/whelk/` — на уровне `parse` (кроме тех, что
  ждут T-138 или горизонта: у них метка `needs`).

## Порядок и параллельность

`internal/compiler/compiler.go` правят T-130 (частично), T-131, T-133,
T-134, T-136, T-137, T-139 — их мержат по одной (`MAINTAINING.md` §5).
Вне компилятора: T-132 (лексер), T-135 (`cmd/brig`, новый `internal/loader`),
T-138 (лексер, после T-132).

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-130 | Прелюдия: порядок аргументов по T-120 | T-120 | sonnet | medium | T-132, T-135 |
| 2 | T-131 | Развилки T-121 в VM и компиляторе | T-121 | sonnet | medium | T-132, T-135 |
| 3 | T-132 | Лексер: `pub`, `quote`, `_name` | T-121, T-128 | sonnet | low | T-130, T-131, T-135 |
| 4 | T-133 | Связывание с паттерном и `(:badmatch, v)` | T-128 | opus | medium | T-135, T-138 |
| 5 | T-134 | `alias` и `import` встроенных модулей | T-128 | sonnet | low | T-135, T-138 |
| 6 | T-135 | Загрузчик модулей из файлов | T-122, T-128 | opus | medium | T-133, T-134, T-138 |
| 7 | T-136 | Пользовательские варианты | T-123, T-128 | opus | medium | T-135, T-138 |
| 8 | T-137 | Компиляция программы из нескольких модулей | T-134, T-135, T-136 | opus | large | T-138 |
| 9 | T-138 | Лексер: offside-мини-блоки внутри скобок | T-124, T-127, T-132 | opus | large | T-133…T-137 |
| 10 | T-139 | `brig check`: неизвестные имена и арность | T-137 | opus | medium | — |

## Задачи

### T-130 · Прелюдия: порядок аргументов по решению T-120
<!-- meta
priority: P0
type: full-fix
effort: medium
model: sonnet
wave: 9
depends_on: T-120
findings: G-1, S-1
-->
- **Файлы:** `internal/vm/prelude.go:131-235` (`map`, `filter`, `find`, `all`, `any`, `fold`); все вызовы в `internal/**/*_test.go`, `testdata/**`, `examples/*.brig` (38 мест на `fefb355`: `rg -nE '\b(map|filter|find|all|any|fold)\((fn \(|[a-z_]+ ->|\(\) ->)'`); `docs/01-language-design.md` — только снять `pending(T-130)`
- **Тест-якорь:** создать `TestPreludePipeMap`, `TestPreludePipeFold` (`[1, 2] |> map(x -> x * 2)` → `[2, 4]`); существующие тесты прелюдии — мигрировать
- **DoD:**
  - порядок аргументов всех коллекционных функций — по T-120 (при варианте A: коллекция первой, `fold(xs, acc, f)`);
  - вызов в старом порядке даёт ловимый `(:type_error, (:map, f))` с понятным вторым элементом, а не падение внутри колбэка;
  - `rg -n 'pending\(T-130\)' docs` пуст; корпус: `corpus/lang/pipe_map.brig` на уровне `run`;
  - `make update-bytecode` (если goldens затронуты) — diff просмотрен; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** добавлять новые функции прелюдии (`List.*` — T-146); менять акторные примитивы; оставлять оба порядка «для совместимости».

### T-131 · Развилки T-121 в VM и компиляторе
<!-- meta
priority: P2
type: full-fix
effort: medium
model: sonnet
wave: 9
depends_on: T-121
findings: G-4, G-5, G-7, G-8
-->
- **Файлы:** `internal/compiler/compiler.go` (guard в `compileClauses` и `compileRecv` — если T-121 п.1 = A или B); `internal/vm/vm.go` (`ADD` для `Str` — T-121 п.3); `internal/vm/scheduler.go:1342,1365` (имя ошибки поля — п.4), `mailbox_size` (п.2); тест T-118 — убрать закрытые пункты из allowlist
- **Тест-якорь:** создать `TestGuardErrorPolicy`, `TestStrPlusPolicy`, `TestMissingFieldErrorName`, `TestMailboxSizeDead`
- **DoD:**
  - каждый из п.1–4 T-121 ведёт себя по решению; пробы g10, g11, h03, h04 из `AUDIT_REPORT-2.md` дают ожидаемое решением;
  - allowlist T-118 не содержит этих пунктов;
  - `make update-bytecode` — diff просмотрен; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** трогать лексер (`_name` — T-132); менять `if` в интерполяции (п.5 не требует кода); решать `Range` в JSON (T-175).

### T-132 · Лексер: ключевые слова `pub`, `quote` и `_name`
<!-- meta
priority: P1
type: full-fix
effort: low
model: sonnet
wave: 9
depends_on: T-121, T-128
findings: G-6, R-7, S-4
-->
- **Файлы:** `internal/lexer/token.go` (ключевые слова), `internal/lexer/lexer.go:367` (`_name`); `internal/parser` — только если `pub` уже разбирается как имя; `internal/sema` — «использование `_name` в теле» (если T-121 п.6 = wildcard)
- **Тест-якорь:** создать `TestLexPubQuoteKeywords`, `TestLexUnderscoreName`, `TestSemaUnderscoreNameNotBound`
- **DoD:**
  - `pub` и `quote` — ключевые слова (`fn pub()` и `quote = 1` → ошибка лексера/парсера с `line:col`); `:pub`, `:quote` — атомы;
  - `_unused`, `_msg` лексятся как `LOWER_IDENT` (§1.2); семантика — по T-121 п.6: при варианте «wildcard» обращение к `_name` в теле — ошибка sema;
  - тест T-118: `pub`/`quote` сняты из allowlist;
  - `go test ./internal/lexer -run '^$' -fuzz FuzzLex -fuzztime 30s` → PASS; `make all` → 0.
- **НЕ делать:** реализовывать `pub fn` (T-143) и `quote` (горизонт); менять правила атомов.

### T-133 · Связывание с паттерном и `(:badmatch, v)`
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 9
depends_on: T-128
findings: G-13 (было T-101 в PR #167; research L21)
-->
- **Файлы:** `internal/compiler/compiler.go` (`compileLetBind`, `compileTrapLetBind`, `compilePattern`); `internal/sema/sema.go` (`checkPatternBinding`)
- **Тест-якорь:** создать `TestLetBindTuplePattern`, `TestLetBindListSpread`, `TestLetBindBadmatch`, `TestTrapLetBindPattern`
- **DoD:**
  - `(a, b, c) = (1, "a", :ok)`, `[h, ..t] = [1, 2, 3]`, `{name: n} = User{name: "Ada", id: 1}`, `Ok(x) = f()` связывают имена;
  - несовпадение → ловимый `(:badmatch, value)`; то же внутри блока `trap`;
  - повторное имя в одном паттерне — ошибка sema с `line:col`;
  - `rg -n 'срез: только простые связывания' internal/compiler` пуст; `pending(T-133)` в спеке сняты;
  - `make update-bytecode` — diff просмотрен; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** менять парсер паттернов; трогать `with`-binds и параметры лямбд (T-141); вводить спред в Map/record-паттернах (§5.1).

### T-134 · `alias` и `import` встроенных модулей (§11.1)
<!-- meta
priority: P2
type: feature
effort: low
model: sonnet
wave: 9
depends_on: T-128
findings: G-13 (было T-104 в PR #167)
-->
- **Файлы:** `internal/compiler/compiler.go:1702` (`isPreludeModule`), `compileMember`; `internal/ast/decl.go` (аксессоры `importDecl`, `aliasDecl`); `internal/sema`
- **Тест-якорь:** создать `TestAliasBuiltinModule`, `TestImportUnknownModule`
- **DoD:**
  - `alias Json as J` + `J.encode([1])` → `"[1]"`; `alias`/`import` неизвестного модуля — `error: file:line:col: module X not found`, exit 1 (до T-135 — любой не встроенный модуль);
  - доступ к встроенным модулям без `import` — как до задачи;
  - `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** загружать файлы (T-135); менять набор функций встроенных модулей.

### T-135 · Загрузчик модулей из файлов (§11.1)
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 9
depends_on: T-122, T-128
findings: G-13 (было T-105 в PR #167; research L2; циклы — по T-122 п.2)
-->
- **Файлы:** новый пакет `internal/loader` (граф модулей); `cmd/brig/main.go` (`run`, `check`); `internal/parser` — только вызов
- **Тест-якорь:** создать `internal/loader/loader_test.go`: `TestLoadResolvesPathToModule`, `TestLoadExplicitModuleOverridesPath`, `TestLoadImportCycle`, `TestLoadMissingModule`; фикстуры в `testdata/modules/`
- **DoD:**
  - корень — каталог входного файла; `import Util` находит `util.brig`, `import Http.Client` — `http/client.brig`; явный `module X` переопределяет имя, расхождение с путём при импорте — ошибка;
  - цикл импортов ведёт себя по T-122 п.2 (при варианте A: допустим, модули загружаются один раз); отсутствующий модуль — `error: file:line:col: module Util not found`, exit 1;
  - `brig check main.brig` парсит и прогоняет sema по всем модулям графа;
  - пока нет T-137, `brig run` для графа из ≥ 2 модулей завершается явной ошибкой `срез: несколько модулей` (exit 1);
  - `make all` → 0.
- **НЕ делать:** компилировать межмодульные вызовы (T-137); пакетный менеджер и пути поиска (горизонт); кэш байткода на диске.

### T-136 · Пользовательские варианты: конструкторы, паттерны, term order (§14.1–14.2)
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 9
depends_on: T-123, T-128
findings: G-13 (было T-103 в PR #167)
-->
- **Файлы:** `internal/compiler/compiler.go` (сбор `TypeDecl` — сейчас только записи; `compilePattern`); `internal/ast/accessors.go`, `internal/ast/decl.go` (варианты); `internal/runtime/value.go:266` (`Variant`), `:886` (`variantTagOrder`)
- **Тест-якорь:** создать `TestUserVariantConstructors`, `TestUserVariantPatterns`, `TestUserVariantTermOrder`
- **DoD:**
  - `type Color { Red, Green, Blue }` даёт три значения; `type Wrapper { Wrap(Int) }` даёт функцию `Wrap` арности 1; `map` по конструктору работает (в порядке T-130); `Wrap(1, 2)` → ловимый `(:function_clause, …)`;
  - мультиклозные `fn` и `match` по тегам работают; непокрытый тег → `:function_clause` / `:case_clause`;
  - затенение `Some`/`None`/`Ok`/`Error` — info-диагностика (§14.7);
  - `sort` значений пользовательских вариантов — по T-123;
  - `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** проверять аннотации типов (контракты — горизонт); generic-параметры сверх разбора; алиасы `type X = Y`; `type Color {}` (T-192).

### T-137 · Компиляция программы из нескольких модулей
<!-- meta
priority: P1
type: feature
effort: large
model: opus
wave: 9
depends_on: T-134, T-135, T-136
findings: G-10, G-15 (было T-106 в PR #167; research L19 — через T-122 п.3)
-->
- **Файлы:** `internal/compiler/compiler.go` (`Compile` — вход по графу модулей; `compileGlobalCall`; `compileMember`; `compilePipe:1557` — `M.f(a)` для пользовательских модулей); `ProgramImage.Functions` (квалифицированные имена); `cmd/brig/main.go` (снять fail-fast T-135)
- **Тест-якорь:** создать `TestMultiModuleCall`, `TestMultiModuleTypes`, `TestMultiModuleAlias`, `TestMultiModulePipe`; e2e-фикстура в `testdata/modules/`
- **DoD:**
  - `Util.f()` из `Main` вызывает `f` из `util.brig`; `alias Http.Client as Http` + `Http.get(...)` работает; `xs |> Util.f(a)` работает, ошибка для неизвестного модуля называет модуль, а не «obj.method» (G-10);
  - записи и варианты из `Util` конструируются и матчатся из `Main` по правилу T-122 п.3;
  - одноимённые `fn f` в двух модулях не перезаписывают друг друга;
  - хвостовой вызов в функцию другого модуля — `TAILCALL`; stack trace показывает `Util.f (util.brig:L:C)`;
  - fail-fast `срез: несколько модулей` удалён; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** приватность (T-143); ссылки на функции модуля как значения (T-144); горячая перезагрузка; менять соглашение о вызовах; менять REPL.

### T-138 · Лексер: offside-мини-блоки внутри скобок (§2.5, §D.6)
<!-- meta
priority: P1
type: feature
effort: large
model: opus
wave: 9
depends_on: T-124, T-127, T-132
findings: S-3 (было T-108 в PR #167; research L1 и #169 ссылаются на это как на «#2, A5.4»)
-->
- **Файлы:** `internal/lexer/lexer.go:81,125` (`blockDepth`, `processLine`); `internal/parser` — только если нужен другой поток токенов; `testdata/golden`, `testdata/positive`
- **Тест-якорь:** создать `TestLexMiniBlockLambdaInCall`, `TestLexMiniBlockMatchInList`, `TestLexMiniBlockFnInMap`, `TestRunMiniBlockExamples`
- **DoD:**
  - по правилу T-124 работают: `map(xs, fn (x) ->` + блок + `)`; `[match v` + клаузы + `]`; `%{ :get => fn (s, k) ->` + блок с `match` + `}` (пример §13.2);
  - после мини-блока внешний скобочный контекст восстановлен: следующий элемент после `,` разбирается;
  - корпус: файлы, которые ждали только T-138, переходят на уровень `parse`;
  - `go test ./internal/lexer -run '^$' -fuzz FuzzLex -fuzztime 60s` → PASS; `make test-roundtrip` → 0;
  - `make update-golden` — diff просмотрен; `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** править спеку (пример §13.2 — T-151); менять правило вне скобок сверх T-124; трогать `"""` (T-145).

### T-139 · `brig check`: неизвестные имена и арность
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 9
depends_on: T-137
findings: G-2, F-8
-->
- **Файлы:** `internal/sema` (новый проход разрешения имён по графу модулей) или `internal/compiler` (ошибка компиляции вместо `GETGLOBAL` неизвестного имени — выбор в body PR); `cmd/brig/main.go`; REPL — неизвестное имя остаётся ошибкой рантайма (строки видят глобалы предыдущих строк)
- **Тест-якорь:** создать `TestCheckUndefinedFunction`, `TestCheckUndefinedModuleFunction`, `TestCheckArityMismatch`, `TestReplUndefinedStillRuntime`
- **DoD:**
  - `fn main() -> nope(1)` → `brig check` и `brig run`: `error: file:1:14: undefined function nope/1`, exit 1;
  - `Json.nope(1)` и `Util.nope()` — то же с именем модуля; вызов известной функции с неверной арностью (для не-variadic без клозов нужной арности) — ошибка компиляции;
  - динамические вызовы (значение-функция в переменной) не проверяются;
  - корпус: файлы, которые ссылаются на несуществующие модули stdlib (`Whelk`, `Crypto`, …), на уровне `check` падают с понятной ошибкой — манифест корпуса отражает это `needs` (горизонт);
  - `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** проверять типы аргументов; менять REPL-семантику; запрещать вызов приватной функции (T-143).
