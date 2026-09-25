# Аудит Brig — отчёт

Проведён по `AUDIT_PROMPT.md`. Пробные программы и тесты, на которые ссылаются теги `[verified: …]`, лежали во временной копии репозитория и в репозиторий не добавлялись.

## 1. Шапка

- **Ветка / коммит / дата:** `iter/regvm` @ `8ab58cf7688dc5f74a30639e92ff03002bd2fb83`, 2026-09-25, go1.27.1.
- **Что запускалось:**
  - `go vet ./...` — ok;
  - `gofmt -l .` — пусто;
  - `go test ./... -race` — ok;
  - `BRIG_VERIFY=1 go test -count=1 ./...` — ok;
  - `make ci-quick` — ok;
  - `go run ./cmd/check-examples -- docs/01-language-design.md` — `checked 62, failed 0`;
  - fuzz 3×10s (`FuzzLex`, `FuzzParse`, `FuzzRoundTrip`) — PASS;
  - `golangci-lint run ./...` — **rc=1, 10 issues**;
  - около 60 пробных `.brig`-программ через `brig run/check`;
  - пробные Go-тесты в копии репо.
- **Шаг 0: насколько актуальны skills** (найдено 7 `.claude/skills/*/SKILL.md`; `.opencode` нет; skills `brig-cli/docs/sync/test` из сообщения коммита 9409a10 **не существуют**):

| Skill | Вердикт | Что разошлось с кодом |
|---|---|---|
| brig-compiler | **устарел частично** | Пишет, что компилятор проверяет инвариант «TAILCALL при trapDepth>0» и «should fail loudly». На деле `trapDepth` только инкрементируется и декрементируется и нигде не читается. Про «локальную fn своих и предков» — ищется только у прямого родителя. Про «операнд and/or хвостовой» — компилируется как CALL. |
| brig-vm | **устарел частично** | «send к мёртвому pid → Ok(())» и «watch мёртвого → :down :noproc» верны только для pid, который никогда не существовал. «(:down, ref, (:raise, val))» — значение `val` теряется. Для Verify не указано, что рёбра MATCHLOCAL и after не моделируются. |
| brig-overview | устарел частично | claim «65 блоков» — на деле 62. Иерархия источников истины расходится с AUDIT_PROMPT. |
| brig-parser-ast | устарел частично | Выдаёт golden `*.ast` за защиту AST, но `Pretty` печатает `(program )`. |
| brig-testing-workflow | почти актуален | Кроме «65». |
| brig-lexer | актуален (ориентир) | Исключения: `0x_1` принимается; ATOM после `)`. |
| brig-sema | актуален | — |

## 2. Executive summary

Инфраструктура в хорошем состоянии: vet, race, fuzz и Verify зелёные, граф импортов чистый, арифметика small-int/big.Int корректна. **Но уровень доверия к семантике низкий.** Зелёный CI держится на слабых тестах: многие тесты компилятора только печатают результат и ничего не проверяют, golden `.ast` пустые, у `vm.Verify` и REPL нет ни одного теста. Прогоны нашли несколько типов проблем:

- **Молча неверные результаты на канонических примерах спеки:**
  - `fact(5)=1`, `classify(-5)="positive"`, `pow(2,10)=1` (§6.1, §6.5);
  - интерполяция печатается как сырой текст;
  - `010=8`, `99999999999999999999` превращается во Float;
  - guard в `recv` отбрасывается парсером.
- **REPL падает Go-паникой на любом вводе**, хотя в STATUS он помечен ✅.
- **Спорные «слои защиты».** Слоя компилятора для TCO-под-trap нет. Инварианты I-1 и I-3 не проверяются. `vm.Verify` не видит тела веток `recv` и `after`.
- **Акторы:** причина `:down` теряет значение raise; мёртвые акторы не удаляются, поэтому `watch` зависает, а `send` возвращает `:busy`.
- **trap/ensure:** `trap` внутри колбэка прелюдии не ловит raise из вложенного кадра; `ensure` не видит локали тела (пример из §10.3 падает).

---

## 3. Находки по слоям

### Слой 1 — Синтаксис

