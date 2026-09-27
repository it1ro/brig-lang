# Wave 9 — Should §16

[← карта плана](README.md)

**Вход:** Wave 7 закрыта; для T-126 — T-108 и T-117. Повторный аудит (T-110) желательно провести раньше: его findings могут встать перед этой волной как Wave 10. **Выход:** реализованы Should-фичи, у которых есть спека или решение DD. Фичи без спеки сначала проходят DD (T-121, T-122); номера T-127…T-129 зарезервированы под реализацию по итогам этих DD.

**Уже работают, задач не нужно:** `Decimal` (`dec"..."`), `when`-guards, as-паттерны.

**Не в этой волне:** `rx"..."` ждёт DD T-134 (движок); порты (subprocess-FFI) и runtime-контракты для аннотаций — нет спеки, горизонт после волны 9.

| T-NN | Название | depends_on | Тип | Источник |
|---|---|---|---|---|
| T-120 | Паттерны в параметрах полной лямбды `fn ((a, b)) -> …` | — | feature | spec-gap |
| T-121 | DD: Should без спеки — kwargs, `"""`, `Range` в JSON | — | design-decision | spec-gap |
| T-122 | DD: stdlib `call` и `trace(pid)` + логер (§17 п.9) | — | design-decision | spec-gap |
| T-123 | Docs: схема TCO сквозь `ensure` (cleanup-регистр) в doc 02 | — | docs | spec-gap |
| T-124 | TCO сквозь `ensure`: реализация | T-123 | feature | spec-gap |
| T-125 | `Supervisor.start` (§13.1) | — | feature | spec-gap |
| T-126 | `Behavior` и `spawn_behavior` (§13.2) | T-108, T-117 | feature | spec-gap |
| T-127…T-129 | резерв: реализация по решениям T-121/T-122 | T-121 / T-122 | feature | spec-gap |

## Задачи

### T-120 · Паттерны в параметрах полной лямбды `fn ((a, b)) -> …`
<!-- meta
priority: P2
type: feature
effort: M
model: opus
wave: 9-should
depends_on: —
findings: — (проба роадмапа: `fn ((a, b)) -> a + b` → `срез: параметр-паттерн "(a, b)" в лямбде не реализован`; fail-fast из T-44)
-->
- **Файлы:** `internal/compiler/compiler.go:613` (fail-fast T-44), `:2586` (`compileLambda`), `:725` (`compileOneClause` — образец для именованных fn)
- **Тест-якорь:** существующий тест на fail-fast T-44 переписать в `TestLambdaPatternParams`; создать `TestLambdaPatternParamMismatch`
- **DoD:**
  - `fn ((a, b)) -> a + b` вызывается с `(1, 2)` → `3`; `fn ([h, ..t]) -> h` работает; `map(fn ((k, v)) -> v, pairs)` работает;
  - несовпадение → ловимый `(:function_clause, args)`, как у именованной fn;
  - замыкания и захват в такой лямбде работают (`TestLambdaPatternParams` включает захват внешнего имени);
  - `rg -n 'параметр-паттерн .* в лямбде не реализован' internal/compiler` пуст; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** мультиклозные лямбды (спека их не вводит); guard в лямбде; менять `() ->`-форму; трогать связывания `pat = expr` (T-101).

