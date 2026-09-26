# Wave 7 — остаток Must §16 и нормативного синтаксиса

[← карта плана](README.md)

**Вход:** Wave 6 закрыта. **Откуда задачи:** пробы на `origin/main` @ `8cd9dbc` (2026-09-26) — маленькие программы по примерам спеки, которые Wave 6 не покрыла. **Выход:** примеры спеки §4.1, §11.1, §14.2, §2.5 исполняются; `rg -n 'срез: только простые связывания' internal/compiler` пуст; `make check-examples` → `failed 0`; `make all` и `BRIG_VERIFY=1 go test ./...` → 0.

**Параллельность.** T-101, T-103, T-104 и T-106 правят `internal/compiler/compiler.go`, поэтому по `MAINTAINING.md` §5 их берут по одной. T-105 (загрузчик в `cmd/brig`) и T-108 (лексер) можно вести параллельно с ними. DD (T-100, T-102, T-107, T-109) — работа человека, их лучше решить до старта волны.

| T-NN | Название | depends_on | Тип | Источник |
|---|---|---|---|---|
| T-100 | DD: ошибка несовпадения паттерна в связывании `pat = expr` | — | design-decision | spec-gap |
| T-101 | Связывание с паттерном: `(a, b) = t`, `[h, ..t] = xs` (§4.1, §5.1) | T-100 | feature | spec-gap |
| T-102 | DD: term order пользовательских вариантов (§7.4) | — | design-decision | spec-gap |
| T-103 | Пользовательские варианты: конструкторы, паттерны, term order (§14.1–14.2) | T-102 | feature | spec-gap |
| T-104 | `alias` и `import` встроенных модулей (§11.1) | — | feature | spec-gap |
| T-105 | Загрузка модулей из файлов: путь → имя модуля (§11.1) | T-104 | feature | spec-gap |
| T-106 | Компиляция программы из нескольких модулей | T-103, T-105 | feature | spec-gap |
| T-107 | DD: якорь offside-блока (§2.4 и §D.6 противоречат примерам) | — | design-decision | spec-gap |
| T-108 | Лексер: offside-мини-блоки внутри скобок (§2.5, §D.6) | T-107 | feature | spec-gap |
| T-109 | DD: что считать Must «тест-фреймворк» и «заделы под горячую перезагрузку» | — | design-decision | spec-gap |

## Задачи

### T-100 · DD: ошибка несовпадения паттерна в связывании `pat = expr`
<!-- meta
priority: P1
type: design-decision
effort: S
model: human
wave: 7-must
depends_on: —
findings: — (проба роадмапа: `(a, b) = (1, 2)` → `срез: только простые связывания`)
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §4.1 (`(a, b, c) = t`), §5.1, §10.4 (список авто-raise)
- **Тест-якорь:** — (решение)
- **Вопрос:** что происходит, если значение не подошло под паттерн слева от `=` (`(a, b) = (1, 2, 3)`, `[h, ..t] = []`)? В списке авто-raise §10.4 такого случая нет.
- **Варианты:** **A:** авто-raise `(:case_clause, val)` — переиспользовать имя из `match`. **B:** новое имя `(:badmatch, val)`, добавить его в §10.4. **C:** слева разрешены только неопровержимые паттерны (кортеж фиксированной длины из имён, `_`); опровержимые (`[h, ..t]`, литералы) — ошибка компиляции.
- **DoD:** в issue записан вариант и судьба T-101 (делается как есть / делается с поправкой DoD). Issue закрыт.
- **НЕ делать:** писать код; менять `with`-binds (у них своя семантика, §8.2); решать заодно паттерны в параметрах лямбды (T-120).

