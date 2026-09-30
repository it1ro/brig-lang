# Design decisions

[← карта плана](README.md)

Вопросы к автору языка: label `design-decision`, модель `human`. Код по вопросу не пишется, пока нет решения. DoD любой DD-задачи — одно и то же: в issue записан выбранный вариант (для каждого подвопроса) и судьба задач, которые ждут решения (делается / won't-fix); issue закрыт. Правку спеки по решению делает отдельная docs-задача.

DD, которые блокируют конкретную волну, лежат в файле этой волны:
- T-120…T-125 — [wave-8.md](wave-8.md) (решены 2026-09-27, таблица ниже);
- T-104 ([#173](https://github.com/it1ro/brig-lang/issues/173)) — [wave-11.md](wave-11.md);
- T-174 — [wave-13.md](wave-13.md);
- T-250…T-259 (третий аудит) — ниже, раздел «DD третьего аудита»: решены 2026-09-30, решение записано в body issue.

Этот файл хранит решённые DD и открытые вопросы §17, которые не
блокируют ни одну волну.

## Решённые

| T-NN | Issue | Вопрос | Решение | Что разблокировал |
|---|---|---|---|---|
| T-90 | [#40](https://github.com/it1ro/brig-lang/issues/40) | Модель планировщика: «1 актор = 1 goroutine» (A-F1) | **C** — спека фиксирует только наблюдаемые гарантии (G1–G4), модель потоков — свобода реализации | T-80, T-48 |
| T-91 | [#41](https://github.com/it1ro/brig-lang/issues/41) | Truthiness или строгий Bool (A-F3) | **A** — строгий Bool везде, правый операнд `and`/`or` проверяется и не хвостовой | T-81; T-82 → won't-fix |
| T-92 | [#42](https://github.com/it1ro/brig-lang/issues/42) | Ловится ли `:type_error` (A-F4) | **A** — все `:type_error` ловимы (K-3 в doc 02) | T-83, T-96 |
| T-93 | [#43](https://github.com/it1ro/brig-lang/issues/43) | Равенство чисел разных видов (I-F8) | **A** для всех трёх мест: паттерны и ключи — строго по виду; `==`/`<` — точно; Decimal×Float → `:type_error` | T-84, T-85, T-86 |
| T-94 | [#55](https://github.com/it1ro/brig-lang/issues/55) | §12.4 vs `after_clause` (инлайн `after`) | вариант 1 (см. issue) | T-60 |
| T-95 | [#129](https://github.com/it1ro/brig-lang/issues/129) | Порядок bind/stmt в `with` (§8.2 vs `brig.ebnf`) | **A** — binds только в заголовке, чередование запрещено | T-87, T-88 |
| T-120 | [#179](https://github.com/it1ro/brig-lang/issues/179) | Порядок аргументов коллекционных функций (S-1) | **A** — коллекция первой; первый аргумент stdlib — субъект pipe | T-127, T-130 |
| T-121 | [#180](https://github.com/it1ro/brig-lang/issues/180) | Развилки спека ↔ реализация (7 пунктов) | guard — проброс ошибки; `mailbox_size` → `Int`; `Str + Str` запрещён; `(:no_field, …)`; `if` в интерполяции разрешён; `_name` — wildcard; `Range` в JSON — ошибка | T-127, T-131, T-132; T-175 → won't-fix |
| T-122 | [#181](https://github.com/it1ro/brig-lang/issues/181) | Решения research в спеке v0.4.8 | пакеты A и B как предложено; `pub fn` сразу; циклы импорта разрешены; тип = последний сегмент + квалификация; `Mod.f` — значение | T-128, T-129, T-135, T-137, T-143, T-144 |
| T-123 | [#182](https://github.com/it1ro/brig-lang/issues/182) | Term order пользовательских вариантов | **A** — как номинальные записи | T-136 |
| T-124 | [#183](https://github.com/it1ro/brig-lang/issues/183) | Якорь offside-блока | **A** — отступ строки стейтмента везде | T-127, T-138 |
| T-125 | [#184](https://github.com/it1ro/brig-lang/issues/184) | Тест-фреймворк и горячая перезагрузка (Must) | `brig test` + `Test.*` + доктесты; hot reload — из Must | T-147, T-149, T-129 |

## Открытые вопросы §17 (T-190…)

Вопросы не блокируют волны 7–13. Issues заведены, DoD и варианты — в них. До второго аудита
у них были номера T-130…T-135: эти номера заняла Wave 9, поэтому вопросы
перенесены в десяток T-190.

У части вопросов §17 уже есть ответ в research (`web-mvp-research/07`),
он указан в issue как рекомендация. DD только утверждает его или
отвергает.

Остальные пункты §17:
- п.3 (кросс-нодовые типы), п.4 (версии записей) — горизонт; research 16
  предлагает для п.4 контракт и хук `upgrade/1` в `Term`;
- п.5 (пакетный менеджер) — решён research Q-pkg и Q-manifest, в спеку
  попадёт с реализацией пакетов (горизонт);
- п.8 (блокирующие операции, N:M) — решён research Q-sched и R15 (горизонт);
- п.9 (`call`) — решён research Q-await, реализация — T-165 и T-170;
  `trace(pid)` — T-174;
- п.10 (`SendError`) — без изменений.

| T-NN | Issue | Вопрос |
|---|---|---|
| T-190 | [#294](https://github.com/it1ro/brig-lang/issues/294) | где хранить stack trace при `raise` (§17 п.1) |
| T-191 | [#295](https://github.com/it1ro/brig-lang/issues/295) | `trap(fn, timeout: N)` (§17 п.2) |
| T-192 | [#296](https://github.com/it1ro/brig-lang/issues/296) | `type Color {}` без полей (§17 п.7) |
| T-193 | [#297](https://github.com/it1ro/brig-lang/issues/297) | or-паттерны `:ok \| :error` (§17 п.11, Nice) |
| T-194 | [#298](https://github.com/it1ro/brig-lang/issues/298) | движок `Regex` для `rx"..."` (§3.3) |
| T-195 | [#299](https://github.com/it1ro/brig-lang/issues/299) | литерал `Set` (§17 п.6, Nice) |

## DD третьего аудита (T-250…T-259)

Заведены по [AUDIT_REPORT-3.md](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT-3.md) (раздел 5), решены
2026-09-30. Решение и реализация — в одном issue (раздел «Решение» в
body), issues — sub-issues эпика [#276](https://github.com/it1ro/brig-lang/issues/276).
Бывшие задачи реализации T-262…T-264, T-266…T-268, T-270, T-274, T-275
поглощены своими DD; T-259 — в T-258.

| T-NN | Issue | Вопрос | Решение | Milestone |
|---|---|---|---|---|
| T-250 | [#397](https://github.com/it1ro/brig-lang/issues/397) | Представление коллекций | **A без T-104**: `List` cons, `Map`/`Set` HAMT, `Vector` trie; шаг 0 — T-283; T-104 отдельно | M2 |
| T-251 | [#404](https://github.com/it1ro/brig-lang/issues/404) | Политика ящика | **A без info sema**: HWM 10 000, `mailbox_hwm`, событие `[:vm, :mailbox, :hwm]`, `dropped` в `Actor.info` | M3 |
| T-252 | [#405](https://github.com/it1ro/brig-lang/issues/405) | Смысл `link` | **A**: владелец → ребёнок, `exit(child, (:linked_exit, reason))` | M3 |
| T-253 | [#406](https://github.com/it1ro/brig-lang/issues/406) | `else if` | **`cond`** вместо `else if`; нет истинной ветки — `raise((:cond_clause, ()))`, без `true ->` — info | M4 |
| T-254 | [#407](https://github.com/it1ro/brig-lang/issues/407) | Term order `List`/`Vector` | **A**: лексикографически | M4 |
| T-255 | [#408](https://github.com/it1ro/brig-lang/issues/408) | Продолжение по `+`/`-` | **A**: убрать из списка продолжений | M4 |
| T-256 | [#409](https://github.com/it1ro/brig-lang/issues/409) | JSON | **A**: `Result<V, (:json, reason)>`, `null` ↔ `None` | M4 |
| T-257 | [#410](https://github.com/it1ro/brig-lang/issues/410) | Дубли §0.2 | **Модуль `Enum`**; голые `map/filter/fold/find/all/any`, `List.*`-дубли, `Vec.len` удаляются (T-284); `to_int` — только числа | M4 |
| T-258 | [#418](https://github.com/it1ro/brig-lang/issues/418) | §0.11 TCO сквозь `ensure`; структура и версия спеки (бывшая T-259) | **A**: гарантия, §16 Must; **A**: история в `docs/spec-history.md`, версия `v0.5.N` | M5 |
