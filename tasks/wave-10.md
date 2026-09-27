# Wave 10 — язык для библиотек и stdlib на Brig

[← карта плана](README.md)

**Откуда:** research, фаза 1 «проектная база» (`web-mvp-research/08-roadmap.md`).

**Вход:**
- Wave 9 закрыта: модули, варианты, связывания, неизвестные имена;
- спека пакетов A и B внесена (T-128, T-129), T-100 (#169) закрыта.

**Зачем:** сделать то, без чего на Brig нельзя писать библиотеки:
- `pub fn`;
- ссылки на функции модулей;
- record update с сохранением вида;
- многострочные строки;
- лямбды.

И запустить второй контур обратной связи: stdlib, написанную на Brig.
После этой волны язык каждый раз проверяется на собственной библиотеке и
её доктестах.

**Выход:**
- `pending` из T-128 и T-129 сняты;
- `brig test` роняет CI при падении;
- модули `List`, `Option`, `Result` на Brig встроены в бинарник и
  покрыты доктестами;
- файлы корпуса `corpus/whelk/hook.brig` и `corpus/lookout/lib/lookout/*`
  доходят до уровня `check` везде, где им не нужны модули рантайма
  (Wave 12) или горизонта: метки `needs` это отражают.

## Порядок и параллельность

`internal/compiler/compiler.go` правят T-140, T-141, T-142, T-143, T-144 —
их мержат по одной. T-145 (лексер), T-146 (stdlib и загрузчик),
T-147 (`cmd/brig`, прелюдия `Test`) и T-148 (Go-native `Str`/`Bytes`)
идут параллельно с ними.

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-140 | Лямбды `fn ->` и `(a, b) ->` (по #169) | T-100 | opus | medium | T-145, T-147, T-148 |
| 2 | T-141 | Паттерны в параметрах полной лямбды | T-140 | opus | medium | T-145, T-147, T-148 |
| 3 | T-142 | Record update сохраняет вид; `Record.to_anon` | T-128 | sonnet | low | T-145, T-147, T-148 |
| 4 | T-143 | `pub fn`: приватность по умолчанию | T-139 | opus | medium | T-145, T-147, T-148 |
| 5 | T-144 | `Mod.f` как значение-функция | T-137 | opus | medium | T-145, T-147, T-148 |
| 6 | T-145 | Многострочные строки `"""` | T-129, T-132 | opus | medium | всё |
| 7 | T-146 | Встроенная stdlib на Brig: `List`, `Option`, `Result` | T-130, T-143 | opus | large | T-147, T-148 |
| 8 | T-147 | `brig test`: раннер, exit-код, доктесты `##` | T-117, T-125 | opus | medium | всё |
| 9 | T-148 | `Str` и `Bytes`: базовый набор (Go-native) | T-130 | sonnet | medium | всё |
| 10 | T-149 | Docs: тест-фреймворк и доктесты в спеке | T-125, T-147 | sonnet | low | — |

## Задачи

### T-140 · Лямбды `fn ->` и `(a, b) ->` (реализация #169)
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 10
depends_on: T-100
findings: — (research L22; #169 требует «создать задачу на реализацию» — это она)
-->
- **Файлы:** `internal/parser/expr.go` (`tryLambda`, `lambda_full` без скобок при нуле параметров, `lambda_short` со списком имён); `internal/ast/format.go` (`fn () ->` → `fn ->`); `internal/compiler` — только если нужен новый путь; `testdata/golden`
- **Тест-якорь:** создать `TestParseFnArrowNoParams`, `TestParseShortLambdaMultiParam`, `TestFormatFnEmptyParensCanon`, `TestRunShortLambdaMultiParam`
- **DoD:**
  - `fn ->` + блок и `(a, b) -> a + b` разбираются и исполняются; `(a, b)` без `->` — по-прежнему кортеж;
  - `Format` печатает `fn ->` для `fn () ->`; round-trip идемпотентен;
  - `fn x ->` без скобок — ошибка парсинга;
  - `pending` по #169 в спеке сняты; `make update-golden` — diff просмотрен; `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** мини-блоки (T-138); блочное тело у стрелки; паттерны в параметрах (T-141).

### T-141 · Паттерны в параметрах полной лямбды `fn ((a, b)) -> …`
<!-- meta
priority: P2
type: feature
effort: medium
model: opus
wave: 10
depends_on: T-140
findings: G-13 (было T-120 в PR #167; fail-fast из T-44)
-->
- **Файлы:** `internal/compiler/compiler.go` (fail-fast T-44 «параметр-паттерн в лямбде», `compileLambda`; образец — `compileOneClause`)
- **Тест-якорь:** переписать тест fail-fast T-44 в `TestLambdaPatternParams`; создать `TestLambdaPatternParamMismatch`
- **DoD:**
  - `fn ((a, b)) -> a + b` с `(1, 2)` → `3`; `fn ([h, ..t]) -> h`; `map(pairs, fn ((k, v)) -> v)` (порядок — T-130);
  - несовпадение → ловимый `(:function_clause, args)`; захват внешнего имени работает;
  - `rg -n 'параметр-паттерн .* в лямбде не реализован' internal/compiler` пуст; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** мультиклозные лямбды; guard в лямбде; менять `() ->`.

### T-142 · Record update сохраняет вид; `Record.to_anon`
<!-- meta
priority: P1
type: full-fix
effort: low
model: sonnet
wave: 10
depends_on: T-128
findings: G-3 (research L18)
-->
- **Файлы:** `internal/compiler/compiler.go` (`compileRecord`: вид по первому спреду, если у литерала нет имени типа); `internal/vm` (`RECORD` — если вид определяется в рантайме); прелюдия `Record.to_anon`; skill `brig-compiler:132` (одна строка)
- **Тест-якорь:** создать `TestRecordUpdateKeepsKind`, `TestRecordUpdateUnknownFieldOnNominal`, `TestRecordToAnon`
- **DoD:**
  - `u = User{ id: 1, name: "a" }`; `{ ..u, name: "b" } == User{ id: 1, name: "b" }` → `true` (проба g09);
  - добавление поля, которого нет в `User`, через update — ловимая ошибка по T-128;
  - `Record.to_anon(u)` → анонимная запись; `{ ..anon, x: 1 }` остаётся анонимной;
  - `make update-bytecode` — diff просмотрен; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** менять равенство записей; менять `User{ ..r }` (явная форма уже задаёт вид).

### T-143 · `pub fn`: приватность по умолчанию
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 10
depends_on: T-139
findings: P-3 (research Q-vis; миграция — по T-122 п.1)
-->
- **Файлы:** `internal/parser/stmt.go` (`pub` перед `fn`); `internal/ast` (флаг видимости, `Format`); проверка вызова приватной функции из другого модуля (проход T-139); все `.brig` в `testdata`, `examples`, `corpus/lang` и примеры спеки (миграция по T-122 п.1); `brig.ebnf` — уже в T-128
- **Тест-якорь:** создать `TestPubFnCallableFromOtherModule`, `TestPrivateFnCallFromOtherModuleIsError`, `TestMainIsEntryWithoutPub`, `TestFormatPubFn`
- **DoD:**
  - `pub fn f` вызывается из другого модуля; вызов не-`pub` функции другого модуля — `error: file:line:col: f/1 is private to Util`, exit 1;
  - `main` и функции тестов вызываются тулчейном без `pub`;
  - внутри модуля приватные функции видны как раньше;
  - корпус: `pub fn` больше не блокирует ни один файл (метки `needs: T-143` сняты); `make corpus` → 0;
  - `make update-golden` — diff просмотрен; `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** приватные типы и поля; `brig fix` (если T-122 п.1 = A — миграция руками в этом PR); контракты на `pub fn` (горизонт).

### T-144 · Ссылка на функцию модуля как значение (`Mod.f`)
<!-- meta
priority: P2
type: feature
effort: medium
model: opus
wave: 10
depends_on: T-137
findings: G-11 (по T-122 п.4; research 17, §13.1 MFA)
-->
- **Файлы:** `internal/compiler/compiler.go` (`compileMember`: `UPPER.lower` без вызова); `internal/runtime` (identity значения-функции модуля — полное имя и арность); проверка T-139 (неизвестная функция модуля)
- **Тест-якорь:** создать `TestModuleFunctionAsValue`, `TestModuleFunctionIdentity`, `TestPreludeModuleFunctionAsValue`
- **DoD:**
  - `f = Json.encode`; `f([1])` → `"[1]"`; `map(xs, Util.g)` работает;
  - `Util.g == Util.g` → `true` при двух разных ссылках (identity по имени, research 11 «identity не адрес»);
  - неоднозначность «функция vs модуль-значение» (L5) не вводится: `Util` без `.f` в выражении — ошибка компиляции, как сейчас;
  - `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** модули и типы как значения (L5 целиком — горизонт); `Module.apply`; сериализацию функций (`Term` — горизонт).

### T-145 · Многострочные строки `"""` (§3.5 по T-129)
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 10
depends_on: T-129, T-132
findings: P-3 (research L9; заменяет fail-fast T-115 из PR #167)
-->
- **Файлы:** `internal/lexer/lexer.go` (чтение `STRING`), `internal/lexer/escape.go`; `internal/parser` — интерполяция внутри многострочной строки; `internal/ast/format.go` (печать)
- **Тест-якорь:** создать `TestLexTripleQuoteIndentStrip`, `TestLexTripleQuoteLessIndentError`, `TestLexTripleQuoteInterp`, `TestFormatTripleQuoteRoundTrip`
- **DoD:**
  - отступ строки с закрывающими `"""` снимается со всех строк; строка с меньшим отступом — ошибка лексера с `line:col`; перевод строки после открывающих и перед закрывающими `"""` в значение не входит;
  - интерполяция `\(expr)` и escape работают как в `Str`;
  - корпус: `needs: T-145` сняты (миграции, SQL в `monitors.brig`); `pending(T-145)` в спеке сняты;
  - `go test ./internal/lexer -run '^$' -fuzz FuzzLex -fuzztime 60s` → PASS; `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** многострочные `b"..."`/`rx"..."`; raw-строки; сигилы.

### T-146 · Встроенная stdlib на Brig: `List`, `Option`, `Result`
<!-- meta
priority: P1
type: feature
effort: large
model: opus
wave: 10
depends_on: T-130, T-143
findings: F-3, G-10 (research L2 «тот же механизм грузит встроенный stdlib из go:embed», 04 «Result/Option — Brig»)
-->
Второй контур обратной связи: язык каждый раз проверяется на
собственной библиотеке. Модули пишутся на Brig, встраиваются в бинарник
и покрываются доктестами.

- **Файлы:** `stdlib/list.brig`, `stdlib/option.brig`, `stdlib/result.brig` (создать); пакет встраивания (`go:embed`) и регистрация в загрузчике T-135 как модулей с фиксированными именами (§11.3 «builtins»); `internal/compiler:1702` (`isPreludeModule` — отличать Go-native и Brig-модули); `Makefile` (`stdlib` в `all`, если сборка отдельная)
- **Тест-якорь:** доктесты `##` в модулях (раннер — T-147; до него — Go-тест, который гоняет `Test.*` внутри модулей); создать `TestStdlibEmbeddedLoads`, `TestStdlibNoFilesystem` (работает без исходников рядом)
- **DoD:**
  - `List`: `each`, `map`, `filter`, `fold`, `reverse`, `take`, `drop`, `sort`, `member?`, `concat`; `Option`: `map`, `and_then`, `unwrap`, `unwrap_or`; `Result`: `map`, `and_then`, `unwrap`, `unwrap_or`, `all`. Каждая `pub fn` — с `##` и хотя бы одним доктестом;
  - порядок аргументов — по T-120; `args |> List.each(log)` (§6.3) исполняется;
  - `brig` без исходников stdlib рядом работает (встроено); размер бинарника — в body PR;
  - корпус: `needs` на `List`/`Option`/`Result` сняты; `make corpus` → 0;
  - `make all` → 0; `make check-examples` → `failed 0`.
- **НЕ делать:** переносить в Brig существующие Go-нативы (`map` прелюдии и т.д.); кэш байткода на диске; `Str`/`Bytes` (T-148 — Go-native); `Supervisor`, `Server` (Wave 13).

### T-147 · `brig test`: раннер, exit-код, доктесты `##`
<!-- meta
priority: P1
type: feature
effort: medium
model: opus
wave: 10
depends_on: T-117, T-125
findings: G-9, F-4 (по решению T-125; доктесты — research 22)
-->
- **Файлы:** `cmd/brig/main.go` (подкоманда `test`); `internal/vm/prelude_test_fw.go` (`Test.run` возвращает итог); разбор `##` и REPL-блоков — переиспользовать `internal/examples` (T-117); `internal/parser` (сохранение `##` перед `pub fn`/`type`/`module`, если нужно для доктестов)
- **Тест-якорь:** создать `TestBrigTestExitCodeOnFailure`, `TestBrigTestDiscoversFiles`, `TestDoctestPasses`, `TestDoctestMismatch`
- **DoD:**
  - `brig test [path]` находит тесты по соглашению из T-125, печатает сводку, exit 0 только если всё прошло; упавший тест — exit 1 (проба h14);
  - доктесты `##` исполняются тем же раннером; сравнение через `==`;
  - `make stdlib-test` (или `brig test stdlib/`) входит в `make all` (после T-146);
  - `make all` → 0.
- **НЕ делать:** `Test.isolated` и экземпляры рантайма (горизонт, research 16); параллельный запуск; `brig doc`.

### T-148 · `Str` и `Bytes`: базовый набор (Go-native)
<!-- meta
priority: P2
type: feature
effort: medium
model: sonnet
wave: 10
depends_on: T-130
findings: — (research L8; корпус: `needs` на `Str.*`)
-->
- **Файлы:** `internal/vm/prelude.go` (или новый `prelude_str.go`); таблица прелюдии §11.5 — строки для новых функций (docs-правка в этом PR допустима только в таблице; иначе — отдельный docs-коммит по согласованию в issue)
- **Тест-якорь:** создать `TestStrSplitJoin`, `TestStrTrimFind`, `TestStrSliceCodepoints`, `TestBytesSliceFind`
- **DoD:**
  - `Str`: `split`, `join`, `trim`, `find`, `replace`, `starts_with?`, `ends_with?`, `lower`, `upper`, `slice` (по кодпоинтам), `to_int` (→ `Option`); `Bytes`: `slice`, `find`, `split`, `concat`, `at`;
  - ошибки — ловимые `:type_error` / `:index_out_of_bounds` по §10.4; порядок аргументов — субъект первым (T-120);
  - тест T-118 знает новые имена; `make all` → 0.
- **НЕ делать:** кодеки (`Base64`, `Url`, `Hex` — горизонт), `Regex`, Unicode-нормализацию.

### T-149 · Docs: тест-фреймворк и доктесты в спеке
<!-- meta
priority: P2
type: docs
effort: low
model: sonnet
wave: 10
depends_on: T-125, T-147
findings: — (было T-116 в PR #167)
-->
- **Файлы:** `docs/01-language-design.md` §11.5 (`Test.*`), подраздел «Doc-комментарии `##`» (из T-129) — раннер и формат доктестов, §16
- **Тест-якорь:** `make check-examples`
- **DoD:**
  - API `Test.*` и `brig test` описаны ровно в объёме T-125 и T-147: функции, арность, соглашение об обнаружении, exit-код;
  - `pending(T-147)` сняты; примеры — блоки `brig`, их проверяет `check-examples`;
  - запись в Part III; `make check-examples` → `failed 0`.
- **НЕ делать:** менять код `Test.*`; описывать `Test.isolated` и горячую перезагрузку.