**S-F1 · blocker · Интерполяция строк не разбирается**
- Где: `parser/expr.go:401-405` (STRING → LiteralExpr), `compiler.go:1844-1848` (`\(` остаётся как текст). §C.2, `interpolation ::= "\\(" expr ")"`, §16 Must.
- Что не так: выражение внутри `\(...)` не попадает в AST. Его не видит sema, компилятор эмитит сырой текст. Невалидное `"a \(1 +) b"` принимается.
- Тег: [verified: `brig run p/t3_interp.brig` → `x = \(x)`; probe `interp_bad` парсится].
- Фикс: лексер отдаёт части строки → парсер строит узел Interp(parts, exprs) → компилятор делает конкатенацию через `to_str`. До готовности — fail-fast в компиляторе.

**S-F2 · blocker · Параметры и guard fn хранятся строками → молча неверная компиляция**
- Где: `parser/stmt.go:111-146` (`pat.String()`), `:90-99` и `:190-199` (guard → `normalizeGuardString`); `compiler.go:357-371` (берётся только `clauses[0]`, строка `"0"` связывается как имя). §6.1, §6.5, грамматика `param ::= pattern`.
- Что не так: AST не может выразить параметр-паттерн. В итоге:
  - `fn fact(0)…fn fact(n)` → `fact(5)=1`;
  - guard игнорируется → `classify(-5)="positive"`;
  - пример из §6.5 → `pow(2,10)=1`.
- K-8 объявляет эти фичи «вне рамок», но при этом не должен давать неверный результат молча.
- Тег: [verified: `p/t10_multiclause.brig`, `p/u2_guard_fn.brig`, `p/u1_spec65.brig`].
- Фикс: немедленно — `Compile` возвращает ошибку, если `len(clauses)>1`, есть guard или параметр не `IdentPattern`/`..name`. Затем — `Params []ast.Pattern`, `Guard ast.Expr` в AST.

**S-F3 · major · Guard в ветках `recv` разбирается и выбрасывается**
- Где: `expr.go:937-941` (инлайн-форма), `:970-974` (блочная). В `RecvBranchArg` нет поля Guard. §12.4, §6.1.
- Что не так: `Format` печатает ветку без `when`. Round-trip на основе `ast.Equal` этого не видит, потому что guard нет в обоих AST. Канонический пример из §12.4 (`when has_pending(...)`) матчит не ту ветку.
- Тег: [verified: probe `recv_guard`; `p/t9_guard.brig` → `:big` для 5].
- Фикс: guard в AST, Format и sema; компилятор — fail-fast, пока guard не поддержан.

**S-F4 · major · Guard из одного идентификатора разбирается как lambda_short**
- Где: `expr.go:34` (`tryLambda`) вызывается из разбора guard. Затронуты `fn f(x) when x -> 1` и `n when ok -> …`.
- Что не так: валидная по грамматике программа получает ошибку парсинга `expected '->'`.
- Тег: [verified: probes `fn_guard_ident`, `recv_guard_ident`].
- Фикс: guard разбирать через `parseOr()`, а не через `parseExpr()`.

**S-F5 · major · `ensure`-блок**
- Где: `expr.go:887-906`. `ensure_clause ::= "ensure" NEWLINE INDENT stmt_list DEDENT`.
- Что не так:
  - форма из грамматики не парсится;
  - гибрид `ensure expr NEWLINE INDENT…` принимается, а блок **молча выбрасывается**.
- Блочная форма — «задел», но терять код молча нельзя.
- Тег: [verified: probes `ensure_block_grammar` (ошибка), `ensure_hybrid` + `p/u3_ensure_hybrid.brig` (`:dropped_block_stmt` не печатается)].
- Фикс: отвергать гибрид; блочную форму либо реализовать, либо явно отвергать с ссылкой на MVP.

**S-F6 · major · Парсер не требует NEWLINE между стейтментами**
- Где: `stmt.go:13-26` (`parseStmtList`). Грамматика: `stmt_list ::= stmt { NEWLINE stmt }`.
- Что не так: `x = 1 y = 2` принимается как две строки. В сочетании с лексером `x = 0b102` превращается в `x = 0b10` плюс висящее выражение `2` — молча.
- Тег: [verified: probe `two_stmts_one_line`; `p/u4_two_stmt_line.brig`; `p/z1.brig` → печатает 2].
- Фикс: после `parseStmt` требовать NEWLINE, DEDENT, EOF или `until`.

**S-F7 · major · Числовые литералы: восьмеричные и потеря Int**
- Где: `compiler.go:1786-1791` — `strconv.ParseInt(s, 0, 64)` с откатом на `ParseFloat`. §3.1, `int_lit ::= dec_digits`.
- Что не так:
  - `010` → 8;
  - `08`/`09` → Float;
  - литерал больше int64 → Float (`99999999999999999999` → `1e+20`), хотя Int — произвольной точности;
  - `-9223372036854775808` → Float.
