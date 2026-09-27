# Wave 9 — язык: остаток Must и модули

[← карта плана](README.md)

**Откуда:**
- задачи «остатка Must» из PR #167 (там они были T-101, T-103…T-106,
  T-108) — перенумерованы и согласованы с research;
- реализация решений Wave 8.

**Вход:**
- Wave 7 закрыта: корпус и компиляция примеров проверяют каждую задачу;
- DD Wave 8, от которых зависит задача, закрыты;
- спека для задачи внесена: T-127 или T-128.

**Выход:**
- примеры спеки §4.1, §6.2, §6.3, §11.1, §14.2 исполняются, их
  `pending` сняты;
- `rg -n 'срез: только простые связывания' internal/compiler` пуст;
- программа из нескольких файлов собирается и запускается;
- `brig check` ловит неизвестные имена;
- в корпусе все файлы `corpus/lang/` на уровне `run`, файлы
  `corpus/lookout/` и `corpus/whelk/` — на уровне `parse` (кроме тех, что
  ждут T-138 или горизонта: у них метка `needs`).

## Порядок и параллельность

`internal/compiler/compiler.go` правят T-130 (частично), T-131, T-133,
T-134, T-136, T-137, T-139 — их мержат по одной (`MAINTAINING.md` §5).
Вне компилятора: T-132 (лексер), T-135 (`cmd/brig`, новый `internal/loader`),
T-138 (лексер, после T-132).

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-130 [#188](https://github.com/it1ro/brig-lang/issues/188) | Прелюдия: порядок аргументов по T-120 | T-120 | sonnet | medium | T-132, T-135 |
| 2 | T-131 [#189](https://github.com/it1ro/brig-lang/issues/189) | Развилки T-121 в VM и компиляторе | T-121 | sonnet | medium | T-132, T-135 |
| 3 | T-132 [#190](https://github.com/it1ro/brig-lang/issues/190) | Лексер: `pub`, `quote`, `_name` | T-121, T-128 | sonnet | low | T-130, T-131, T-135 |
| 4 | T-133 [#191](https://github.com/it1ro/brig-lang/issues/191) | Связывание с паттерном и `(:badmatch, v)` | T-128 | opus | medium | T-135, T-138 |
| 5 | T-134 [#192](https://github.com/it1ro/brig-lang/issues/192) | `alias` и `import` встроенных модулей | T-128 | sonnet | low | T-135, T-138 |
| 6 | T-135 [#193](https://github.com/it1ro/brig-lang/issues/193) | Загрузчик модулей из файлов | T-122, T-128 | opus | medium | T-133, T-134, T-138 |
| 7 | T-136 [#194](https://github.com/it1ro/brig-lang/issues/194) | Пользовательские варианты | T-123, T-128 | opus | medium | T-135, T-138 |
| 8 | T-137 [#195](https://github.com/it1ro/brig-lang/issues/195) | Компиляция программы из нескольких модулей | T-134, T-135, T-136 | opus | large | T-138 |
| 9 | T-138 [#196](https://github.com/it1ro/brig-lang/issues/196) | Лексер: offside-мини-блоки внутри скобок | T-124, T-127, T-132 | opus | large | T-133…T-137 |
| 10 | T-139 [#197](https://github.com/it1ro/brig-lang/issues/197) | `brig check`: неизвестные имена и арность | T-137 | opus | medium | — |

## Задачи

Все задачи волны заведены на доске (milestone `pre-alpha`): DoD, файлы и «НЕ делать» — в issues по ссылкам из таблицы.
