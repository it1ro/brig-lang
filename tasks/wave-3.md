# Wave 3 — major fixes

[← карта плана](README.md)

Вход: T-10; для T-37 — T-30. Задачи, завязанные на A-F1/A-F3/A-F4/I-F8, сюда не входят (T-80…T-86). Выход: все задачи волны закрыты, ни одного `t.Skip("blocked: T-3x")` в репо.

T-96…T-98 созданы по ходу работ над T-83 и T-86; T-88 — по решению T-95. Задачи T-80…T-86 ждали DD T-90…T-93 (см. [decisions.md](decisions.md)).

## Задачи

| T-NN | Issue | Название | depends_on | Источник |
|---|---|---|---|---|
| T-30 | [#19](https://github.com/it1ro/brig-lang/issues/19) | Переписать print-only тесты на утверждения | T-10 | audit |
| T-31 | [#20](https://github.com/it1ro/brig-lang/issues/20) | Компилятор: проверка TAILCALL при trapDepth>0 | T-10 | audit |
| T-32 | [#21](https://github.com/it1ro/brig-lang/issues/21) | Компилятор: инвариант I-1 (стек-нейтральность compileExpr) | T-10 | audit |
| T-33 | [#22](https://github.com/it1ro/brig-lang/issues/22) | Компилятор: инвариант I-3 (запись в bound-регистр) | T-10 | audit |
| T-34 | [#23](https://github.com/it1ro/brig-lang/issues/23) | callSync: unwind raise в колбэке прелюдии | T-10 | audit |
| T-35 | [#24](https://github.com/it1ro/brig-lang/issues/24) | Локальные fn: поиск у предков и манглинг в лямбдах | T-10 | audit |
| T-36 | [#25](https://github.com/it1ro/brig-lang/issues/25) | vm.Verify: рёбра MATCHLOCAL и RECVTAKE→after | T-10 | audit |
| T-37 | [#26](https://github.com/it1ro/brig-lang/issues/26) | ensure: область видимости тела и точка регистрации | T-30 | audit |
| T-38 | [#27](https://github.com/it1ro/brig-lang/issues/27) | Fail-fast: локальная fn с захватом | T-10 | audit |
| T-39 | [#28](https://github.com/it1ro/brig-lang/issues/28) | Локальная fn с захватом: полная реализация | T-35, T-38 | audit |
| T-40 | [#29](https://github.com/it1ro/brig-lang/issues/29) | Акторы: удаление мёртвых и значение raise в :down | T-10 | audit |
| T-41 | [#30](https://github.com/it1ro/brig-lang/issues/30) | Позиции 0:0 в bytecode | T-10 | audit |
| T-42 | [#31](https://github.com/it1ro/brig-lang/issues/31) | JSON: маркер $bytes, Inf, Float 1.0 | T-10 | audit |
| T-43 | [#32](https://github.com/it1ro/brig-lang/issues/32) | sema: полная проверка позиции trap | T-10 | audit |
| T-44 | [#50](https://github.com/it1ro/brig-lang/issues/50) | Fail-fast: параметры-паттерны и variadic в лямбдах fn (…) | T-01, T-10 | audit |
| T-45 | [#57](https://github.com/it1ro/brig-lang/issues/57) | exit-коды: классификация ошибок в cmd/brig | T-14 | audit |
| T-46 | [#58](https://github.com/it1ro/brig-lang/issues/58) | runModule прогоняет sema | T-14 | audit |
| T-47 | [#60](https://github.com/it1ro/brig-lang/issues/60) | RECVTIMER: валидация ms | T-15 | audit |
| T-48 | [#61](https://github.com/it1ro/brig-lang/issues/61) | wakeExpired: детерминированный порядок (deadline, seq) | T-15, T-90 | audit |
| T-49 | [#91](https://github.com/it1ro/brig-lang/issues/91) | compileVar: локальная fn предка перекрывает локаль промежуточной функции | T-51 | audit |
| T-55 | [#92](https://github.com/it1ro/brig-lang/issues/92) | vm.Verify: timeout-ребро RECVTAKE и структурные инварианты MATCHLOCAL | T-36 | audit |
| T-56 | [#94](https://github.com/it1ro/brig-lang/issues/94) | Локальная fn: затенение на промежуточном уровне | — | audit |
| T-57 | [#99](https://github.com/it1ro/brig-lang/issues/99) | sema: guard клозов fn не проверяется | — | audit |
| T-58 | [#144](https://github.com/it1ro/brig-lang/issues/144) | Fairness: колбэки прелюдии не тратят редукции (callSync) | — | audit |
| T-80 | [#105](https://github.com/it1ro/brig-lang/issues/105) | Scheduler: привести доки или код к решению A-F1 | T-90, T-13 | audit |
| T-81 | [#106](https://github.com/it1ro/brig-lang/issues/106) | JMPIF/JMPIFNOT: не-Bool → :type_error | T-91 | audit |
| T-82 | [#107](https://github.com/it1ro/brig-lang/issues/107) | Компилятор: правый операнд and/or в хвостовой позиции | T-91, T-10 | audit |
| T-83 | [#108](https://github.com/it1ro/brig-lang/issues/108) | Единая классификация type errors | T-92 | audit |
| T-84 | [#109](https://github.com/it1ro/brig-lang/issues/109) | Паттерны: точное сравнение литералов (MatchEqual) | T-93 | audit |
| T-85 | [#110](https://github.com/it1ro/brig-lang/issues/110) | Точное сравнение Int×Float | T-93 | audit |
| T-86 | [#111](https://github.com/it1ro/brig-lang/issues/111) | Decimal×Float: единое поведение в INDEX, Map/Set, паттернах | T-93 | audit |
| T-88 | [#132](https://github.com/it1ro/brig-lang/issues/132) | Parser: ошибка на bind после тела with (решение T-95) | T-95, T-87 | audit |
| T-96 | [#150](https://github.com/it1ro/brig-lang/issues/150) | Ловимые :type_error/:function_clause в прелюдии, индексации и акторных примитивах | T-83 | audit |
| T-97 | [#158](https://github.com/it1ro/brig-lang/issues/158) | Docs: вернуть решения DD #41–#43 в doc 02 и спеку (после revert T-62) | T-86 | audit |
| T-98 | [#159](https://github.com/it1ro/brig-lang/issues/159) | Decimal-литерал как ключ map-паттерна отвергается компилятором | T-86 | audit |