- Тег: [verified: `p/t4_leading0.brig`, `p/u5_octal.brig`, `p/t2_bigint.brig`].
- Фикс: разбирать по префиксу (`0x`/`0b`/`0o` → base 16/2/8, иначе base 10) через `big.Int.SetString`, затем `runtime.IntBig`.

**S-F8 · minor · `sep ::= NEWLINE` не поддержан в args/params/tuple**
- Где: `parseArgs` (`expr.go:377`), `parseParams` (`stmt.go:134`), tuple (`expr.go:470`). Спека §A.1 п.1, §D.5.
- Что не так: лексер эмитит NEWLINE, а парсер ждёт `,`. В list/map/record это сделано.
- Тег: [verified: probes `args_newline_sep`, `params_newline_sep`].

**S-F9 · minor · Паттерн `()` (unit_lit) не разбирается**
- Где: `pattern.go:98-103`.
- Тег: [verified: probe `unit_pattern`].

**S-F10 · minor · `with`: bind после стейтмента → ошибка парсинга**
- Где: `expr.go:797-813`. Грамматика: `with_item ::= bind_stmt | stmt`, в любом порядке.
- Тег: [verified: probe `with_interleave`].

**S-F11 · minor · Лексер: отдельные формы**
- `0x_1` принимается, хотя §3.1 запрещает `_` рядом с `x`.
- `0b102` превращается в `0b10`+`2` без ошибки.
- После `)` эмитится ATOM вместо COLON (нарушение правила §1.5).
- Тег: [verified: `brig check` probes; `p/z4.brig`].

**S-F12 · minor · Round-trip guard не идемпотентен**
- Где: `stripOuterParens` (`stmt.go:232`) считает скобки внутри строковых литералов.
- Что не так: `fn f(x) when x == ")" -> 1` даёт `EQUAL=false IDEMPOTENT=false`.
- Тег: [verified: probe `guard_paren_string`].

**S-F13 · major (инфраструктура слоя 1) · `ast.Pretty` и `ast.Walk` не видят Decl**
- Где: все Decl реализуют `IsExpression()` (`decl.go:14,25,40,69`), поэтому ветка `case Expr` срабатывает раньше `case Decl` (`pretty.go:35`, `visitor.go:31`).
- Что не так: 24 из 25 `testdata/golden/*.ast` равны `(program )`. Golden-тесты AST ничего не проверяют.
- Тег: [verified: `grep -L`; пробный тест `TestAuditPrettyPrintsFuncDecl`].
- Фикс: убрать `IsExpression` у Decl или поставить `case Decl` первым; затем `make update-golden` с ручным просмотром diff.

### Слой 2 — Архитектура

**A-F1 · major · Модель планировщика расходится со спекой**
- Где: architecture.md:148 и §15.2 спеки говорят «1 актор = 1 goroutine». Код — однопоточный кооперативный run-loop (`scheduler.go:313-428`). Расхождение зафиксировано только в skill (тир 4).
- Тег: [inferred: scheduler.go:320-350].
- Фикс: задокументировать отклонение в architecture.md и doc 02 либо привести код к спеке.

**A-F2 · major · Заявленные слои защиты отсутствуют**
- Где: doc 02 §4 и §7 («инварианты проверяются всегда»).
- Что не так:
  - `trapDepth` — write-only (`compiler.go:1292-1411`, ни одного чтения);
  - `bound[]` — write-only (`:138,164`), значит I-3 не проверяется;
  - проверки I-1 (стек-нейтральность `compileExpr`) нет.
- TCO-под-trap держится только на двух вещах: тело trap компилируется с `val(dst)` (структурно) и есть проверка в Verify.
- Тег: [verified: `grep -n 'bound\[\|trapDepth' internal/compiler`].
- Фикс: в `compileGenericCall`/`compileGlobalCall` — `if d.tail && fc.trapDepth>0 { fc.fail(...) }`; в `emit` — проверка записи A в `bound`-регистр (кроме `MATCHLOCAL`); в `compileExpr` — `defer`-проверка `nextReg`.

