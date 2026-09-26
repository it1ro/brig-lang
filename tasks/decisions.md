# Design decisions

[← карта плана](README.md)

Вопросы к автору языка: label `design-decision`, модель `human`. Код по вопросу не пишется, пока нет решения. DoD любой DD-задачи — одно и то же: в issue записан выбранный вариант (для каждого подвопроса) и судьба задач, которые ждут решения (делается / won't-fix); issue закрыт. Правку спеки по решению делает отдельная docs-задача.

DD, которые блокируют конкретную волну, лежат в файле этой волны: T-100, T-102, T-107, T-109 — [wave-7.md](wave-7.md); T-121, T-122 — [wave-9.md](wave-9.md).

## Решённые (волны 0–6)

| T-NN | Issue | Вопрос | Решение | Что разблокировал |
|---|---|---|---|---|
| T-90 | [#40](https://github.com/it1ro/brig-lang/issues/40) | Модель планировщика: «1 актор = 1 goroutine» (A-F1) | **C** — спека фиксирует только наблюдаемые гарантии (G1–G4), модель потоков — свобода реализации | T-80, T-48 |
| T-91 | [#41](https://github.com/it1ro/brig-lang/issues/41) | Truthiness или строгий Bool (A-F3) | **A** — строгий Bool везде, правый операнд `and`/`or` проверяется и не хвостовой | T-81; T-82 → won't-fix |
| T-92 | [#42](https://github.com/it1ro/brig-lang/issues/42) | Ловится ли `:type_error` (A-F4) | **A** — все `:type_error` ловимы (K-3 в doc 02) | T-83, T-96 |
| T-93 | [#43](https://github.com/it1ro/brig-lang/issues/43) | Равенство чисел разных видов (I-F8) | **A** для всех трёх мест: паттерны и ключи — строго по виду; `==`/`<` — точно; Decimal×Float → `:type_error` | T-84, T-85, T-86 |
| T-94 | [#55](https://github.com/it1ro/brig-lang/issues/55) | §12.4 vs `after_clause` (инлайн `after`) | вариант 1 (см. issue) | T-60 |
| T-95 | [#129](https://github.com/it1ro/brig-lang/issues/129) | Порядок bind/stmt в `with` (§8.2 vs `brig.ebnf`) | **A** — binds только в заголовке, чередование запрещено | T-87, T-88 |

## Открытые вопросы §17 (T-130…)

Эти вопросы не блокируют волны 7–8. Issues не заведены. Остальные пункты §17 (п.3 кросс-нодовые типы, п.4 версии записей при горячей перезагрузке, п.5 пакетный менеджер, п.8 блокирующие операции и N:M, п.10 `SendError`) — горизонт после Should; номеров пока не получают.

### T-130 · DD: где хранить stack trace при `raise` (§17 п.1)
<!-- meta
priority: P3
type: design-decision
effort: S
model: human
wave: —
depends_on: —
findings: —
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §17 п.1, §10.2; `internal/vm/scheduler.go` (печать trace, T-79)
- **Тест-якорь:** — (решение)
- **Вопрос:** stack trace непойманного `raise` сейчас печатает CLI (T-79), но в значение ошибки он не попадает. Нужен ли trace в самом значении — например, для `:down` или логера?
- **Варианты:** **A:** отдельный debug-канал, значение ошибки не меняется (как сейчас). **B:** trace — часть значения, `raise` оборачивает payload в запись; это ломает паттерны `Error(:x)`. **C:** trace доступен только через `trap` с опцией, по умолчанию его нет.
- **DoD:** см. шапку файла.
- **НЕ делать:** менять формат `:down`; писать код до решения; решать заодно `trace(pid)` (T-122).

### T-131 · DD: `trap(fn, timeout: N)` (§17 п.2)
<!-- meta
priority: P3
type: design-decision
effort: S
model: human
wave: —
depends_on: —
findings: —
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §10.2, §17 п.2
- **Тест-якорь:** — (решение)
- **Вопрос:** нужен ли `trap` с таймаутом? Если да — какой синтаксис (зависит от kwargs, T-121) и какой вид ошибки (`(:timeout, ms)`?).
- **Варианты:** **A:** не вводить: таймауты — только в `recv … after`. **B:** ввести после kwargs как `trap(fn, timeout: N)`. **C:** ввести как отдельную функцию прелюдии `trap_timeout(fn, ms)`.
- **DoD:** см. шапку файла.
- **НЕ делать:** писать код; менять `recv … after`; решать kwargs здесь (это T-121).

### T-132 · DD: `type Color {}` без полей (§17 п.7)
<!-- meta
priority: P2
type: design-decision
effort: S
model: human
wave: —
depends_on: —
findings: —
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §14.1, §17 п.7; `brig.ebnf` (`type_body`)
- **Тест-якорь:** — (решение)
- **Вопрос:** что значит `type Color {}`: номинальный маркер, алиас Unit или ошибку?
- **Варианты:** **A:** ошибка парсинга — пустое тело запрещено. **B:** номинальный маркер: значение `Color` без полей, равенство по имени типа. **C:** алиас для `()`.
- **DoD:** см. шапку файла; в решении указано, влияет ли оно на T-103.
- **НЕ делать:** писать код; менять T-103 до решения; трогать алиасы `type X = Y`.

### T-133 · DD: or-паттерны `:ok | :error` (§17 п.11, Nice)
<!-- meta
priority: P3
type: design-decision
effort: S
model: human
wave: —
depends_on: —
findings: —
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §9, §16 (Nice), §17 п.11; `brig.ebnf` (`pattern`)
- **Тест-якорь:** — (решение)
- **Вопрос:** вводить ли or-паттерны, и если да — где они разрешены и могут ли альтернативы связывать имена?
- **Варианты:** **A:** не вводить (Nice остаётся Nice). **B:** только без связываний (`:ok | :error ->`). **C:** со связываниями, но одинаковым набором имён во всех альтернативах.
- **DoD:** см. шапку файла.
- **НЕ делать:** писать код; менять лексер (`|` сейчас не токен); решать заодно guard'ы.

### T-134 · DD: движок `Regex` для `rx"..."` (§3.3)
<!-- meta
priority: P3
type: design-decision
effort: S
model: human
wave: —
depends_on: —
findings: —
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §3.3; `internal/compiler/compiler.go:1099` (`срез: regex не реализован`)
- **Тест-якорь:** — (решение)
- **Вопрос:** какой движок и какой диалект у `rx"..."`? От этого зависит, что делает литерал: компиляция при загрузке или в рантайме, ошибка — compile-time или `raise`.
- **Варианты:** **A:** Go `regexp` (RE2): линейное время, без backreferences. **B:** PCRE-совместимый сторонний движок. **C:** отложить `rx"..."` до Nice, литерал оставить fail-fast.
- **DoD:** см. шапку файла; при A или B в решении перечислены функции прелюдии (`Regex.match?` и т.п.).
- **НЕ делать:** писать код; трогать `internal/compiler` до решения (skill `brig-overview`: Regex отложен).

### T-135 · DD: литерал `Set` (§17 п.6, Nice)
<!-- meta
priority: P3
type: design-decision
effort: S
model: human
wave: —
depends_on: —
findings: —
extra_labels: design-decision
-->
- **Файлы:** `docs/01-language-design.md` §4.6, §16 (Nice), §17 п.6
- **Тест-якорь:** — (решение)
- **Вопрос:** нужен ли отдельный литерал `Set` помимо конструктора `set(...)`? Принцип §0 п.2 «один способ» говорит против.
- **Варианты:** **A:** не вводить, закрыть п.6 §17. **B:** ввести `%s{...}` или похожий. **C:** отложить.
- **DoD:** см. шапку файла.
- **НЕ делать:** писать код; менять `set(...)`.
