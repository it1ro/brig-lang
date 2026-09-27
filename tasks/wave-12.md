# Wave 12 — рантайм-механизмы

[← карта плана](README.md)

**Откуда:**
- research, фаза 2 (`web-mvp-research/02-runtime.md` R1, R3–R8, R12, R13;
  `11-memory.md` M1–M2);
- решения Q-await, Q-reg, Q-exit (07).

**Вход:**
- Wave 8 закрыта: T-122 отнёс эти решения к отдельному пакету спеки,
  он здесь (T-160…T-162);
- для T-166 — T-102 (#171, куча таймеров).

**Зачем:** без этих механизмов не пишутся `Supervisor`, `Server.call`,
любой синхронный клиент (главная находка research — R12) и вообще
любой I/O. Всё это — механизмы VM, а политика (супервизоры, сервер)
пишется на Brig в Wave 13. Спека правится до кода: примитивы акторов —
нормативная часть §12 и §15.

**Выход:**
- `exit`, `spawn_watched`, `register`/`whereis`, `await`/`reply`,
  `Timer`, `Time`, `Global` работают и описаны в спеке;
- run-loop ждёт внешние события и не завершается, пока есть живые
  подписки;
- первый порт `Signal` доставляет SIGTERM актору;
- `corpus/lookout/lib/main.brig` и чекер доходят до уровня `check`.

## Порядок и параллельность

Docs-задачи T-160…T-162 правят `docs/01-language-design.md` §12 и §15 —
по одной. Код T-163…T-169 почти весь в `internal/vm/scheduler.go` — по
одной, кроме T-167 (`Global`, отдельный файл) и T-169.

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-160 [#277](https://github.com/it1ro/brig-lang/issues/277) | Спека: `exit`, `spawn_watched`, реестр, `await`/`reply`, бюджет хода | T-122 | opus | medium | — |
| 2 | T-161 [#278](https://github.com/it1ro/brig-lang/issues/278) | Спека: `Timer`, `Time`, `Global` | T-122 | sonnet | low | T-163…T-165 |
| 3 | T-162 [#279](https://github.com/it1ro/brig-lang/issues/279) | Спека: внешние события, G5, условие завершения | T-122 | opus | medium | T-163…T-165 |
| 4 | T-163 [#280](https://github.com/it1ro/brig-lang/issues/280) | `exit(pid, reason)` и `spawn_watched` | T-160 | opus | medium | T-167 |
| 5 | T-164 [#281](https://github.com/it1ro/brig-lang/issues/281) | Реестр имён `register`/`whereis` | T-160 | sonnet | medium | T-167 |
| 6 | T-165 [#282](https://github.com/it1ro/brig-lang/issues/282) | `await(ref, timeout)` и `reply` — ref-слоты | T-102, T-160 | opus | large | T-167 |
| 7 | T-166 [#283](https://github.com/it1ro/brig-lang/issues/283) | `Timer.send_after`/`cancel`, `Time.monotonic_ms`/`now` | T-102, T-161 | sonnet | medium | T-167 |
| 8 | T-167 [#284](https://github.com/it1ro/brig-lang/issues/284) | `Global.put`/`get` | T-161 | sonnet | low | всё |
| 9 | T-168 [#285](https://github.com/it1ro/brig-lang/issues/285) | Внешние события в run-loop и порт `Signal` | T-162, T-163 | opus | large | — |
| 10 | T-169 [#286](https://github.com/it1ro/brig-lang/issues/286) | Бюджет хода и `Actor.info` (M1, M2) | T-163 | opus | medium | T-167 |

## Задачи

Все задачи волны заведены на доске: DoD — в issues по ссылкам из таблицы.