**A-F3 · major · K-2 (truthiness) противоречит тиру 1**
- Где: doc 02 K-2 разрешает «не-Bool проваливается». Спека: §7.2 «всегда Bool», §8.1 «if ≡ match true/false», §16 Must «строгий Bool» и «Не надо: truthiness».
- Что не так: `if 5 then` идёт в then-ветку, `1 or 2` → 2.
- Тег: [verified: `p/t7_if_nonbool.brig`, `p/w3_andor_nonbool.brig`].
- Фикс: `JMPIF`/`JMPIFNOT` на не-Bool → `(:type_error, …)`; правое значение `and`/`or` проверять на Bool.

**A-F4 · major · Ошибки классифицируются непоследовательно**
- Где: K-3 (doc 02) говорит, что type errors не ловятся. Спека §10.4 перечисляет `:type_error` среди авто-raise. Код: `decArithErr` возвращает ловимый ErrRaise, а `arithErr`, `NOT` и `Compare` — `fmt.Errorf`.
- Что не так: `trap(dec"1"+"a")` → `Error(...)`, а `trap(1+"a")` убивает актор.
- Тег: [verified: `p/x1_typeerr.brig`].

**A-F5 · major · Два run-loop расходятся в семантике raise**
- Где: `callSync` (`scheduler.go:1096-1097`) при `stepFailed` сразу возвращает ошибку, без `tryUnwindRaise`.
- Что не так: `map(fn (x) -> trap(g(x)), xs)`, где `g` бросает raise, валит main.
- Тег: [verified: `p/v4_trap_across_callsync.brig` → `raise: :bad`, rc=2].
- Фикс: в `callSync` при `stepFailed` вызывать `s.tryUnwindRaise(tmp)`, по образцу `runSlice`.

**A-F6 · minor · Разрешение имён и манглинг локальных fn**
- Где: `compileVar` (`compiler.go:752-758`) смотрит `localFns` только у себя и у прямого родителя. Doc 02 §7 требует «предков». Все лямбды одной функции получают общий `prefix+"lambda$"` (`:1588`), поэтому одноимённые локальные fn в разных лямбдах перезаписывают друг друга в `image.Functions`.
- Тег: [verified: `p/t5_…` → `undefined: h`; `p/t6_…` → печатает `2 2`].

**A-F7 · minor · Коды выхода и обход sema в тестах**
- «срез: не реализовано» (ошибка пользователя) → exit 3 «internal».
- `internal: upvalue out of range` → exit 2 «raise».
- Тесты компилятора (`runModule`) не прогоняют sema и содержат программы, которые CLI отверг бы, например `print(trap(1+1))`.
- Тег: [inferred: cmd/brig/main.go:162-166, 208-211; compiler_test.go:13-33].

**A-F8 · minor · Недокументированные пробелы вне K-8**
- Pipe `|>` (§7.5), record-литералы, `link` (§12.6), `Sys.args()` (§16 Must), `mailbox_size()` без аргументов (§12.6).
- Интерполяция — хуже остальных, потому что она **молчаливая** (S-F1).
- Тег: [verified: `p/w4_pipe.brig`, `p/y.brig`-пробы].

### Слой 3 — Реализация

**R0**

**I-F1 · major · В `vm.Verify` дыра в CFG**
- Где: `successors` (`verify.go:304-316`) не моделирует успешное ребро `MATCHLOCAL` (ip+2) и ребро `RECVTAKE→after`. `applyWrites` не помечает слоты паттерна как определённые.
- Что не так: тела всех веток `recv` и `after` для анализа definite assignment недостижимы (`in[ip]==nil`) и не проверяются вовсе.
- Тег: [verified: `copy/internal/vm/zz_verify_probe_test.go` — чтение неопределённого r2 в теле ветки и в after принимается].
- Фикс: у `MATCHLOCAL` преемники `{ip+1, ip+2}` + слоты паттерна на ребре ip+2 (новый `CompiledPattern.Slots()`); у `RECVTAKE` с `sBx≠0` — `{ip+1, ip+1+sBx}`.

**I-F2 · major · Ни один из трёх слоёв TCO-под-trap не покрыт тестом**
- Слой компилятора отсутствует (A-F2).
- Unit-тестов `vm.Verify` нет вовсе: нет `verify_test.go`, grep по `TAILCALL under` пуст.
- Guard в VM (`scheduler.go:676`) не тестируется.
- Структурная гарантия сейчас держится, пробный тест `TestAuditNoTailCallInsideTrap` проходит. Но регресс поймал бы только Verify, и только если в тестах есть trap с вызовом в хвостовой позиции — таких тестов нет.
- Тег: [verified: grep + пробный тест].