### T-121 · DD: Should без спеки — kwargs, `"""`, `Range` в JSON
<!-- meta
priority: P2
type: design-decision
effort: S
model: human
wave: 9-should
depends_on: —
findings: — (§16 Should называет kwargs, многострочные строки и сериализацию `Range`, но спека не определяет ни синтаксиса, ни формата; проба: `Json.encode(1 to 3)` → `(:json_encode_error, … (:unknown_kind, Range))`)
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §3.5, §6, §16, Part II N10; `internal/runtime/json.go`; `internal/runtime/value.go:969` (`Serialize` уже пропускает `Range`)
- **Тест-якорь:** — (решение)
- **Вопросы и варианты:**
  1. **kwargs.** **A:** сахар над последним аргументом-анонимной записью: `f(a, timeout: 5)` ≡ `f(a, {timeout: 5})`, без дефолтов (§16 «Не надо»: дефолтные аргументы). **B:** настоящие именованные параметры в сигнатуре. **C:** не вводить, закрыть пункт Should.
  2. **`"""`.** **A:** отступ снимается по колонке закрывающих `"""` (как в Swift), интерполяция `\(...)` работает. **B:** без снятия отступа и без интерполяции. **C:** оставить не-MVP.
  3. **`Range` в JSON.** **A:** `{"$range": [start, end]}` (по образцу маркера `$bytes`, T-42). **B:** массив `[start, …, end]` — с потерей типа при decode. **C:** оставить `:json_encode_error`, «сериализация» в N10 значит только `runtime.Serialize` (уже работает).
- **DoD:** в issue записан вариант по каждому пункту; для каждого пункта, который делается, — черновой блок задачи T-127…T-129 (meta, Файлы, Тест-якорь, DoD, НЕ делать) в `tasks/wave-9.md`. Issue закрыт.
- **НЕ делать:** писать код; вводить дефолтные аргументы (§16 «Не надо»); решать `trap(fn, timeout:)` (T-131).

### T-122 · DD: stdlib `call` и `trace(pid)` + логер (§17 п.9)
<!-- meta
priority: P2
type: design-decision
effort: S
model: human
wave: 9-should
depends_on: —
findings: —
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §12, §15.2 (редукционный счётчик и `trace(pid)`), §16, §17 п.9; `internal/vm/prelude.go` (`log`)
- **Тест-якорь:** — (решение)
- **Вопросы и варианты:**
  1. **`call(pid, msg, timeout)`** — синхронный запрос-ответ поверх `make_ref` + `send` + `recv … after`. **A:** в прелюдию. **B:** в модуль stdlib (`Actor.call`), нужен `import`. **C:** не вводить, пусть это будет паттерном в документации.
  2. **`trace(pid)` и логер.** **A:** `trace(pid)` включает печать входящих сообщений и `:down` актора в stderr через `log`; уровни логера — атомы `:debug`/`:info`/`:warn`/`:error`. **B:** `trace(pid)` отдаёт события сообщениями подписчику (актору-трассировщику). **C:** отложить до Nice.
- **DoD:** в issue записан вариант по обоим пунктам; для того, что делается, — черновой блок T-127…T-129 в `tasks/wave-9.md`. Issue закрыт.
- **НЕ делать:** писать код; менять `log`; решать kwargs (T-121).

### T-123 · Docs: схема TCO сквозь `ensure` (cleanup-регистр) в doc 02
<!-- meta
priority: P3
type: docs
effort: M
model: opus
wave: 9-should
depends_on: —
findings: — (doc 02:306: «TCO сквозь `ensure` остаётся Should и не реализуется»)
-->
- **Файлы:** `docs/02-register-based-virtual-machine.md` (раздел о trap/ensure и TCO, строка 306; соглашения K-*); `docs/01-language-design.md` §10.3, §15.3 — только ссылка на doc 02, норматив не меняется
- **Тест-якорь:** — (docs); в разделе — пример байткода «до/после»
- **DoD:**
  - в doc 02 есть раздел «TCO сквозь `ensure`»: где хранятся отложенные ensure при `TAILCALL` (cleanup-регистр или список в кадре), порядок выполнения (LIFO сохраняется), что видит `vm.Verify`, что меняется в опкодах (новые опкоды или флаги — явным списком);
  - перечислены инварианты для тест-якорей T-124: хвостовая рекурсия глубиной 10⁶ внутри `trap` с `ensure` не растит стек; ensure выполняются ровно один раз и в порядке LIFO;
  - строка 306 обновлена ссылкой на новый раздел.
