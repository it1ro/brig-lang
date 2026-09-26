# Wave 5 — docs cleanup

[← карта плана](README.md)

Вход: T-12; для T-61 — все волны 0–4. Выход: §8 и §5 аудита закрыты, `TASKS.md` и `AUDIT_REPORT.md` связаны с issues. T-60 переоткрыт 2026-09-26: его PR #52 закрыт без merge. T-62 ждёт design decisions, T-63 синхронизирует план с доской.

T-65 (этот каталог) заменил корневой `TASKS.md`.

## Задачи

| T-NN | Issue | Название | depends_on | Источник |
|---|---|---|---|---|
| T-60 | [#38](https://github.com/it1ro/brig-lang/issues/38) | Docs: §8 и §5 аудита, пробелы вне K-8 | T-12 | audit |
| T-61 | [#39](https://github.com/it1ro/brig-lang/issues/39) | Закрыть TASKS.md и AUDIT_REPORT.md ссылками | T-60 | audit |
| T-62 | [#112](https://github.com/it1ro/brig-lang/issues/112) | Docs: записать решения DD #41–#43 в doc 02 и спеку | T-91, T-92, T-93 | audit |
| T-63 | [#113](https://github.com/it1ro/brig-lang/issues/113) | Docs: актуализировать TASKS.md, удалить STATUS.md, правила для spec-gap | T-61 | audit |
| T-64 | [#143](https://github.com/it1ro/brig-lang/issues/143) | Docs: ужать TASKS.md до карты плана | T-63 | audit |
| T-87 | [#131](https://github.com/it1ro/brig-lang/issues/131) | Docs: with — binds только в начале (решение T-95) | T-95 | audit |
| T-65 | [#166](https://github.com/it1ro/brig-lang/issues/166) | Docs: разбить TASKS.md на tasks/ по волнам | T-64 | audit |