### T-101 · Связывание с паттерном: `(a, b) = t`, `[h, ..t] = xs` (§4.1, §5.1)
<!-- meta
priority: P1
type: feature
effort: M
model: opus
wave: 7-must
depends_on: T-100
findings: — (проба роадмапа)
-->
- **Файлы:** `internal/compiler/compiler.go:1008` (`compileLetBind`), `:2266` (`compileTrapLetBind`), `:2669` (`compilePattern`); `internal/sema/sema.go:233` (`checkPatternBinding` — проверить, что имена из паттерна попадают в область)
- **Тест-якорь:** создать `TestLetBindTuplePattern`, `TestLetBindListSpread`, `TestLetBindMismatch`, `TestTrapLetBindPattern` в `internal/compiler`
- **DoD:**
  - `(a, b, c) = (1, "a", :ok)` связывает три имени; `[h, ..t] = [1, 2, 3]` даёт `h == 1`, `t == [2, 3]`; `{name: n} = User{name: "Ada", id: 1}` связывает `n`;
  - несовпадение ведёт себя по решению T-100, ошибка ловится `trap`;
  - то же внутри блока `trap` (путь `compileTrapLetBind`) — `TestTrapLetBindPattern`;
  - повторное имя внутри одного паттерна (`(a, a) = t`) — ошибка sema с `line:col`;
  - `rg -n 'срез: только простые связывания' internal/compiler` пуст;
  - `make update-bytecode` — прогнать, diff просмотреть глазами; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** менять парсер паттернов; трогать `with`-binds и параметры лямбд (T-120); вводить спред в Map/record-паттернах (§5.1: запрещён в MVP); менять набор опкодов без необходимости (если нужен — отдельный коммит с обоснованием в PR).

### T-102 · DD: term order пользовательских вариантов (§7.4)
<!-- meta
priority: P2
type: design-decision
effort: S
model: human
wave: 7-must
depends_on: —
findings: — (§7.4 п.13 упорядочивает только встроенные `Ok`/`Error`/`Some`/`None`; `runtime.variantTagOrder` в `internal/runtime/value.go:886` знает только их)
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §7.4, §14.1; `internal/runtime/value.go:886`
- **Тест-якорь:** — (решение)
- **Вопрос:** где в term order стоят значения пользовательских вариантов (`type Color { Red, Green }`) и как они сравниваются между собой?
- **Варианты:** **A:** как номинальные записи (п.12): по имени типа, затем по порядку объявления тега, затем по полям. **B:** отдельной ступенью после встроенных вариантов (п.13): по имени типа, затем по имени тега лексикографически, затем по полям. **C:** по порядку объявления тега, без имени типа (как `deriving Ord` в Haskell), разные типы — по имени типа.
- **DoD:** в issue записан вариант; указано, куда встаёт ступень в списке §7.4. Issue закрыт.
- **НЕ делать:** писать код; менять порядок встроенных вариантов; решать `type Color {}` (T-132).

### T-103 · Пользовательские варианты: конструкторы, паттерны, term order (§14.1–14.2)
<!-- meta
priority: P1
type: feature
effort: M
model: opus
wave: 7-must
depends_on: T-102
findings: — (проба роадмапа: `type Shape { Circle(Float), Sq(Float) }` парсится, `Sq(2.0)` → `undefined: Sq`)
-->
- **Файлы:** `internal/compiler/compiler.go:478-485` (сбор `TypeDecl` — сейчас только записи), `:2669` (`compilePattern`); `internal/ast/accessors.go:355` (`TypeDecl`: добавить аксессор вариантов), `internal/ast/decl.go:31` (`typeDecl.variants`); `internal/runtime/value.go:266` (`Variant`), `:886` (`variantTagOrder`)
- **Тест-якорь:** создать `TestUserVariantConstructors`, `TestUserVariantPatterns`, `TestUserVariantTermOrder`
- **DoD:**
  - `type Color { Red, Green, Blue }` даёт три значения; `Red == Red`, `Red != Green`;
  - `type Wrapper { Wrap(Int) }` даёт функцию `Wrap` арности 1; `map(Wrap, [1, 2])` → `[Wrap(1), Wrap(2)]`; `Wrap(1, 2)` → ловимый `(:function_clause, …)`;
  - мультиклозная `fn area(Circle(r)) -> …` / `fn area(Sq(a)) -> …` и `match` по тегам работают; непокрытый тег → `(:function_clause, …)` / `(:case_clause, …)`;
  - затенение `Some`/`None`/`Ok`/`Error` пользовательским вариантом даёт info-диагностику (§14.7);
  - `sort` смешанного списка значений пользовательских вариантов даёт порядок по решению T-102;
  - `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** проверять аргументы по аннотациям типов (runtime-контракты — Should); реализовывать generic-параметры сверх разбора; трогать алиасы `type X = Y`; решать `type Color {}` (T-132).

### T-104 · `alias` и `import` встроенных модулей (§11.1)
<!-- meta
priority: P2
type: feature
effort: S
model: sonnet
wave: 7-must
depends_on: —
findings: — (проба роадмапа: `alias Json as J` → `J.encode(...)` падает с `undefined: J`; `importDecl`/`aliasDecl` компилятор не читает)
-->
- **Файлы:** `internal/compiler/compiler.go:1702` (`isPreludeModule`), `:1852` (`compileMember`), места вызова `Module.fn`; `internal/ast/decl.go:9,20` (`importDecl`, `aliasDecl`: экспортировать аксессоры, если их нет); `internal/sema` (неизвестный модуль)
- **Тест-якорь:** создать `TestAliasBuiltinModule`, `TestImportUnknownModule`
- **DoD:**
  - `alias Json as J` + `J.encode([1])` → `"[1]"`; `alias` на неизвестный модуль → ошибка `error: file:line:col: …` и exit 1;
  - `import X`, где `X` — не встроенный модуль и не файл, → та же ошибка (загрузку файлов делает T-105; до неё это честная ошибка, а не `undefined` в рантайме);
  - доступ к встроенным модулям без `import` ведёт себя как до задачи (изменение — только по отдельному issue, если спека этого требует);
  - `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** загружать файлы (T-105); менять набор функций встроенных модулей; менять `Prelude.*` (T-75); трогать `cmd/brig`.

