# Wave 6 — Must-пробелы §16

[← карта плана](README.md)

Вход: волны 0–4 закрыты. Задачи с label `spec-gap` и Task type `feature` реализуют Must-фичи §16, которые парсятся, но не компилируются или отсутствуют: `match`, `with`, pipe, записи, вариадики, `Sys.args()`/`link`/`mailbox_size()`, term order, формат диагностики, stack trace. Зависимости: T-71 ждёт T-70; T-74 ждёт T-70 и T-73. Почти все задачи волны правят `internal/compiler/compiler.go`, поэтому по `WORKFLOW.md` §7 их не берут параллельно; исключения — T-77 (`runtime.Compare`) и T-79 (scheduler, `cmd/brig`). Выход: `rg -n 'срез: (pipe|record literal|неподдерживаемое выражение)' internal/compiler` пуст, `make check-examples` → `failed 0`.

## Задачи

| T-NN | Issue | Название | depends_on | Источник |
|---|---|---|---|---|
| T-70 | [#114](https://github.com/it1ro/brig-lang/issues/114) | Компиляция match (§8.3) | — | spec-gap |
| T-71 | [#122](https://github.com/it1ro/brig-lang/issues/122) | Компиляция with/else (§8.2) | T-70 | spec-gap |
| T-72 | [#115](https://github.com/it1ro/brig-lang/issues/115) | Pipe |> (§7.5) | — | spec-gap |
| T-73 | [#116](https://github.com/it1ro/brig-lang/issues/116) | Записи: номинальные и анонимные (§4.7) | — | spec-gap |
| T-74 | [#123](https://github.com/it1ro/brig-lang/issues/123) | Record-паттерны (§9.6) | T-73, T-70 | spec-gap |
| T-75 | [#117](https://github.com/it1ro/brig-lang/issues/117) | Прелюдия: Sys.args(), Prelude.*, link, mailbox_size() | — | spec-gap |
| T-76 | [#118](https://github.com/it1ro/brig-lang/issues/118) | Диагностика: формат error: file:line:col (§E) | — | spec-gap |
| T-77 | [#119](https://github.com/it1ro/brig-lang/issues/119) | Term order: тотальный < между видами (§7.4) | — | spec-gap |
| T-78 | [#120](https://github.com/it1ro/brig-lang/issues/120) | Вариадики: клозы разной арности, лямбды, захват (§6.3) | — | spec-gap |
| T-79 | [#121](https://github.com/it1ro/brig-lang/issues/121) | Stack trace для непойманного raise | — | spec-gap |
| T-89 | [#135](https://github.com/it1ro/brig-lang/issues/135) | Term order записей (§7.4) | T-73 | spec-gap |
