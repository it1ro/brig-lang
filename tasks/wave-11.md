# Wave 11 — укрепление и производительность

[← карта плана](README.md)

**Откуда:**
- замеры heap (PR #174, `web-mvp-research/23-heap-measurements.md`) и
  issues, найденные ими, — #171, #172, #173;
- research 19 (бенчмарки как метрика);
- задачи «укрепления» из PR #167, там это была Wave 8.

**Вход:**
- T-102 и T-103 от других задач не зависят. Их можно брать сразу после
  Wave 7, параллельно волнам 8–10: они правят `scheduler.go`, а не
  компилятор;
- T-150 и T-154 ждут Wave 9 и 10;
- T-153 ждёт merge PR #174 (стенд `bench-heap`).

**Зачем:** research показал, что производительность и память упираются в
три вещи:
- представление `Value` (296 Б);
- аллокацию регистров;
- таймеры.

Это нужно решить до рантайм-механизмов Wave 12: `await`, `Timer`,
100k акторов-соединений. Заодно контур «производительность → решения»
становится постоянным: бенчмарки в каждом PR.

**Выход:**
- `make bench` в CI с порогом регрессии;
- #171 и #172 закрыты, решение по #173 принято;
- Z2–Z4 повторены после них;
- третий аудит проведён на свежем `main`.

## Порядок и параллельность

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-152 [#274](https://github.com/it1ro/brig-lang/issues/274) | `make bench` и порог регрессий в CI | — | sonnet | medium | всё |
| 2 | T-102 | [#171](https://github.com/it1ro/brig-lang/issues/171) VM: куча таймеров | — | opus | medium | T-104, T-152 |
| 3 | T-103 | [#172](https://github.com/it1ro/brig-lang/issues/172) VM: регистры кадра без аллокации на вызов | — | opus | medium | T-104, T-152 |
| 4 | T-104 | [#173](https://github.com/it1ro/brig-lang/issues/173) DD: компактное представление `runtime.Value` | — | human | large | всё |
| 5 | T-153 [#275](https://github.com/it1ro/brig-lang/issues/275) | Повтор замеров Z2–Z4 после #172/#173 | T-103, T-104, T-152 | opus | medium | — |
| 6 | T-150 [#272](https://github.com/it1ro/brig-lang/issues/272) | Ревизия оставшихся «срез:» | T-139, T-144 | sonnet | low | T-151 |
| 7 | T-151 [#273](https://github.com/it1ro/brig-lang/issues/273) | Docs: пример §13.2 — снять `text`, пометить `brig` | T-138 | sonnet | low | T-150 |
| 8 | T-154 [#276](https://github.com/it1ro/brig-lang/issues/276) | Третий аудит на свежем `main` | T-139, T-146, T-147 | opus | large | — |
| 9 | T-155 [#209](https://github.com/it1ro/brig-lang/issues/209) | Спред `..` в List/Vector/Map и в любой позиции вызова (§5.2) | — | opus | medium | T-150 |

T-102 и T-103 обе правят `internal/vm/scheduler.go` — по одной.

## Задачи

Все задачи волны заведены на доске: DoD — в issues по ссылкам из таблицы.
