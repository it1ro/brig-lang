# Wave 8 — решения: DD и спека

[← карта плана](README.md)

**Откуда:** второй аудит, слои S, G, R ([AUDIT_REPORT-2.md](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT-2.md)).

**Вход:** DD можно решать сразу, параллельно Wave 7. Docs-задачи ждут
своих DD и T-116, потому что после T-116 правки спеки сразу проверяются
компиляцией примеров.

**Зачем:** спека противоречит себе (порядок аргументов, интерполяция,
`mailbox_size`, `fn ->`) и отстаёт от 30+ утверждённых решений research.
Код Wave 9–10 не начинается, пока норма не записана: иначе снова будет
«фактическое поведение, под которое подстроим доку».

**Выход:**
- все DD волны закрыты;
- спека v0.4.8: противоречия S-1…S-12 сняты, решения research из пакета
  T-122 внесены;
- `make check-examples` → `failed 0`, число `pending` не выросло
  относительно T-116, кроме примеров новых фич с номером задачи.

## Порядок и параллельность

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-120 [#179](https://github.com/it1ro/brig-lang/issues/179) | DD: порядок аргументов коллекционных функций | — | human | low | все DD |
| 2 | T-121 [#180](https://github.com/it1ro/brig-lang/issues/180) | DD: развилки спека ↔ реализация (6 пунктов) | — | human | low | все DD |
| 3 | T-122 [#181](https://github.com/it1ro/brig-lang/issues/181) | DD: какие решения research становятся нормой в v0.4.8 | — | human | medium | все DD |
| 4 | T-123 [#182](https://github.com/it1ro/brig-lang/issues/182) | DD: term order пользовательских вариантов | — | human | low | все DD |
| 5 | T-124 [#183](https://github.com/it1ro/brig-lang/issues/183) | DD: якорь offside-блока | — | human | low | все DD |
| 6 | T-125 [#184](https://github.com/it1ro/brig-lang/issues/184) | DD: тест-фреймворк и «заделы горячей перезагрузки» (Must §16) | — | human | low | все DD |
| 7 | T-126 [#185](https://github.com/it1ro/brig-lang/issues/185) | Docs: чистка спеки без смысловых изменений | T-116 | sonnet | medium | DD |
| 8 | T-100 | [#169](https://github.com/it1ro/brig-lang/issues/169) Spec: `fn ->` и `(a, b) ->` (§6.2) | — | sonnet | low | T-126 |
| 9 | T-127 [#186](https://github.com/it1ro/brig-lang/issues/186) | Docs: спека по T-120, T-121, T-124 | T-120, T-121, T-124, T-126 | sonnet | medium | T-128 |
| 10 | T-128 [#187](https://github.com/it1ro/brig-lang/issues/187) | Docs: спека — пакет research A (модули, записи, ошибки, `pub`) | T-122, T-126 | opus | large | T-127 |
| 11 | T-129 [#264](https://github.com/it1ro/brig-lang/issues/264) | Docs: спека — пакет research B (§16, `"""`, `##`) | T-122, T-126 | sonnet | medium | T-127, T-128 |

Все правки спеки идут через `docs/01-language-design.md`: T-126, T-127,
T-128, T-129 и T-100 правят один файл, поэтому их мержат по одной, в
порядке таблицы (T-100 можно раньше — у неё свои разделы).

## Задачи

Все задачи волны заведены на доске: DoD — в issues по ссылкам из таблицы.