**I-F3 · major · Правый операнд `and`/`or` компилируется не в хвостовой позиции**
- Где: `compileAndOr` (`compiler.go:921`) передаёт `val(acc)` вместо `d`. Doc 02 §4, таблица п.6; принцип #11.
- Что не так: `fn loop(n) -> n == 0 or loop(n - 1)` → `CALL r2 1 -> r1; RETURN r1`, кадры растут.
- Тег: [verified: `brig run --dump-bytecode p/s7_andor_tailcall.brig`].
- Фикс: в хвостовом контексте `compileExpr(b.Right(), d)`, а для левой ветки эмитить `RETURN acc` после перехода.

**I-F4 · ok · TAILCALL + `clear(regs[NumParams:cap])` корректны**
- `bindArgs` использует семантику memmove, variadic-хвост копируется первым (`scheduler.go:129-139, 701-711`). Нарушений не найдено.
- Тег: [inferred + TestTailCallArgOrder/GCD].

**R1**

**I-F5 · major · `ensure`: область видимости и момент регистрации**
- Где: ensure компилируются после `popScope()` тела (`compiler.go:1388-1407`), поэтому не видят локалей тела. Пример из §10.3 (`ensure close(f1)`) падает с `undefined: f1`.
- Что не так: ensure выполняется, даже если raise случился до точки его «регистрации», а §10.3 говорит «от точки регистрации».
- LIFO и «последняя ошибка побеждает» — ✓.
- Тег: [verified: `p/v1_…` → `undefined: f1`; `p/v2_…` печатает `:should_not_run…`; `p/v3_…` → `Error(:first_registered)` ✓].
- Фикс: компилировать ensure в области тела; флаг «зарегистрирован» на каждый ensure (LOADK true в точке текста) и JMPIFNOT перед его выполнением.

**I-F6 · nit · Преинициализация `dst` в trap+ensure — не дыра**
- Где: `compiler.go:1368-1374`. Verify не различает пути: состояние обработчика — это состояние на `TRAPBEGIN` плюс `errReg` (`verify.go:252-259`). Поэтому он дал бы ложное срабатывание на `MAKEOK T T`.
- Преинициализация — мёртвая запись на обоих путях: тело перезаписывает `T`, на пути ошибки перезаписывает `MAKEERROR`. Ложного пропуска (false negative) нет. Комментарий в коде путает эти термины.
- Тег: [inferred].

**I-F7 · major · Локальная fn с захватом падает только в рантайме**
- Где: `compileLocalFn` создаёт child с `parent=fc` (`:618`), поэтому `resolveUpvalue` регистрирует upvalue. Но функция кладётся как обычная глобальная `Function` без captures → `internal: upvalue 0 out of range`, rc=2. Спека §6.5 требует захвата.
- Тег: [verified: `p/t1_localfn_capture.brig`].
- Фикс: минимум — `fail` при `len(child.upvalues)>0`; полностью — лямбда-лифтинг (передача захватов как скрытых параметров) либо замыкание плюс letrec.

**I-F8 · major · Сквозная типобезопасность равенства**
- `PatLiteral` использует `runtime.Equal` (`pattern.go:67`), поэтому паттерн `1` матчит `1.0` и `dec"1"`, хотя §4.8 требует точного сопоставления.
- Decimal×Float: операторы `==` и `<` бросают ошибку, а `INDEX` (`scheduler.go:1038`), `set`/`Map.*` (`prelude.go:103,286,304,317`) и паттерны молча дают false. Это оформлено только комментарием в `vm.go:361-363`; в спеке и doc 02 решения нет.
- Int↔Float сравниваются через float64 (`value.go:370`): `2^53+1 == 2^53.0` → true. Равенство нетранзитивно, дедупликация в Set и Map неверна.
- Тег: [verified: `p/w1_pat_exact.brig`, `p/w2_bigprec.brig`, `p/v6_mixed.brig`].
- Фикс: отдельная `runtime.MatchEqual` (тот же Kind + точное значение) для паттернов; точное сравнение Int×Float через `big.Float`/`big.Rat`; решение для Decimal×Float внести в doc 02.

**R2**

**I-F9 · major · Мёртвые акторы не удаляются из `s.actors`**
- Где: удаления нет нигде (grep `delete(s.actors` пуст).
- Что не так:
  - `watch` на завершившийся актор никогда не шлёт `:down` — наблюдатель висит;
  - `send` на такой актор копит mailbox и после 64 сообщений возвращает `Error(:busy)`;
  - `mailbox_size` мёртвого актора = 64 (doc 02 §6 требует 0).
