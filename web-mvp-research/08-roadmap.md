# 08. Roadmap

Фазы идут последовательно; внутри фазы задачи в основном независимы.
Привязка к доске — по существующим T-NN, где они есть.

## Фаза 0. Язык готов (предусловие — текущие волны)

- Wave 6: T-70 match, T-71 with, T-72 pipe, T-73 записи, T-74 record-паттерны,
  T-75 прелюдия (`Sys.args`, `link`, `mailbox_size`), T-79 stack trace.
- T-58: учёт native-вызовов в редукциях.
- Внести принятые решения в спеку — задачи `edit-spec` (07; первая — #169):
  Q-await, акторные примитивы (R3–R6), порты и G5 (R1–R2, B2), `pub fn`,
  модули/типы как значения, L18–L22, контракты (§14.4), пакеты (§17.5).

**Выход:** первая «рабочая версия языка».

Сквозное с фазы 1: микро-бенчмарки VM в каждом PR (19).

## Фаза 1. Проектная база (без I/O)

1. Многофайловые модули + кэш байткода (L2).
2. `pub fn` (L3), `rec.field` (L4), модули/типы как значения (L5, L19),
   `(:badmatch, v)` (L21), `_name` (L20), `fn ->` и `(a, b) ->` (L22, #169),
   info-диагностика `_ <-` (L17).
3. Embedded stdlib на Brig через `go:embed` (L2) — первый модуль: `Result`/`Option`-хелперы.
4. `Str`/`Bytes`/кодеки (L8), многострочные строки (L9),
   `Instant`/`Date`/`Duration`, `Decimal` в Must (L12), `Json.at`/`Map.get_or` (L16).
5. `brig fmt`, `brig test` с обнаружением тестов и доктестами `##` (22).
6. `brig fix` и первая миграция `fn` → `pub fn` (B5).
7. **Пакетный менеджер** (05): git + MVS + lock + кэш + vendor; пакеты — только Brig-исходники.
8. Контракты уровня P на `pub fn` за флагом (09).

**Выход:** можно писать и публиковать многофайловые библиотеки на Brig.

## Фаза 2. Рантайм-механизмы

1. Внешние события в run-loop (R1) + `Signal`-порт как первый порт (минимальный риск).
2. `exit`, `spawn_watched`, реестр, `Global` (R3–R5, R13).
3. `await(ref)` (R12), `Timer` (R6), `Time` (R7).
   Бюджет «хода» и счётчики на актор (11/M1–M2), `--memory-limit` (11/M3).
   `Telemetry` с событиями VM, `Term` (16), пул Go-воркеров для тяжёлых native (R15).
4. `File`, `Proc`, `Stdin` (R2) → **скриптинг-ниша закрыта**
   (shebang, режим `script` Q-script).
5. `Supervisor`, `Server` (`call`/`reply`) на Brig.

**Выход:** серверные скрипты и долгоживущие демоны на Brig.

Параллельно после релиза языка: WASM-песочница в браузере (B6), грамматики
tree-sitter, RFC-процесс и политика товарных знаков, тур языка (20).

## Фаза 3. Сеть

1. `Tcp` (pull-режим, владение, iodata), `Tls`.
2. Фейковые часы + in-memory транспорт (R9), `brig test --simulate --seed` (B4),
   `Test.isolated` на экземплярах рантайма (16).
3. Эталон: echo-сервер под супервизором; нагрузочный тест (без утечек, без
   блокировки планировщика, graceful shutdown). Замеры памяти Z1–Z5 (11),
   включая `--copy-on-send`; макро-бенчмарки и ночной прогон с историей (19).
4. HTTP-порты на Go `net/http` (сервер + клиент, HTTP/1.1 + HTTP/2, B2), `Conn` и плаги на Brig,
   `Log`, `Config`, `Crypto`, `Cache` (B3).
5. `Acme`: автоматический HTTPS (`--domain`), `brig cert dev` (B1).

**Выход:** HTTP hello с автоматическим HTTPS: транспорт Go, вся логика на Brig,
run-loop не блокируется.

## Фаза 4. Calmar MVP

1. `Conn`, конвейер, роутер (17: `resources`, типизированные параметры, хелперы,
   `forward`, HTTP-семантика), контроллеры, fallback ошибок.
2. `Json.decode_as` с проверкой формы, `Check` + `validate/1`, «модуль типа» (L6),
   `Changeset` поверх них (09).
3. `Sql` + встроенный SQLite + миграции.
4. `PubSub`, SSE.
5. Шаблоны `.bt` (15): параметры, компоненты, контекстное экранирование,
   «статика + динамика», проверки при сборке; сессии, CSRF.
6. Фронтенд: import maps, кеш-бастинг ассетов (C8); загрузки файлов (C2).
7. `brig observe`, страница ошибок в dev (C7); `SO_REUSEPORT` + graceful drain (C6).
8. Официальные пакеты первой волны: `Postgres`, `Mailer` (D2).
9. Лаунчер `calmar` (`brig install brig.dev/calmar`): `calmar new` (и `--api`), `calmar server`
   (на `brig run --watch`), `calmar db migrate`; `brig build` (payload с зависимостями).
10. `brig task` и `tasks/`; команды релиза `start`/`console`/`eval` (16).
11. `calmar gen auth`, `Calmar.authorize` (18).
12. HTTP-кэш и `send_file`, отчёты о падениях через `Telemetry` (20).

**Выход:** `corpus/lookout` собирается `brig build` в один файл и работает.

## Фаза 5. После MVP

- Данные: пакет data mapper в духе Ecto (14).
- Контракты: экспорт/дифф схем (`brig schema export|diff`), импорт OpenAPI/protobuf (09).
- DX: LSP (сразу после MVP), `brig console` с `h`, `brig doc`, генераторы `calmar gen`, отладчик;
  сборка документации пакетов в Chandler (22).
- Пакеты: 2FA, passkeys, OAuth/OIDC (18); фоновые задания с персистентной очередью (C5); NATS, Redis, S3,
  OpenTelemetry (D2); Chandler — индекс и прокси; права зависимостей (10/#12).
- Веб: WebSocket, серверный UI в духе LiveView (C8), метрики Prometheus.
- Публичный дашборд производительности, участие в TechEmpower (19).
- Документация на английском, гайды «Brig by example», «С Phoenix/Rails на Calmar»,
  перевод спеки к 1.0 (20).
- Рантайм: N:M (R10), Go-embedding API (R11), порты `Serial`/`Gpio`,
  WASM-песочница (B6); решение по модели heap — по замерам (11).

## Критерии успеха web-MVP (уточнённые)

- `brig install brig.dev/calmar && calmar new lookout && cd lookout && calmar server`
  — < 5 с после скачивания; повторно — офлайн из кэша.
- `brig build --target linux/arm64` → один файл ≤ 25 МБ, запускается на
  чистом хосте без зависимостей.
- 10k keep-alive соединений на одном ядре без роста памяти после прогона;
  планировщик не блокируется I/O.
- Падение handler'а/чекера не затрагивает другие запросы; SIGTERM →
  graceful shutdown за ≤ таймаута.
- Интеграционные тесты demo проходят без сети и без `sleep`.
- Ни одного `nil`, async/await, макроса; ошибки — `Result`/`raise`+`trap`.
