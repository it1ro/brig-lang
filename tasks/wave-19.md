# Wave 19 — библиотеки: справочник и недостающие функции (третий аудит)

[← карта плана](README.md)

**Откуда:** третий аудит ([AUDIT_REPORT-3.md](../AUDIT_REPORT-3.md)),
S-22 и журнал трения dogfooding (#7, #9, #11–#13).

**Вход:**
- T-240 (Wave 16) — спека без дрейфа, сверка операторов;
- T-267 (Wave 17) — решение по дублям §0.2, чтобы новые функции не
  повторяли удалённые;
- T-272 (Wave 18) — `Map` на HAMT: новые `Map.*` сразу на новом
  представлении.

**Зачем:** около 35 публичных функций встроенных и stdlib-модулей не описаны в
спеке (S-22), а в программах аудита 7 из 23 строк журнала трения — «нет
функции, написал свою»: `zip`, `with_index`, `sort_by`, `group_by`,
`Map.to_list`/`filter`, форматирование чисел и строк.

**Выход:**
- у каждой публичной функции встроенных и stdlib-модулей есть строка
  таблицы в спеке, `make spec-tables` сверяет;
- программы `corpus/apps` (T-249) переписаны без самописных `zip`,
  `index`, `unwrap`, `pad`, `money`.

## Порядок и параллельность

T-281 и T-282 правят разные файлы (`stdlib/list.brig` и
`internal/vm/prelude.go`/`stdlib/`), но обе — таблицы спеки T-280:
мержить по одной.

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-280 | Docs: справочник встроенных и stdlib-модулей + сверка | T-240, T-267 | sonnet | medium | — |
| 2 | T-281 | stdlib: функции коллекций | T-280, T-272 | sonnet | medium | T-282 |
| 3 | T-282 | stdlib: числа и строки — форматирование и разбор | T-280 | sonnet | medium | T-281 |

## Задачи

### T-280 · Docs: справочник встроенных и stdlib-модулей + сверка
<!-- meta
priority: P2
type: docs
effort: medium
model: sonnet
wave: 19
depends_on: T-240, T-267
findings: S-22 (AUDIT_REPORT-3)
-->
- **Файлы:** `docs/01-language-design.md` (новый раздел «Встроенные модули»: `Map`, `Vec`, `Json`, `Record`, `List`, `Option`, `Result`, `Server`, `Supervisor`, `Behavior`), `internal/examples/spec_tables_test.go`
- **Тест-якорь:** расширить `TestSpecPreludeMatchesInstall` (или создать `TestSpecModulesMatchCode`): каждая функция `sema.BuiltinModules()` и каждая `pub fn` `stdlib/*.brig` — строка таблицы с той же арностью, и наоборот.
- **DoD:**
  - таблицы в формате §11.5 (имя, арность, аргументы, результат, авто-raise) для всех модулей из «Файлов»; ошибки — формой T-236, если она влита;
  - тест-якорь зелёный, allowlist пуст или каждая запись — с номером задачи;
  - `make check-examples` → `failed 0`; `make all` → 0.
- **НЕ делать:** добавлять функции (T-281, T-282); менять поведение; описывать модули горизонта.

### T-281 · stdlib: функции коллекций
<!-- meta
priority: P2
type: feature
effort: medium
model: sonnet
wave: 19
depends_on: T-280, T-272
findings: журнал трения #7, #8, #9, #11 (AUDIT_REPORT-3)
-->
- **Файлы:** `stdlib/list.brig`, `stdlib/*_test.brig`, `internal/vm/prelude.go` (Go-нативы `Map.*`), `internal/sema` (`BuiltinModules`), `docs/01-language-design.md` (таблицы T-280), `corpus/apps/*` (замена самописных функций)
- **Тест-якорь:** доктесты `##` каждой новой функции (`brig test stdlib`); `TestSpecModulesMatchCode` (T-280).
- **DoD:**
  - добавлены и описаны: `List.zip/2`, `List.with_index/1`, `List.sort_by/2`, `List.group_by/2` (→ `Map`), `List.sum/1`, `List.min/1`, `List.max/1` (`Option`), `Map.to_list/1`, `Map.from_list/1`, `Map.values/1`, `Map.filter/2`, `Map.update/4`; субъект первым (§7.5);
  - у каждой — доктест с успешным вызовом и `raise` на неверном виде аргумента;
  - `corpus/apps/csv` и `corpus/apps/kv` используют их, самописные `zip`, `index`, `unwrap` удалены; `make corpus` → ok;
  - `make bench-scaling` не краснеет; `make all` → 0.
- **НЕ делать:** ленивые `Stream` (DD T-226); функции, удалённые T-267; менять порядок аргументов существующих функций.

### T-282 · stdlib: числа и строки — форматирование и разбор
<!-- meta
priority: P2
type: feature
effort: medium
model: sonnet
wave: 19
depends_on: T-280
findings: журнал трения #12, #13 (AUDIT_REPORT-3)
-->
- **Файлы:** `internal/vm/prelude.go` (Go-нативы), `internal/sema` (`BuiltinModules`), `docs/01-language-design.md` (таблицы `Str`, новый модуль чисел), `corpus/apps/csv`
- **Тест-якорь:** создать `TestFloatFormat`, `TestStrPad` в `internal/vm`; строки таблиц — в `TestSpecModulesMatchCode` (T-280).
- **DoD:**
  - `Float.round(x, digits)` → `Float`; `Float.to_str(x, digits)` → `Str` с ровно `digits` знаками (`Float.to_str(34.456, 2) == "34.46"`); `Str.pad_left(s, width, fill)`, `Str.pad_right(s, width, fill)` (ширина — в кодпоинтах); `Str.to_float(s)` → `Option<Float>`, если T-257 его не добавил;
  - ошибки — `(:type_error, ((:mod, :f), v))` (форма T-236) или текущая, если T-236 не влит;
  - `corpus/apps/csv` без самописных `money`, `two`, `pad`; `make corpus` → ok;
  - `make all` → 0.
- **НЕ делать:** `Instant`/`Date`/`Duration` (горизонт, R-10); локали и CLDR (research C4); `sprintf`-подобный мини-язык форматов.