- **НЕ делать:** менять код; менять норматив §10.3/§15.3 (принцип #11 остаётся гарантией минимума); проектировать TCO для тела `trap` без `ensure` (там мешает `MAKEOK`, это другой вопрос).

### T-124 · TCO сквозь `ensure`: реализация
<!-- meta
priority: P3
type: feature
effort: L
model: opus
wave: 9-should
depends_on: T-123
findings: —
-->
- **Файлы:** по схеме T-123: `internal/compiler/compiler.go:2087` (`compileTrapWithEnsure`), `:2199` (`compileTrapBodyWithEnsures`), проверка `trapDepth` у `TAILCALL` (T-31); `internal/vm/vm.go`, `internal/vm/verify.go`
- **Тест-якорь:** создать `TestTailCallThroughEnsureDeepRecursion` (глубина 10⁶), `TestEnsureOrderWithTailCall`; `TestAuditNoTailCallInsideTrap` обновить по схеме T-123
- **DoD:**
  - оба новых теста зелёные; стек при рекурсии 10⁶ не растёт (проверка по числу кадров, а не по времени);
  - ensure выполняются ровно один раз и в порядке LIFO, в том числе при `raise` из хвостового вызова;
  - `make update-bytecode` — прогнать, diff просмотреть глазами; `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** отступать от схемы T-123 без правки doc 02 (стоп, комментарий в issue); TCO для тела `trap` без `ensure`; менять семантику `ensure`.

### T-125 · `Supervisor.start` (§13.1)
<!-- meta
priority: P2
type: feature
effort: L
model: opus
wave: 9-should
depends_on: —
findings: —
-->
- **Файлы:** `internal/vm/prelude*.go` (новый модуль `Supervisor`) или stdlib на Brig (выбор — в body PR); `internal/compiler/compiler.go:1702` (`isPreludeModule`); `internal/vm/scheduler.go` (только если не хватает примитивов — отдельным коммитом)
- **Тест-якорь:** создать `TestSupervisorOneForOneRestart`, `TestSupervisorTransientNormalExit`, `TestSupervisorMaxRestartsEscalates`
- **DoD:**
  - пример §13.1 (`strategy: :one_for_one`, `max_restarts`, `within`, `children` с `restart: :permanent`/`:transient`) запускается;
  - `:permanent` ребёнок перезапускается после любого выхода; `:transient` — только после аварийного (не `:normal`);
  - больше `max_restarts` перезапусков за `within` мс → супервизор завершается аварийно (эскалация); тест детерминирован (без реальных часов — через `after` в рантайме или инжекцию времени);
  - супервизор — обычный актор, который делает `watch` детям (§13.1); `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** стратегии кроме `:one_for_one` (спека других не называет — issue, если нужно); распределённые MFA-дескрипторы; `Behavior` (T-126); менять семантику `watch`/`:down`.

### T-126 · `Behavior` и `spawn_behavior` (§13.2)
<!-- meta
priority: P3
type: feature
effort: M
model: opus
wave: 9-should
depends_on: T-108, T-117
findings: —
-->
- **Файлы:** stdlib: `type Behavior { handlers: Map<Atom, Function> }`; примитив `spawn_behavior` (не в прелюдии, §13.2); `internal/compiler/compiler.go` (регистрация примитива), `internal/vm/scheduler.go`
- **Тест-якорь:** создать `TestBehaviorDispatch` (пример §13.2 после T-117), `TestBehaviorUnknownTag`
- **DoD:**
  - пример §13.2 (kv с `:get`/`:put`) исполняется: `send(pid, (:put, k, v))`, затем `(:get, k)` → `(:ok, v)`;
  - диспетч идёт по тегу верхнего уровня; неизвестный тег → поведение, записанное в body PR (если спека молчит — стоп, комментарий в issue);
  - `BRIG_VERIFY=1 go test ./...` → 0; `make all` → 0.
- **НЕ делать:** валидировать колбэки (вне MVP, §13.2); добавлять `spawn_behavior` в прелюдию; менять `Supervisor` (T-125).
