# Wave 1 — test-infra и разблокировка

[← карта плана](README.md)

Вход: T-07. Выход: набор §7 в `main` (упавшие тесты — через `t.Skip("blocked: T-NN")`), golden `.ast` содержательны, skills актуальны, по трём `[inferred]` findings есть вердикт.

## Задачи

| T-NN | Issue | Название | depends_on | Источник |
|---|---|---|---|---|
| T-10 | [#8](https://github.com/it1ro/brig-lang/issues/8) | Набор регресс-тестов из §7 аудита | T-07 | audit |
| T-11 | [#9](https://github.com/it1ro/brig-lang/issues/9) | ast.Pretty и ast.Walk не видят Decl | T-10 | audit |
| T-12 | [#10](https://github.com/it1ro/brig-lang/issues/10) | Обновить устаревшие skills | T-11 | audit |
| T-13 | [#11](https://github.com/it1ro/brig-lang/issues/11) | Verify: A-F1 — однопоточный scheduler | T-07 | audit |
| T-14 | [#12](https://github.com/it1ro/brig-lang/issues/12) | Verify: A-F7 — exit-коды и тесты в обход sema | T-07 | audit |
| T-15 | [#13](https://github.com/it1ro/brig-lang/issues/13) | Verify: I-F14 — таймеры | T-07 | audit |
| T-16 | [#65](https://github.com/it1ro/brig-lang/issues/65) | CI: установка golangci-lint падает на checksum | — | — |
