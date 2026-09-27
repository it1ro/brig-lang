# Wave 2 — small fixes слоя 1

[← карта плана](README.md)

Вход: T-10 и T-11 (golden `.ast` осмысленны). Выход: все задачи волны закрыты, `make all` зелёный.

## Задачи

| T-NN | Issue | Название | depends_on | Источник |
|---|---|---|---|---|
| T-20 | [#14](https://github.com/it1ro/brig-lang/issues/14) | Parser: guard через parseOr и идемпотентный round-trip guard | T-11 | audit |
| T-21 | [#15](https://github.com/it1ro/brig-lang/issues/15) | Parser: NEWLINE обязателен между стейтментами | T-11 | audit |
| T-22 | [#16](https://github.com/it1ro/brig-lang/issues/16) | Числовые литералы: base 10 по умолчанию и произвольная точность | T-10 | audit |
| T-23 | [#17](https://github.com/it1ro/brig-lang/issues/17) | Lexer: `0x_1`, `0b102`, ATOM после `)` | T-10 | audit |
| T-24 | [#18](https://github.com/it1ro/brig-lang/issues/18) | Parser: NEWLINE-sep, паттерн `()`, порядок в with | T-11 | audit |