- Тег: [verified: `p/s2_watch_dead.brig`, `p/s3_send_dead.brig`].
- Фикс: удалять актор при `actorDone`/`actorFailed` (кроме `mainPid`, чей результат читается).

**I-F10 · major · Причина `:down` теряет значение raise**
- Где: `fail()` обнуляет `a.result = Unit` (`scheduler.go:437-441`), после чего вызывается `notifyWatchers(Tuple(:raise, a.result))` (`:414`).
- Что не так: получается `(:down, ref, (:raise, ()))`.
- Тег: [verified: `p/s1_down_reason.brig`].
- Фикс: брать значение из `errors.As(a.err, &rerr)`.

**I-F11 · ok · `MATCHLOCAL`+`JMP`**
- Единственное место эмиссии — `compileRecv:1469-1470`, пара эмитится всегда. `match`/`with` не компилируются. Verify проверяет пару.
- Тег: [inferred].

**I-F12 · ok · Граница small-int/big.Int**
- add/sub/mul/div/rem/neg корректны вблизи `MinInt64`.
- Тег: [verified: `p/x2_minint.brig`].

**I-F13 · minor · JSON**
- Коллизия маркера `$bytes`: `%{"$bytes"=>"aGk="}` → encode → decode даёт `b"hi"`.
- Inf кодируется как `+Inf`, а это не JSON.
- Float `1.0` после round-trip становится Int.
- Дубликаты ключей молча решаются в пользу последнего.
- Лимиты глубины есть ✓.
- Тег: [verified: `p/s8_json_inf.brig`, probes].

**I-F14 · minor · Таймеры**
- Большой `ms` в `RECVTIMER` молча усекается или переполняется (`scheduler.go:888-894`).
- `wakeExpired` обходит map, поэтому порядок в `ready` недетерминирован (§15.4).
- Тег: [inferred].

**R3**

**I-F15 · minor · Проверка позиции `trap` неполная**
- Sema запрещает только аргументы и элементы литералов. `x = 1 + trap(y)` и `if c then trap(y) else z` принимаются, хотя по §10.2 trap разрешён только как RHS `let` или как отдельный expr_stmt.
- Тег: [verified: `brig check` probes].

### Слой 4 — Всё остальное

**O-F1 · blocker · REPL падает nil-deref на любой строке**
- Где: `CompileReplLine` не инициализирует `c.image`, а `repl.go:81` читает `c.Image().Functions`. В `internal/repl` нет тестов.
- Тег: [verified: `printf 'x = 5\n' | go run ./cmd/brig repl` → panic; `TestAuditReplSnapshot` → panic].
- Фикс: в `CompileReplLine` — `c.image = &ProgramImage{Functions: map…}` плюс тест.

**O-F2 · major · Тесты без проверок**
- Тела тестов ничего не утверждают, только печатают: `TestTrapEnsureLifo`, `…LifoOnError`, `TestEnsureAllRunOnFailure`, `TestTrapEnsureRaisesIn*`, `TestTrapCatchesDivisionByZero`, `TestTrapBlockPropagatesThroughFn`, `TestAndOr`, `TestLocalFn`, `TestClosure*`, `TestMutualRecursion`, `TestLocalFnRecursion`.
- LIFO и «последняя побеждает» не утверждаются ни одним тестом.
- Тег: [verified: чтение compiler_test.go:60-260, 478-490].

**O-F3 · minor · `make all` и CI красные**
- `golangci-lint` находит 10 проблем (errcheck 1, revive 6, staticcheck 3), rc=1.
- Тег: [verified].

**O-F4 · minor · Позиции `0:0` в bytecode-goldens**
- 41 инструкция в goldens имеет позицию `0:0` (Must §15.1 требует line:col). `fc.pos` не выставляется перед LOADK/GETGLOBAL callee.
- Тег: [verified: `grep -c ' 0:0 ' testdata/bytecode/*.txt`].

---

## 4. Сводная таблица