### T-105 · Загрузка модулей из файлов: путь → имя модуля (§11.1)
<!-- meta
priority: P1
type: feature
effort: M
model: opus
wave: 7-must
depends_on: T-104
findings: — (проба роадмапа: `import Util` при `util.brig` рядом → `undefined: Util`)
-->
- **Файлы:** `cmd/brig/main.go` (`run`, `check`); новый пакет `internal/loader` (граф модулей); `internal/parser` (только вызов, без правки грамматики)
- **Тест-якорь:** создать `internal/loader/loader_test.go`: `TestLoadResolvesPathToModule`, `TestLoadExplicitModuleOverridesPath`, `TestLoadImportCycle`, `TestLoadMissingModule`; фикстуры в `testdata/modules/`
- **DoD:**
  - корень — каталог входного файла; `import Util` находит `util.brig`, `import Http.Client` находит `http/client.brig`; явный `module X` в файле переопределяет имя, а расхождение с путём при импорте даёт ошибку;
  - цикл импортов → `error: file:line:col: import cycle: A -> B -> A`, exit 1; отсутствующий модуль → `error: file:line:col: module Util not found`, exit 1;
  - `brig check main.brig` парсит и прогоняет sema по всем модулям графа;
  - пока нет T-106, `brig run` для графа из ≥2 модулей завершается явной ошибкой `срез: несколько модулей` (exit 1), а не молча;
  - `make all` → 0.
- **НЕ делать:** компилировать вызовы между модулями (T-106); вводить пакетный менеджер или пути поиска (§17 п.5); менять грамматику `import`/`alias`; кэшировать байткод на диск.

### T-106 · Компиляция программы из нескольких модулей
<!-- meta
priority: P1
type: feature
effort: L
model: opus
wave: 7-must
depends_on: T-103, T-105
findings: —
-->
- **Файлы:** `internal/compiler/compiler.go:463` (`Compile` → вход по графу модулей), `:1668` (`compileGlobalCall`), `:1852` (`compileMember`); `ProgramImage.Functions` (квалифицированные имена `Util.f`); `cmd/brig/main.go` (снять fail-fast из T-105)
- **Тест-якорь:** создать `TestMultiModuleCall`, `TestMultiModuleTypes`, `TestMultiModuleAlias`; e2e-фикстура в `testdata/modules/`
- **DoD:**
  - `Util.f()` из `Main` вызывает `f` из `util.brig`; `alias Http.Client as Http` + `Http.get(...)` работает;
  - записи и варианты, объявленные в `Util`, конструируются и матчатся из `Main` (`Util.User{...}` или по правилу спеки — если спека молчит, стоп и комментарий в issue);
  - одноимённые `fn f` в двух модулях не перезаписывают друг друга в `image.Functions`;
  - хвостовой вызов в функцию другого модуля — `TAILCALL`; стек-трейс (T-79) показывает `Util.f (util.brig:L:C)`;
  - fail-fast `срез: несколько модулей` удалён; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** вводить приватность или экспорт (спека не определяет — отдельный DD, если понадобится); горячую перезагрузку; менять соглашение о вызовах; менять REPL.

