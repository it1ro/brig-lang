# Wave 17 — язык: трение библиотек и нормы (третий аудит)

[← карта плана](README.md)

**Откуда:** третий аудит ([AUDIT_REPORT-3.md](../AUDIT_REPORT-3.md)),
журнал трения dogfooding (раздел 6), findings G-20, G-22, G-23, G-24,
D-2; DD T-253…T-259 ([decisions.md](decisions.md)).

**Вход:**
- Wave 16 закрыта: `brig test` работает в проектах (T-244, T-245),
  корпус диагностик (T-248) и корпус с заглушками (T-249) проверяют
  каждую задачу;
- задачи с DD в «Ждёт» — после решения DD; при варианте «ничего» задача
  закрывается как won't-fix по своему DoD.

**Зачем:** программы dogfooding работают, но пишутся с обходами:
вложенные `if` вместо цепочки, `0 - x` вместо `-x`, свои `unwrap` и
`zip`, сортировка списков по длине, ошибки JSON строкой. Молча неверное
поведение языка здесь одно — локальная привязка не затеняет акторный
примитив (T-261).

**Выход:**
- все `# pending: T-265` корпуса диагностик сняты;
- `else if`, порядок списков, продолжение строки, JSON и дубли §0.2 —
  по решениям DD;
- спека: §0.11 и структура — по T-258, T-259;
- `internal/compiler/compiler.go` разбит на файлы по подсистемам.

## Порядок и параллельность

`internal/compiler/compiler.go` правят T-261 и T-269 — по одной, T-269
последней в волне (перенос кода конфликтует со всеми правками
компилятора). Спеку правят T-262…T-268 — мержить по одной в порядке
таблицы.

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-260 | Печатная форма `Float` сохраняет вид | — | sonnet | low | всё |
| 2 | T-261 | Локальная привязка затеняет акторный примитив | — | opus | low | всё, кроме T-269 |
| 3 | T-262 | Продолжение строки по `+`/`-` — по T-255 | T-255 | sonnet | low | T-260, T-261 |
| 4 | T-263 | `else if` — по T-253 | T-253 | sonnet | low | T-260, T-261 |
| 5 | T-264 | Term order `List`/`Vector` — по T-254 | T-254 | sonnet | low | всё |
| 6 | T-265 | Сообщения об ошибках из корпуса диагностик | T-248, T-262 | opus | medium | T-264, T-266 |
| 7 | T-266 | JSON: маппинг и структурные ошибки — по T-256 | T-256 | sonnet | medium | T-264, T-265 |
| 8 | T-267 | §0.2: дубли прелюдии и stdlib — по T-257 | T-257 | sonnet | low | T-264, T-266 |
| 9 | T-268 | Docs: §0.11, структура и версия спеки — по T-258, T-259 | T-258, T-259, T-240 | sonnet | medium | — |
| 10 | T-269 | Разбить `compiler.go` на файлы (без изменения поведения) | T-261 | opus | medium | — |

## Задачи

