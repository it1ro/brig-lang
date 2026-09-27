# Wave 10 — язык для библиотек и stdlib на Brig

[← карта плана](README.md)

**Откуда:** research, фаза 1 «проектная база» (`web-mvp-research/08-roadmap.md`).

**Вход:**
- Wave 9 закрыта: модули, варианты, связывания, неизвестные имена;
- спека пакетов A и B внесена (T-128, T-129), T-100 (#169) закрыта.

**Зачем:** сделать то, без чего на Brig нельзя писать библиотеки:
- `pub fn`;
- ссылки на функции модулей;
- record update с сохранением вида;
- многострочные строки;
- лямбды.

И запустить второй контур обратной связи: stdlib, написанную на Brig.
После этой волны язык каждый раз проверяется на собственной библиотеке и
её доктестах.

**Выход:**
- `pending` из T-128 и T-129 сняты;
- `brig test` роняет CI при падении;
- модули `List`, `Option`, `Result` на Brig встроены в бинарник и
  покрыты доктестами;
- файлы корпуса `corpus/whelk/hook.brig` и `corpus/lookout/lib/lookout/*`
  доходят до уровня `check` везде, где им не нужны модули рантайма
  (Wave 12) или горизонта: метки `needs` это отражают.

## Порядок и параллельность

`internal/compiler/compiler.go` правят T-140, T-141, T-142, T-143, T-144 —
их мержат по одной. T-145 (лексер), T-146 (stdlib и загрузчик),
T-147 (`cmd/brig`, прелюдия `Test`) и T-148 (Go-native `Str`/`Bytes`)
идут параллельно с ними.

| # | T-NN | Задача | Ждёт | Модель | Effort | Параллельно с |
|---|---|---|---|---|---|---|
| 1 | T-140 [#203](https://github.com/it1ro/brig-lang/issues/203) | Лямбды `fn ->` и `(a, b) ->` (по #169) | T-100 | opus | medium | T-145, T-147, T-148 |
| 2 | T-141 [#265](https://github.com/it1ro/brig-lang/issues/265) | Паттерны в параметрах полной лямбды | T-140 | opus | medium | T-145, T-147, T-148 |
| 3 | T-142 [#266](https://github.com/it1ro/brig-lang/issues/266) | Record update сохраняет вид; `Record.to_anon` | T-128 | sonnet | low | T-145, T-147, T-148 |
| 4 | T-143 [#267](https://github.com/it1ro/brig-lang/issues/267) | `pub fn`: приватность по умолчанию | T-139 | opus | medium | T-145, T-147, T-148 |
| 5 | T-144 [#268](https://github.com/it1ro/brig-lang/issues/268) | `Mod.f` как значение-функция | T-137 | opus | medium | T-145, T-147, T-148 |
| 6 | T-145 [#269](https://github.com/it1ro/brig-lang/issues/269) | Многострочные строки `"""` | T-129, T-132 | opus | medium | всё |
| 7 | T-146 [#270](https://github.com/it1ro/brig-lang/issues/270) | Встроенная stdlib на Brig: `List`, `Option`, `Result` | T-130, T-143 | opus | large | T-147, T-148 |
| 8 | T-147 [#198](https://github.com/it1ro/brig-lang/issues/198) | `brig test`: раннер, exit-код, доктесты `##` | T-117, T-125 | opus | medium | всё |
| 9 | T-148 [#271](https://github.com/it1ro/brig-lang/issues/271) | `Str` и `Bytes`: базовый набор (Go-native) | T-130 | sonnet | medium | всё |
| 10 | T-149 [#199](https://github.com/it1ro/brig-lang/issues/199) | Docs: тест-фреймворк и доктесты в спеке | T-125, T-147 | sonnet | low | — |

## Задачи

Все задачи волны заведены на доске: DoD — в issues по ссылкам из таблицы.