| ID | Слой | Sev | Кратко |
|---|---|---|---|
| S-F1 | Синт | blocker | Интерполяция не разбирается, печатается сырой текст |
| S-F2 | Синт | blocker | Параметры/guard строками → мультиклоз/guard/паттерны молча неверны |
| S-F3 | Синт | major | Guard в `recv` выбрасывается парсером |
| S-F4 | Синт | major | `when ident ->` разбирается как лямбда |
| S-F5 | Синт | major | Блок `ensure`: ошибка или молча теряется |
| S-F6 | Синт | major | Нет обязательного NEWLINE между стейтментами |
| S-F7 | Синт | major | `010`=8, `08`→Float, большой Int→Float |
| S-F8 | Синт | minor | NEWLINE-sep в args/params/tuple |
| S-F9 | Синт | minor | Паттерн `()` |
| S-F10 | Синт | minor | Порядок bind/stmt в `with` |
| S-F11 | Синт | minor | Лексер: `0x_1`, `0b102`, ATOM после `)` |
| S-F12 | Синт | minor | Guard с `")"` — round-trip не идемпотентен |
| S-F13 | Синт | major | `Pretty`/`Walk` не видят Decl, golden `.ast` пустые |
| A-F1 | Арх | major | «1 актор = 1 goroutine» vs однопоточный loop |
| A-F2 | Арх | major | `trapDepth`/I-1/I-3 не проверяются |
| A-F3 | Арх | major | K-2 truthiness vs строгий Bool (тир 1) |
| A-F4 | Арх | major | type_error то ловится, то нет |
| A-F5 | Арх | major | `callSync` без unwind — trap в колбэке не работает |
| A-F6 | Арх | minor | Разрешение и манглинг локальных fn |
| A-F7 | Арх | minor | Коды выхода; тесты в обход sema |
| A-F8 | Арх | minor | Пробелы вне K-8 (pipe, record, link, Sys.args) |
| I-F1 | Реал | major | Verify: нет рёбер MATCHLOCAL/after |
| I-F2 | Реал | major | Нет тестов на три слоя TCO-под-trap |
| I-F3 | Реал | major | `and`/`or` не хвостовые |
| I-F5 | Реал | major | Область видимости и регистрация `ensure` |
| I-F6 | Реал | nit | Преинициализация `dst` — не дыра |
| I-F7 | Реал | major | Захват в локальной fn → падение в рантайме |
| I-F8 | Реал | major | Равенство: паттерны неточны, Decimal×Float, 2^53 |
| I-F9 | Реал | major | Мёртвые акторы: `watch` висит, `send` → `:busy` |
| I-F10 | Реал | major | `:down` теряет значение raise |
| I-F13 | Реал | minor | JSON: `$bytes`, Inf, 1.0 |
| I-F14 | Реал | minor | Таймеры: усечение и недетерминизм |
| I-F15 | Реал | minor | Неполная проверка позиции trap |
| O-F1 | Прочее | blocker | REPL — паника |
| O-F2 | Прочее | major | Тесты без утверждений |
| O-F3 | Прочее | minor | lint rc=1 |
| O-F4 | Прочее | minor | Позиции `0:0` |

## 5. Проверка claim'ов STATUS.md / CHANGELOG / architecture.md

| Claim | Итог | Доказательство |
|---|---|---|
| check-examples «65 блоков» | ✗ | `blocks: checked 62` (в доке 68 fenced-блоков, из них 6 `invalid`) |
| «D-1..D-5 закрыты» | ✓ | Тела тестов прочитаны (`regvm_test.go`). D-4 — только для лямбд. `TestVariadicSum` — Skip |
| «vm.Verify — линейный dataflow» | частично ✗ | I-F1 |
| «Хвостовость распознаётся точно» | ✗ | I-F3 |
| «REPL ✅, `x=5…f()→5`» | ✗ | O-F1 |
| «`examples/test_demo.brig`» | ✗ | Файла нет (`ls examples`) |
| «CI → make all» зелёный | ✗ | `golangci-lint` rc=1 |
| «авто-raise `:function_clause`» | частично | Только арность; несовпадение клоза не проверяется (S-F2) |
| «49 опкодов», `isTailCall` удалён | ✓ | Константы в `opcodes.go`; grep пуст |
| architecture.md: `check-smallint` в `ci-quick` | ✗ | В цели `ci-quick` Makefile его нет |
| architecture.md: TCO «проверяется линейно в Verify» | ✓ | Проверено пробным тестом |
| «все негативные кейсы лексера» | почти | `0x_1` (S-F11) |
| race / verify / ci-quick / fuzz зелёные | ✓ | Прогоны |

## 6. Что сделано хорошо

