# 08. Roadmap

Фазы идут последовательно; внутри фазы задачи в основном независимы.
Привязка к доске — по существующим T-NN, где они есть.

## Фаза 0. Язык готов (предусловие — текущие волны)

- Wave 6: T-70 match, T-71 with, T-72 pipe, T-73 записи, T-74 record-паттерны,
  T-75 прелюдия (`Sys.args`, `link`, `mailbox_size`), T-79 stack trace.
- T-58: учёт native-вызовов в редукциях.
- DD по §17: Q-await, Q-mod, Q-vis, L18 (record update), L19 (тип/модуль).

**Выход:** первая «рабочая версия языка».

## Фаза 1. Проектная база (без I/O)

1. Многофайловые модули + кэш байткода (L2).
2. `pub fn` (L3), `rec.field` (L4), модули/типы как значения (L5).
3. Embedded stdlib на Brig через `go:embed` (L2) — первый модуль: `Result`/`Option`-хелперы.
4. `Str`/`Bytes`/кодеки (L8), многострочные строки (L9).
5. `brig fmt`, `brig test` с обнаружением тестов.
6. `brig fix` и первая миграция `fn` → `pub fn` (B5).
7. **Пакетный менеджер** (05): git + MVS + lock + кэш + vendor; пакеты — только Brig-исходники.
8. Контракты уровня P на `pub fn` за флагом (09).

**Выход:** можно писать и публиковать многофайловые библиотеки на Brig.

## Фаза 2. Рантайм-механизмы

1. Внешние события в run-loop (R1) + `Signal`-порт как первый порт (минимальный риск).
2. `exit`, `spawn_watched`, реестр, `Global` (R3–R5, R13).
3. `await(ref)` (R12), `Timer` (R6), `Time` (R7).
   Бюджет «хода» и счётчики на актор (11/M1–M2), `--memory-limit` (11/M3).
4. `File`, `Proc`, `Stdin` (R2) → **скриптинг-ниша закрыта**
   (shebang, режим `script` Q-script).
5. `Supervisor`, `Server` (`call`/`reply`) на Brig.

**Выход:** серверные скрипты и долгоживущие демоны на Brig.

Параллельно после релиза языка: WASM-песочница в браузере (B6).

## Фаза 3. Сеть

1. `Tcp` (pull-режим, владение, iodata), `Tls`.
2. Фейковые часы + in-memory транспорт (R9), `brig test --simulate --seed` (B4).
3. Эталон: echo-сервер под супервизором; нагрузочный тест (без утечек, без
   блокировки планировщика, graceful shutdown). Замеры памяти Z1–Z5 (11),
   включая `--copy-on-send`.
4. HTTP-порты на Go `net/http` (сервер + клиент, HTTP/1.1 + HTTP/2, B2), `Conn` и плаги на Brig,
   `Log`, `Config`, `Crypto`, `Cache` (B3).
5. `Acme`: автоматический HTTPS (`--domain`), `brig cert dev` (B1).

**Выход:** HTTP hello с автоматическим HTTPS: транспорт Go, вся логика на Brig,
run-loop не блокируется.

## Фаза 4. Calmar MVP

1. `Conn`, конвейер, роутер-данные, контроллеры, fallback ошибок.
2. `Json.decode_as` с проверкой формы, `Check` + `validate/1`, «модуль типа» (L6),
   `Changeset` поверх них (09).
3. `Sql` + встроенный SQLite + миграции.
4. `PubSub`, SSE.
5. Шаблоны `.html.bt` (L10), сессии, CSRF.
6. Лаунчер `calmar` (`brig install brig.dev/calmar`): `calmar new`, `calmar server`
   (на `brig run --watch`), `calmar db migrate`; `brig build` (payload с зависимостями).

**Выход:** demo/`lookout` собирается `brig build` в один файл и работает.

## Фаза 5. После MVP

Экспорт/дифф схем (`brig schema export|diff`), импорт OpenAPI/protobuf (09),
WebSocket, генераторы `calmar gen`, `brig console`, LSP, Chandler (индекс + прокси),
права пакетов как в Deno (10),
Postgres, фоновые задания с персистентностью, метрики, LiveView-подобное,
N:M, Go-embedding API, порты `Serial`/`Gpio`.

## Критерии успеха web-MVP (уточнённые)

- `brig install brig.dev/calmar && calmar new lookout && cd lookout && calmar server`
  — < 5 с после скачивания; повторно — офлайн из кэша.
- `brig build --target linux/arm64` → один файл ≤ 25 МБ, запускается на
  чистом хосте без зависимостей.
- 10k keep-alive соединений на одном ядре без роста памяти после прогона;
  планировщик не блокируется I/O.
- Падение handler'а/чекера не затрагивает другие соединения; SIGTERM →
  graceful shutdown за ≤ таймаута.
- Интеграционные тесты demo проходят без сети и без `sleep`.
- Ни одного `nil`, async/await, макроса; ошибки — `Result`/`raise`+`trap`.
