# Wave 13 — Should §16 и политика на Brig

[← карта плана](README.md)

**Откуда:**
- Should-задачи из PR #167 (там это была Wave 9) — перенумерованы;
- решения research: `Supervisor` и `Server` — на Brig поверх механизмов
  VM (02, «Supervisor — механизм в ядре, политика в библиотеке»); L16,
  L17.

**Вход:**
- для T-170 — механизмы Wave 12 (T-163, T-165) и stdlib на Brig (T-146);
- для T-171 — мини-блоки (T-138) и пример §13.2 (T-151);
- остальные задачи от Wave 12 не зависят.

**Зачем:** первая настоящая библиотечная политика на Brig — `Supervisor`
и `Server.call`. Это самый сильный тест контура «язык ↔ библиотеки»:
если на них неудобно писать, это сразу видно по коду stdlib, а не по
жалобам пользователей.

**Выход:**
- пример §13.1 исполняется;
- `Server.call` синхронен для вызывающего и не трогает его ящик;
- `corpus/lookout/lib/lookout/application.brig` и `monitors/*` доходят до
  уровня `check`; то, что ждёт HTTP и SQL, помечено `needs` горизонта.

## Порядок и параллельность

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-170 [#287](https://github.com/it1ro/brig-lang/issues/287) | `Supervisor` и `Server` на Brig | T-146, T-163, T-164, T-165 | opus | large | T-172, T-174…T-177 |
| 2 | T-171 [#288](https://github.com/it1ro/brig-lang/issues/288) | `Behavior` и `spawn_behavior` (§13.2) | T-138, T-151 | opus | medium | T-172, T-174…T-177 |
| 3 | T-172 [#289](https://github.com/it1ro/brig-lang/issues/289) | Docs: схема TCO сквозь `ensure` в doc 02 | — | opus | medium | всё |
| 4 | T-173 [#290](https://github.com/it1ro/brig-lang/issues/290) | TCO сквозь `ensure`: реализация | T-172 | opus | large | T-174…T-177 |
| 5 | T-174 [#291](https://github.com/it1ro/brig-lang/issues/291) | DD: `trace(pid)` и логер (§17 п.9) | — | human | low | всё |
| 6 | T-175 | `Range` в JSON по решению T-121 п.7 | T-121 | sonnet | low | всё |
| 7 | T-176 [#292](https://github.com/it1ro/brig-lang/issues/292) | `Json.at`, `Map.get_or` (L16) | T-146 | sonnet | low | всё |
| 8 | T-177 [#293](https://github.com/it1ro/brig-lang/issues/293) | sema: info-диагностика `_ <-` с `Result` (L17) | T-146 | sonnet | low | всё |

## Задачи

Задачи со ссылкой на issue в таблице выше заведены на доске: DoD — в issue. T-175 не заведена: по решению T-121 (#180) она won't-fix (`decisions.md`), блок оставлен для истории.

### T-175 · `Range` в JSON по решению T-121 п.7
<!-- meta
priority: P3
type: full-fix
effort: low
model: sonnet
wave: 13
depends_on: T-121
findings: — (Part II N10; остаток T-121 из PR #167)
-->
- **Файлы:** `internal/runtime/json.go`; `docs/01-language-design.md` Part II N10 и §16 — строка «сериализация `Range`» (по решению)
- **Тест-якорь:** создать `TestJsonRangePolicy`
- **DoD:**
  - поведение `Json.encode(1 to 3)` — по T-121 п.7; при варианте «оставить ошибку» задача закрывается как won't-fix правкой N10 и §16 (Should → «не делается», `Term` сериализует `Range`);
  - `make all` → 0.
- **НЕ делать:** `Term` (горизонт); `Json.decode` в `Range`.