### T-107 · DD: якорь offside-блока (§2.4 и §D.6 противоречат примерам)
<!-- meta
priority: P1
type: design-decision
effort: S
model: human
wave: 7-must
depends_on: —
findings: — (§2.4: правило «base_indent = колонка открывателя, тело строго глубже», а пример там же: `trap` в колонке 14, тело на отступе 8; пример §13.2: `fn` в колонке 21, тело `match` в колонке 17)
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §2.4, §2.5, §D.6, §D.7, §13.2; `internal/lexer/lexer.go:81,125` (`blockDepth`, A5.4)
- **Тест-якорь:** — (решение); пробы: `xs = map(fn (x) ->` с телом на следующих строках → `expected INDENT` при любом отступе тела
- **Вопрос:** от чего отсчитывается отступ тела блока — от колонки токена-открывателя или от отступа физической строки, в которой он стоит? Вне скобок реализация уже использует отступ строки (`r = trap` + тело на +4 работает).
- **Варианты:** **A:** отступ физической строки открывателя — везде, включая скобки; правится текст §2.4/§D.6, примеры остаются. **B:** колонка открывателя — везде; правятся примеры §2.4, §13.2 и реализация вне скобок. **C:** вне скобок — отступ строки, внутри скобок — колонка открывателя.
- **DoD:** в issue записан вариант и список разделов спеки, которые правит docs-задача. Issue закрыт.
- **НЕ делать:** писать код; менять правило для `ensure` (v0.4.7: `trap_item`); решать заодно NEWLINE-разделители (§D.5).

### T-108 · Лексер: offside-мини-блоки внутри скобок (§2.5, §D.6)
<!-- meta
priority: P1
type: feature
effort: L
model: opus
wave: 7-must
depends_on: T-107
findings: — (проба роадмапа; audit §9 «не покрыто»: мини-блоки §D.6/D.7)
-->
- **Файлы:** `internal/lexer/lexer.go:81,125` (`blockDepth`, `processLine`); `internal/parser` (только если парсер ждёт другой поток токенов); `testdata/golden`, `testdata/positive`
- **Тест-якорь:** создать `TestLexMiniBlockLambdaInCall`, `TestLexMiniBlockMatchInList`, `TestLexMiniBlockFnInMap` (lexer) и `TestRunMiniBlockExamples` (compiler, e2e)
- **DoD:**
  - по правилу из T-107 работают: `map(fn (x) ->` + блок + `, xs)`; `[match v` + клаузы + `]`; `%{ :get => fn (s, k) ->` + блок с `match` + `}` (пример §13.2);
  - после мини-блока внешний скобочный контекст восстановлен: следующий элемент после `,` разбирается;
  - `go test ./internal/lexer -run '^$' -fuzz FuzzLex -fuzztime 60s` → PASS; `make test-roundtrip` → 0;
  - `make update-golden` — прогнать, diff просмотреть глазами; `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** править спеку (docs-задача после T-107; снять TODO в §13.2 — T-117); менять правило для блоков вне скобок, если T-107 его не меняет; трогать `"""` (T-115); рефакторить офсайд-алгоритм сверх нужного.

### T-109 · DD: что считать Must «тест-фреймворк» и «заделы под горячую перезагрузку»
<!-- meta
priority: P2
type: design-decision
effort: S
model: human
wave: 7-must
depends_on: —
findings: — (§16 Must называет обе фичи, но в спеке нет ни API `Test.*`, ни определения «заделов»; в коде есть `internal/vm/prelude_test_fw.go`: `Test.describe/it/run/assert/assert_eq/assert_ne/fail`)
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §16, §11.5; `internal/vm/prelude_test_fw.go`; `cmd/brig/main.go`
- **Тест-якорь:** — (решение)
- **Вопросы и варианты:**
  1. Тест-фреймворк. **A:** закрепить текущий API `Test.*` как есть и описать в спеке (T-116). **B:** изменить API, например ввести `brig test <file>` и находить тесты по соглашению об именах. **C:** перенести в Should, Must закрыть текущей реализацией.
  2. «Заделы под горячую перезагрузку». **A:** считать выполненным: имена функций в образе квалифицированы (T-106), вызовы идут через таблицу глобалов. **B:** явный список гарантий (например, «вызов по полному имени `Module.f` всегда идёт в текущую версию»), который фиксируется в doc 02. **C:** убрать из Must, горячая перезагрузка целиком — Nice.
- **DoD:** в issue записан вариант по каждому пункту и что делает T-116. Issue закрыт.
- **НЕ делать:** писать код; менять `Test.*`; решать §17 п.4 (версии записей).