- Арифметика small-int: корректные проверки переполнения, включая `MinInt64` (`vm.go:143-151,195-200,249,264`) [verified x2].
- Кадр TAILCALL переиспользуется безопасно для memmove; для GC делается `clear` (`scheduler.go:701-714`).
- K-5: native получают свежий слайс (`scheduler.go:646,685`).
- Схема флаг-регистра D-5; LIFO и «последняя побеждает» работают [verified v3].
- Единая таблица `vm.RegUse` для `emit` (I-4) и Verify (`verify.go:83`, `compiler.go:244`).
- Настоящие блочные области видимости (D-4) [verified v5]; рекурсивный `resolveUpvalue` для лямбд.
- Чистый граф импортов, нарушений слоёв нет (`go list`); `runtime.Code` развязывает `runtime` и `vm`.
- Строгая валидация escape в лексере (суррогаты, `\x` в Str, `\u` и `\(` в Bytes, табы, continuation).

## 7. Тесты, которые стоит добавить

Код готов и прогнан в копии. Все тесты, кроме первого, **сейчас падают**, что подтверждает находки.

- **`internal/compiler/audit_regress_test.go`** (стиль `regvm_test.go`):
  - `TestAuditNoTailCallInsideTrap` — дизассемблирование `f_inline`/`f_block`/`f_ensure`, в них нет `TAILCALL`. Сейчас проходит, это guard-тест;
  - `TestAuditAndOrRightOperandIsTail`;
  - `TestAuditMultiClauseNotSilentlyDropped` — допустимы либо fail-fast, либо корректный результат;
  - `TestAuditLocalFnCapturesEnclosingParam`;
  - `TestAuditBigIntLiteral`;
  - `TestAuditLeadingZeroIsDecimal`;
  - `TestAuditRecvGuardSurvivesRoundTrip`;
  - `TestAuditDownReasonCarriesRaiseValue`;
  - `TestAuditTrapInsideNativeCallback`;
  - `TestAuditEnsureSeesBodyLocals`;
  - `TestAuditLiteralPatternIsExact`.
- **`internal/vm/verify_test.go`** — вручную собранные чанки через `NewChunk`/`Emit`:
  - `TAILCALL` между `TRAPBEGIN` и `TRAPEND` → ошибка (сейчас ✓);
  - чтение неопределённого регистра в теле ветки после `MATCHLOCAL`+`JMP` → ошибка (сейчас ✗);
  - то же для after-ветки `RECVTAKE` (сейчас ✗).
  - Плюс тест guard в VM: чанк с `TRAPBEGIN; TAILCALL` без Verify → `RunMain` возвращает `internal: TAILCALL under active trap`.
- **`internal/ast/audit_pretty_test.go`** — `Pretty(fn main)` содержит `(fn main`.
- **`internal/repl/audit_repl_test.go`** — снимок N12: `x=5; f=()->x; x=10; f()==5`.
- Переписать print-only тесты (O-F2) на `assert(result == Error(:x))`. Для LIFO: `result == Error(:first_registered)` при двух `ensure raise(...)`.

Исходники: `…/scratchpad/copy/internal/{compiler/audit_regress_test.go, vm/zz_verify_probe_test.go, ast/audit_pretty_test.go, repl/audit_repl_test.go}`.

## 8. Мелкое (документация)

- architecture.md после строки ~287 содержит вставленные копии README.md и STATUS.md — дубли, которые устаревают.
- doc 02, шапка: «раздел для вставки… Я не компилировал этот код» — остаток черновика.
- §12.4 (проза «else/after — только блочная форма») противоречит `brig.ebnf` `after_clause` с инлайн-формой `-> expr` — источник истины несогласован сам с собой.
- skill brig-overview ставит doc 02 выше `brig.ebnf`/architecture.md — противоречит иерархии в AUDIT_PROMPT.
- `cmd/brig/main.go:6,203`: «repl — отладочный цикл (токены лексера)» — устарело; `main.go:25` ссылается на несуществующий «скилл brig-cli».
- Makefile, `ebnf-check`: «regeneration from §16 pending» — устарело.

## 9. Что не покрыто в этом проходе

- Мини-блоки офсайда внутри скобок (§D.6/D.7) и якоря `trap`/`if` в глубину; §D.8 NEWLINE у клауз.
- Полный term order `runtime.Compare` (§7.4) и сортировка смешанных типов.
- Реальные гонки в `RunMainWithArgs`/REPL — REPL не запускается (O-F1); переиспользование `Scheduler` между строками.
- Аудит арности native в прелюдии (открытый вопрос 3), тест-фреймворк `Test.*`, `Serialize`, `FormatDecimal`.
- sema для `match`/`with`/record (K-8), парсер типов (`type.go`).
- `make fuzz` 3×60s не запускался — только 10s на таргет.

