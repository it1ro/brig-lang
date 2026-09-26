# Wave 4 — blockers full-fix

[← карта плана](README.md)

Вход: T-11, T-20, T-23, T-36. Выход: fail-fast из T-01, T-02, T-04 удалены, канонические примеры §6.1/§6.5/§12.4 и интерполяция дают правильный результат.

## Задачи

| T-NN | Issue | Название | depends_on | Источник |
|---|---|---|---|---|
| T-50 | [#33](https://github.com/it1ro/brig-lang/issues/33) | S-F2: параметры-паттерны и guard fn в AST и парсере | T-11, T-20 | audit |
| T-51 | [#34](https://github.com/it1ro/brig-lang/issues/34) | S-F2: компиляция мультиклозных fn и guard | T-36, T-50 | audit |
| T-52 | [#35](https://github.com/it1ro/brig-lang/issues/35) | S-F3: компиляция guard в recv | T-02, T-51 | audit |
| T-53 | [#36](https://github.com/it1ro/brig-lang/issues/36) | S-F1: интерполяция в лексере и парсере (узел Interp) | T-11, T-23 | audit |
| T-54 | [#37](https://github.com/it1ro/brig-lang/issues/37) | S-F1: компиляция интерполяции | T-53 | audit |
