---
name: brig-testing-workflow
description: >
  Порядок прогона тестов и Makefile-целей для проекта Brig: узкие таргеты
  по слоям, golden/bytecode/negative тесты, fuzz, полный ci-прогон.
  Использовать в конце КАЖДОЙ задачи перед тем, как сообщить, что работа
  завершена, и при выборе, какие тесты гонять на промежуточных шагах.
---

# Brig — тестовая инфраструктура

## Порядок прогона (от дешёвого к дорогому)

1. **Узкий таргет по изменённому слою** — гонять после каждого
   логического шага, не дожидаясь конца задачи:
   - `make test-lexer` / `make test-parser` / `make test-ast` (частично
     через `go test ./internal/ast/...`)
   - `make test-vm` / `make test-compiler`
   - `go test ./internal/sema/...`
   - `make test-one PKG=./internal/xxx TEST=TestYyy` — для одного теста
2. **`make ci-quick`** — `fmt-check` + `vet` + focused tests
   (`test-lexer test-parser test-roundtrip test-vm test-compiler`) +
   `run-examples`. Не требует `golangci-lint`, быстрый — гонять перед
   каждым коммитом.
3. **`make test-race`** — обязателен при любой правке `internal/vm`
   (акторы, замыкания) перед тем, как считать задачу завершённой.
4. **`make all`** — `check-smallint` + `fmt` + `vet` + `test` + `lint` +
   `build`; его же запускает CI (`.github/workflows/ci.yml`) на push и PR
   в `main`. Вместе с `BRIG_VERIFY=1 go test ./...` — обязательный гейт
   перед PR (`CONTRIBUTING.md` §5).
5. **`make fuzz`** — 3×60s (лексер, парсер, round-trip). Не входит в
   стандартный цикл; гонять точечно после правок в `lexer`/`parser`/
   `ast`-форматтере, особенно если менялась offside-логика или граничные
   случаи escape-последовательностей.

## Golden-тесты (`testdata/golden/`)

`internal/parser/golden_test.go` сверяет `ast.Pretty` (S-expression) и
`ast.Format` (round-trip .brig) с зафиксированными файлами `*.ast` и
`*.round.brig`. При намеренном изменении форматирования/AST-структуры:

```sh
go test ./internal/parser/... -run TestGolden -update
# или
make update-golden
```

**Перед коммитом diff'а golden-файлов — обязательно вручную прочитать,
что изменилось.** Golden-тесты — это защита от случайных регрессий; слепой
`-update` без чтения diff'а сводит их ценность к нулю. Обновлённые golden —
отдельный коммит `test(<scope>): regenerate golden files [T-NN]`.

`*.ast` — вывод `ast.Pretty`. Decl, Pattern и Type в `prettyNode`/
`walkNode` стоят раньше `Expr` (S-F13, T-11): у этих узлов есть
`IsExpression()`, и `case Expr` выше них снова сделает `.ast` пустым.
Пустой `(program )` допустим только у программы без decl и stmt.

## Bytecode-goldens (`testdata/bytecode/`)

`internal/compiler/bytecode_test.go` сверяет дизассемблированный вывод
фиксированного набора модулей (`bytecodeCases`) с `testdata/bytecode/*.txt`.
Обновление:

```sh
go test ./internal/compiler/... -run TestBytecodeGolden -update-bytecode
# или
make update-bytecode
```

Изменение здесь означает изменение наблюдаемого байткода — почти всегда
достойно отдельного упоминания в коммите/changelog, а не молчаливого
прогона `-update`.

## Negative-тесты (`testdata/negative/`)

Пары `<name>.brig` + `<name>.err` (`internal/parser/negative_test.go`):
парсинг `<name>.brig` обязан упасть с ошибкой, содержащей подстроку из
`<name>.err`. При добавлении нового класса ошибок парсинга — всегда
добавлять пару файлов сюда, а не только `t.Fatalf` в обычном Go-тесте:
это единственное место, которое `check-examples`-подобный ревьюер видит
как «каталог известных невалидных программ».

## `check-examples` (A2)

`make check-examples` прогоняет все ```` ```brig ```` fenced-блоки из
`docs/01-language-design.md` через парсер + sanity round-trip. Метки:
`brig module`, `brig repl`, `brig expr`, `brig stmt`, `brig invalid`,
`text`/`pseudo` для мета-примеров (не код). Он только **парсит** блоки,
не компилирует и не исполняет. Ожидаемый результат:
`blocks: checked 62, failed 0` (68 fenced-блоков, из них 6 `invalid`) —
если при правке документации это число
меняется, нужно либо поправить пример, либо (если он специально
демонстрирует невалидный код) пометить `brig invalid`, либо (если это
мета-пример вроде обёртки `fn main() -> <expr>`) понизить до `text`.

## Регресс-тесты аудита и `t.Skip("blocked: T-NN")`

Набор §7 `AUDIT_REPORT.md` лежит в
`internal/compiler/audit_regress_test.go`, `internal/vm/verify_test.go`,
`internal/ast/audit_pretty_test.go`, `internal/repl/audit_repl_test.go`
(добавлен T-10 #8). Тест, который падает на текущем коде, помечен
`t.Skip("blocked: T-NN")` — CI остаётся зелёным. Задача T-NN обязана:
снять свой skip, увидеть падение, починить, увидеть зелёный. После неё
`rg -n 'blocked: T-NN' internal` пуст. Новые skip'ы — только в таком
формате и только со ссылкой на существующий issue.

## Exit-коды и что они значат в тестах

При написании тестов CLI (`cmd/brig`) помнить коды из `brig-overview`:
`0` ok, `1` parse/sema/compile-«срез», `2` runtime raise, `3` internal
(`internal:`). Тест, проверяющий обработку ошибки, должен целиться в
конкретный код, а не просто «err != nil». Классификация exit-кодов —
T-45 (#57); `runModule` без sema — T-46 (#58)
(`TestVerifyAF7RunModuleSkipsSema`).

Тест должен **утверждать** результат (`assert(...)` в brig-коде /
`t.Fatalf` при несовпадении), а не только печатать его. Якорные
print-only тесты O-F2 переписаны в T-30 (#19); новые — только с
утверждениями.

## Что сообщать пользователю по завершении задачи

Это же — содержимое раздела «Как проверялось» в body PR (`CONTRIBUTING.md`
§5). Явно указать:
1. Какие `make`-цели были прогнаны и с каким результатом.
2. Обновлялись ли golden/bytecode файлы, и если да — что именно изменилось
   (не просто «golden обновлены»).
3. Если `make fuzz` не гонялся (обычно так, из-за времени) — сказать это
   прямо, а не умалчивать.
4. Если менялась семантика языка — обновлён ли
   `docs/01-language-design.md` синхронно с кодом.
