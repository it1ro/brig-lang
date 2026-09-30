# Wave 7 — контуры обратной связи и согласование

[← карта плана](README.md)

**Откуда:** второй аудит ([AUDIT_REPORT-2.md](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT-2.md)), слои P и F.

**Вход:** Wave 6 закрыта. До старта мейнтейнер выполняет действия из
`README.md` («Перед Wave 7»).

**Зачем:** прежде чем менять язык, нужно, чтобы каждое изменение сразу
проверялось на трёх вещах:
- на коде библиотек — корпус;
- на спеке — исполняемые примеры;
- на плане — `plan-check`.

Всё здесь дёшево и не трогает семантику языка.

**Выход:**
- `make all` в CI гоняет `check-examples` (с компиляцией блоков),
  `run-examples` с эталонным выводом, `corpus`, `plan-check` и сверку
  таблиц спеки с кодом;
- fuzz идёт ночью, `-race` — на PR;
- процессные документы не ссылаются на удалённые файлы;
- у каждого файла корпуса есть ожидаемый уровень и список `needs`.

## Порядок и параллельность

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-110 [#258](https://github.com/it1ro/brig-lang/issues/258) | Процессные документы после удаления журнала | — | sonnet | low | всё |
| 2 | T-112 [#260](https://github.com/it1ro/brig-lang/issues/260) | Хук commit-msg ↔ CONTRIBUTING | — | sonnet | low | всё |
| 3 | T-111 [#259](https://github.com/it1ro/brig-lang/issues/259) | `make plan-check` | T-110 | sonnet | medium | T-113…T-119 |
| 4 | T-113 [#175](https://github.com/it1ro/brig-lang/issues/175) | Примеры спеки и `examples/` в `make all` | — | sonnet | low | T-111, T-114 |
| 5 | T-114 [#261](https://github.com/it1ro/brig-lang/issues/261) | CI: ночной fuzz и `-race` на PR | — | sonnet | low | всё |
| 6 | T-115 [#176](https://github.com/it1ro/brig-lang/issues/176) | Корпус библиотечного кода и `make corpus` | T-113 | opus | large | T-116, T-118 |
| 7 | T-116 [#177](https://github.com/it1ro/brig-lang/issues/177) | `check-examples`: компиляция блоков, `pending(T-NN)`, причина `invalid` | T-113 | opus | medium | T-115, T-118 |
| 8 | T-117 [#178](https://github.com/it1ro/brig-lang/issues/178) | `check-examples`: исполнение REPL-блоков | T-116 | opus | medium | T-118 |
| 9 | T-118 [#262](https://github.com/it1ro/brig-lang/issues/262) | Сверка таблиц спеки с кодом | T-113 | sonnet | medium | T-115…T-117 |
| 10 | T-119 [#263](https://github.com/it1ro/brig-lang/issues/263) | Устаревшие doc 02, architecture, skills, README | — | sonnet | low | всё |

Ни одна задача волны не трогает `internal/compiler/compiler.go`.

## Задачи

Все задачи волны заведены на доске: DoD — в issues по ссылкам из таблицы.
