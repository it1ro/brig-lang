# Wave 0 — на ветке `iter/regvm`, до merge

[← карта плана](README.md)

Вход: аудит на `8ab58cf`. Коммиты идут прямо в `iter/regvm` (integration-ветка, `WORKFLOW.md` §3), формат `<type>(<scope>): <subject> [T-NN]`; issue закрывается ссылкой на коммит. Каждая задача сама создаёт свой тест-якорь: правило «тесты §7 до фиксов» действует с Wave 1. Отклонение от «только fail-fast»: T-06 (lint, O-F3) — без него `make all` красный и merge невозможен. Выход: T-07 закрыт, теги `stack-vm-final` и `regvm-merged` на origin.

## Задачи

| T-NN | Issue | Название | depends_on | Источник |
|---|---|---|---|---|
| T-01 | [#1](https://github.com/it1ro/brig-lang/issues/1) | Fail-fast: мультиклозы, guard и параметры-паттерны fn | — | audit |
| T-02 | [#2](https://github.com/it1ro/brig-lang/issues/2) | Fail-fast: guard в ветках recv | — | audit |
| T-03 | [#3](https://github.com/it1ro/brig-lang/issues/3) | Fail-fast: гибрид ensure теряет блок | — | audit |
| T-04 | [#4](https://github.com/it1ro/brig-lang/issues/4) | Fail-fast: интерполяция строк → явная ошибка | — | audit |
| T-05 | [#5](https://github.com/it1ro/brig-lang/issues/5) | REPL: nil-deref на любой строке | — | audit |
| T-06 | [#6](https://github.com/it1ro/brig-lang/issues/6) | Lint: 10 замечаний golangci-lint | — | audit |
| T-07 | [#7](https://github.com/it1ro/brig-lang/issues/7) | Merge iter/regvm → main | T-01, T-02, T-03, T-04, T-05, T-06 | audit |