### T-260 · Печатная форма `Float` сохраняет вид
<!-- meta
priority: P2
type: full-fix
effort: low
model: sonnet
wave: 17
depends_on: —
findings: G-23 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/runtime/value.go` (`Inspect`, `KindFloat`), `docs/01-language-design.md` §11.5 (строка `to_str`), `examples/*.out`, `testdata/`
- **Тест-якорь:** создать `TestInspectFloatKeepsKind` в `internal/runtime/inspect_test.go`.
- **DoD:**
  - `to_str(1.0) == "1.0"`, `print([1.0, 2.5, 1e20])` → `[1.0, 2.5, 1.0e20]`; кратчайшее представление, которое разбирается обратно в тот же `Float` и всегда содержит `.` или `e`; `NaN`, `Inf`, `-Inf` — как сейчас;
  - строка `to_str` §11.5: «печатная форма разбирается обратно в значение того же вида»;
  - `make update-examples` — diff `.out` просмотрен и отдельным коммитом;
  - `make all` → 0.
- **НЕ делать:** менять `Json.encode`; менять печать `Int` и `Decimal`; менять сравнение в доктестах.

### T-261 · Локальная привязка затеняет акторный примитив
<!-- meta
priority: P1
type: full-fix
effort: low
model: opus
wave: 17
depends_on: —
findings: G-20 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/compiler/compiler.go` (`compileCall` ~стр. 1984, `compileActorCall`), `internal/compiler/*_test.go`, `.claude/skills/brig-compiler/SKILL.md` (строка «затенение локальной переменной компилятор пока не учитывает»)
- **Тест-якорь:** создать `TestLocalBindingShadowsActorPrimitive` в `internal/compiler`: `send = (a, b) -> (:mine, a, b)` и `send(self(), 2)`; `spawn = x -> x + 1` и `spawn(1)`; `reply = (x) -> x * 2` и `reply(21)`.
- **DoD:**
  - вызовы из теста-якоря возвращают `(:mine, #<pid …>, 2)`, `2`, `42`; ящик вызывающего пуст;
  - параметр функции и имя из паттерна с именем примитива затеняют его так же;
  - `Prelude.send(…)` — по-прежнему примитив;
  - info «shadows prelude binding» не меняется;
  - `make all` → 0; `BRIG_VERIFY=1 go test ./...` → 0; `make update-bytecode` — если diff есть, отдельным коммитом.
- **НЕ делать:** менять pipe-запрет §7.5 (он по имени); менять sema; трогать другие места `compiler.go`.

### T-262 · Продолжение строки по `+`/`-` — по решению T-255
<!-- meta
priority: P2
type: full-fix
effort: low
model: sonnet
wave: 17
depends_on: T-255
findings: G-22 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/lexer/token.go` (`continuationOps`), `internal/ast/format.go`, `docs/01-language-design.md` §2.2, §D.4, `internal/ast/testdata/fuzz/FuzzRoundTrip/` (seed)
- **Тест-якорь:** seed `(fn->-0)` в корпус `FuzzRoundTrip`; создать `TestUnaryMinusStartsStatement` в `internal/lexer`.
- **DoD:**
  - вариант A T-255: `-`/`+` не продолжают строку; `fn f(x) ->⏎    y = x⏎    -y` возвращает `-x`; вариант B: форматтер заключает тело с ведущим унарным минусом в скобки, ошибка лексера подсказывает скобки;
  - seed `(fn->-0)` проходит `go test ./internal/ast -run FuzzRoundTrip`;
  - `rg -n '^\s+[-+] ' corpus stdlib examples docs/01-language-design.md` — совпадения переписаны (вариант A);
  - `make fuzz` → 0; `make all` → 0.
- **НЕ делать:** менять остальные операторы списка продолжений; вводить значимый пробел; решать за T-255.

### T-263 · `else if` — по решению T-253
<!-- meta
priority: P2
type: feature
effort: low
model: sonnet
wave: 17
depends_on: T-253
findings: журнал трения #1, #2 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/parser` (разбор `if`), `internal/ast/format.go`, `brig.ebnf` (`else_if_clause`), `docs/01-language-design.md` §8.1, `testdata/golden`
- **Тест-якорь:** создать `TestElseIfChain` в `internal/parser` (AST = вложенный `if`) и round-trip в `internal/ast`.
- **DoD:**
  - вариант A T-253: `if a⏎    1⏎else if b⏎    2⏎else⏎    3` разбирается во вложенный `if` без правки компилятора; цепочка любой длины на одном отступе; форматтер печатает цепочку как `else if`;
  - §8.1 и `brig.ebnf` описывают форму; пример — блок `brig` в §8.1, проходит `check-examples`;
  - `make update-golden` — отдельным коммитом; `make all` → 0.
- **НЕ делать:** guard'ы в `match`; менять однострочный `if … then … else`; трогать компилятор.

### T-264 · Term order `List`/`Vector` — по решению T-254
<!-- meta
priority: P2
type: full-fix
effort: low
model: sonnet
wave: 17
depends_on: T-254
findings: журнал трения #16 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/runtime` (`Compare` для `KindList`, `KindVector`), `internal/runtime/compare_test.go`, `docs/01-language-design.md` §7.4 п.8–9
- **Тест-якорь:** `TestCompareListLexicographic` в `internal/runtime/compare_test.go`.
- **DoD:**
  - вариант A T-254: `[1, 1] < [2]`, `[1] < [1, 0]`, `%[1, 1] < %[2]`; `List.sort([[1, 1], [2], [0, 0, 0]])` → `[[0, 0, 0], [1, 1], [2]]`;
  - кортежи, `Map`, `Set` — без изменений (их тесты зелёные);
  - §7.4 п.8–9 переписаны;
  - `make all` → 0.
- **НЕ делать:** менять порядок ступеней term order; менять `==`; трогать кортежи.

### T-265 · Сообщения об ошибках из корпуса диагностик
<!-- meta
priority: P2
type: full-fix
effort: medium
model: opus
wave: 17
depends_on: T-248, T-262
findings: D-2 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/parser`, `internal/lexer`, `internal/vm/scheduler.go` (`deadlock`, raise из натива), `cmd/brig`, `testdata/diagnostics/*.brig`
- **Тест-якорь:** `TestDiagnosticsCorpus` (T-248) — записи `# pending: T-265`.
- **DoD:**
  - висящий бинарный оператор в конце строки: позиция оператора и подсказка «продолжение строки — ведущим оператором (§2.2)»;
  - блочное тело короткой лямбды: «тело короткой лямбды — одна строка; для блока — `fn (x) ->` (§6.2)»;
  - `when` в ветке `match`: «guard в `match` не разрешён (§8.3); используйте `if` в теле или `fn` с guard»;
  - `deadlock`: имя функции и позиция `recv`, на которой стоит main;
  - raise из натива, вызванного с неверным аргументом (`fold(xs, f, 0)`), — позиция вызова и trace;
  - строки ошибок e10, e26 — строка исходника, где ошибка, а не следующая;
  - в `testdata/diagnostics` не осталось `# pending: T-265`; `make all` → 0.
- **НЕ делать:** менять формат §E; менять значения авто-raise; переводить сообщения на другой язык.

### T-266 · JSON: маппинг и структурные ошибки — по решению T-256
<!-- meta
priority: P2
type: full-fix
effort: medium
model: sonnet
wave: 17
depends_on: T-256
findings: G-24, S-21 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/runtime/json.go`, `internal/runtime/json_test.go`, `internal/vm/prelude_json.go`, `docs/01-language-design.md` (подраздел JSON по T-256, §10.4, N10), `examples/json.brig`, `examples/json.out`, `internal/examples/spec_tables_test.go` (`raiseNotInSpec104`)
- **Тест-якорь:** `TestJsonMappingTable` в `internal/runtime/json_test.go` — строка на каждую строку таблицы маппинга T-256.
- **DoD:**
  - `Json.decode("nope")` → `Error((:json, :syntax))` (или форма, выбранная T-256), паттерн `Error((:json, _))` совпадает;
  - `Json.decode(Json.encode(v)) == Ok(v)` для `None`, `Some(1)`, вложенных `Map`/`List` (вариант A);
  - неподдерживаемое значение в `encode` — `raise((:json, (:unsupported, v)))`;
  - спека содержит таблицу маппинга и ошибки; `make spec-tables` → ok;
  - `make all` → 0.
- **НЕ делать:** `Json.decode_as`; менять `Json.at`; поддержку `Range` (N10).

### T-267 · §0.2: дубли прелюдии и stdlib — по решению T-257
<!-- meta
priority: P3
type: full-fix
effort: low
model: sonnet
wave: 17
depends_on: T-257
findings: L-8, D-3 (AUDIT_REPORT-3)
-->
- **Файлы:** `stdlib/list.brig`, `internal/vm/prelude.go`, `internal/sema` (`BuiltinModules`), `docs/01-language-design.md` §7.3a, §11.5, тесты и примеры, которые зовут удаляемые имена
- **Тест-якорь:** `TestSpecPreludeMatchesInstall` (`make spec-tables`) и доктесты `stdlib/` (`brig test stdlib`).
- **DoD:**
  - по каждому пункту T-257 с решением «удалить»: имя удалено из кода, sema и спеки; `rg -n 'List\.(map|filter|fold)\b|Vec\.len' stdlib corpus examples docs internal --glob '!*_test.go'` пуст (для удалённых);
  - при решении по `to_int`: `to_int("42")` → `(:type_error, (:to_int, "42"))`, `Str.to_float` добавлен в таблицу §11.5;
  - правило §7.3a записано;
  - `make all` → 0.
- **НЕ делать:** удалять голые коллекционные функции; менять `<>`; пункты, по которым T-257 решил «оставить».

### T-268 · Docs: §0.11, структура и версия спеки — по решениям T-258, T-259
<!-- meta
priority: P3
type: docs
effort: medium
model: sonnet
wave: 17
depends_on: T-258, T-259, T-240
findings: S-14, S-19 (AUDIT_REPORT-3)
-->
- **Файлы:** `docs/01-language-design.md` (§0.11, §10.3, §15.3, §16, Part III), `docs/spec-history.md` (создать), `brig.ebnf` (заголовок), `internal/examples/spec_tables_test.go`, `tasks/*.md` и skills — ссылки на §H/§L
- **Тест-якорь:** создать `TestSpecVersionMatchesEbnf` в `internal/examples/spec_tables_test.go` (вариант A T-259).
- **DoD:**
  - §0.11, §15.3, §16 — по решению T-258;
  - по варианту A T-259: Part III перенесён в `docs/spec-history.md`, в спеке — раздел «Что изменилось в v0.5.0», версия `v0.5.0` в шапке спеки и в `brig.ebnf`, тест-якорь зелёный; §16 — списки по уровням;
  - `rg -n '§H\b|§L\b|§I\.|Part III' tasks .claude/skills docs` — ссылки ведут в `spec-history.md`;
  - `make check-examples` → `failed 0`; `make all` → 0.
- **НЕ делать:** менять нормативное содержание сверх T-258; переписывать историю; править `AUDIT_REPORT*.md`.

### T-269 · Разбить `compiler.go` на файлы (без изменения поведения)
<!-- meta
priority: P3
type: full-fix
effort: medium
model: opus
wave: 17
depends_on: T-261
findings: P-18, сопровождаемость (AUDIT_REPORT-3, раздел 10)
-->
- **Файлы:** `internal/compiler/compiler.go` (4 043 строки) → `compiler.go` (ядро: `Compiler`, `funcCompiler`, регистры), `expr.go`, `call.go`, `pattern.go`, `trap.go`, `record.go`, `module.go`; `.claude/skills/brig-compiler/SKILL.md`
- **Тест-якорь:** существующие: `go test ./internal/compiler`, `TestBytecodeGolden`.
- **DoD:**
  - только перенос функций между файлами пакета, без правки тел: `git diff --stat` — строки перемещены, `make update-bytecode` не даёт diff;
  - ни один файл пакета не длиннее 1 200 строк;
  - skill `brig-compiler` называет файлы, которые нельзя править параллельно; правка правила `WORKFLOW.md` §7.2 — предложение мейнтейнеру в PR (агент `WORKFLOW.md` не правит);
  - `make all` → 0; `BRIG_VERIFY=1 go test ./...` → 0.
- **НЕ делать:** менять логику, имена и сигнатуры; объединять с любой другой задачей компилятора; делать параллельно с другими правками `internal/compiler`.
