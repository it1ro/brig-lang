# Wave 15 — observer и TUI

[← карта плана](README.md)

**Откуда:**
- research: `web-mvp-research/05-toolchain.md` (C7, `brig observe`: живое
  дерево акторов, ящики, счётчики, TUI и веб-вид), `16-infrastructure.md`
  §3 (`Telemetry`, события VM), `11-memory.md` (M2, `Actor.info`),
  `20-ecosystem.md` (Q-crash);
- ревизия доски 2026-09-28: фокус — core brig, `Supervisor`, observer и
  TUI; всё, что к ним не относится, перенесено в Backlog.

**Решения (автор языка, 2026-09-28):**
1. Observer v0 — хелперы консоли `brig -i` (`tree()`, `info(pid)`,
   `top(n)`, `observe()`) внутри процесса приложения. Подключения к чужому
   процессу по сокету в волне нет.
2. TUI — на Go в тулчейне поверх `golang.org/x/term`, как редактор строки
   (T-202) и подсветка (T-203). TUI-библиотека на Brig ждёт портов
   `Stdin`/`Term` и в волну не входит.
3. Первая версия `Telemetry` — минимальная: `emit`/`attach`/`detach` и
   события VM (`[:vm, :spawn]`, `[:vm, :actor, :down]`,
   `[:vm, :actor, :crash]`, `[:vm, :mailbox, :hwm]`). Observer строится на
   ней и на опросе `Actor.list`/`Actor.info`.

**Вход:**
- T-160 (#277), T-161 (#278) — спека акторных примитивов, `Timer`, `Global`;
- T-163 (#280) `exit`, T-164 (#281) реестр, T-167 (#284) `Global`,
  T-169 (#286) `Actor.info` и бюджет хода;
- T-170 (#287) `Supervisor` — для дерева акторов;
- T-209 (#247) `brig -i` — консоль, в которой живут хелперы.

**Выход:**
- в `brig -i .` при работающем приложении `tree()` показывает дерево
  супервизоров и акторов с именами, статусами и ящиками, `top(n)` — самых
  нагруженных;
- `observe()` открывает полноэкранный вид с обновлением, выбором актора и
  лентой падений; выход возвращает в REPL, приложение не останавливается;
- падения и `:down` доступны подписчикам `Telemetry`.

**Вне волны:** `brig observe` к работающему процессу по сокету (ставится
на remote console, см. `wave-14.md`); веб-вид; `Telemetry.span`, экспорт
метрик и отчёты о падениях; TUI-библиотека на Brig; `state_size_estimate`.

## Порядок и параллельность

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-220 [#312](https://github.com/it1ro/brig-lang/issues/312) | Спека: интроспекция акторов, `Telemetry`, хелперы observer | T-160, T-161 | opus | medium | — |
| 2 | T-221 [#313](https://github.com/it1ro/brig-lang/issues/313) | `Actor.list`, поля `Actor.info` и снимок планировщика | T-220, T-164, T-169 | opus | medium | T-222 |
| 3 | T-222 [#314](https://github.com/it1ro/brig-lang/issues/314) | Минимальная `Telemetry` и события VM | T-220, T-163, T-167 | opus | medium | T-221, T-223 |
| 4 | T-223 [#315](https://github.com/it1ro/brig-lang/issues/315) | Observer v0: `tree()`, `info(pid)`, `top(n)` в `brig -i` | T-209, T-221, T-170 | opus | medium | T-222 |
| 5 | T-224 [#316](https://github.com/it1ro/brig-lang/issues/316) | TUI observer на Go: `observe()` в консоли | T-221, T-222, T-223 | opus | large | — |

## Задачи

Все задачи волны заведены на доске: DoD, файлы и «НЕ делать» — в issues по ссылкам из таблицы.
