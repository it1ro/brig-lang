# 04. Stdlib: батарейки внутри бинарника

## Правило раскладки

| Слой | Когда | Примеры |
|---|---|---|
| **VM (Go, ядро)** | нужен доступ к планировщику/памяти/ОС | порты, `exit`, реестр, таймеры |
| **Go-native stdlib** | криптография, парсинг форматов, горячие циклы над байтами | `Crypto`, `Tls`, `Json`, `Bytes`/`Str`, `Time` форматирование, SQLite-драйвер |
| **Brig-stdlib (embedded)** | политика, протоколы, всё, что выиграет от читаемости и dogfooding | `Supervisor`, `GenServer`-аналог, `Http` (`Conn`, плаги, роутинг поверх HTTP-портов), `PubSub`, `Log`, `Config`, `Sql`-пул |
| **Пакеты** | узкие домены, быстрая эволюция | `Postgres`, `Mailer` (первая волна, D2), NATS, Redis, S3, OAuth, i18n-каталоги |

Критерий Go vs Brig для горячего пути: сначала Brig; переносим в Go, только
если бенчмарк показывает узкое место **и** API остаётся тем же.

## Модули и уровни

| Модуль | Содержимое | Слой | Уровень |
|---|---|---|---|
| `Result`, `Option` | `map`, `and_then`, `unwrap`, `all` (список Result → Result списка), `unwrap_or` | Brig | M |
| `Bytes`, `Str` | см. 03/L8 | Go | M |
| `Base64`, `Hex`, `Url` | кодеки | Go | M |
| `Json` | уже есть; + `decode_as(Type, …)`, потоковый encode в iodata | Go | M |
| `Time`, `Timer` | `monotonic_ms`, `now`, `send_after`, форматы RFC3339/HTTP-date; встроенная база часовых поясов (C1) | VM+Go | M |
| `Unicode`, `Plural` | нормализация, регистр, CLDR-правила плюрализации (C4); переводы — пакет | Go | M |
| `Crypto` | `sha256`, `hmac`, `random_bytes`, `secure_compare`, `argon2id`/`bcrypt` | Go | M (сессии, CSRF, пароли) |
| `Tcp`, `Tls` | порты | VM+Go | M |
| `Acme` | автоматический HTTPS (Let's Encrypt): HTTP-01 + TLS-ALPN-01, хранение в каталоге состояния, продление супервизируемым актором; интегрирован с `HttpServer` (`--domain`) (B1) | Go + Brig-актор | M |
| `Cache` | шардированные акторы-кэши с TTL: сессии, rate limiting (B3) | Brig | M |
| `File`, `Dir`, `Path` | потоковое чтение/запись, листинг, `glob`, временные файлы | VM+Go | M (скриптинг, статика, загрузки) |
| `Http.Multipart` | потоковый разбор multipart; файлы больше порога — во временный файл, `Upload{ path, name, size, type }` (C2) | Brig | M |
| `Proc` | subprocess как порт, `Proc.run(cmd, args)` для скриптов | VM+Go | M для скриптинга |
| `Env`, `Sys` | `Env.get -> Option<Str>`, `Sys.args`, `Sys.exit`, `Signal` | VM | M |
| `Supervisor`, `Server` | супервизор (статический и `start_dynamic`); generic server: `Server.call(pid, req, timeout)` шлёт `(:call, from, req)` и ждёт через `await` (02/R12), `Server.reply(from, v)` (§17.9) | Brig | M |
| `Registry`, `PubSub` | над реестром имён R5; topic → подписчики с `watch` | Brig | M |
| `Log` | структурированные логи (key-value), уровни, JSON-вывод | Brig | M |
| `Config` | слои: дефолты → файл → env; валидация на старте | Brig | M |
| `Http` | транспорт — порты на Go `net/http` (HTTP/1.1 + HTTP/2 + TLS, сервер и клиент, B2); на Brig — `Conn`, `Plug`-протокол, базовые плаги (сессии, CSRF, статика, CORS) — общий слой Calmar и Whelk | Go (транспорт) + Brig | M |
| `Http.Ws`, `Http.Sse` | WebSocket, Server-Sent Events | Brig | S (SSE почти бесплатен — M) |
| `Sql` + `Sqlite` | общий интерфейс + встроенный pure-Go драйвер | Brig + Go | M (см. Q-db) |
| `Test`, `Http.Test` | `Test` уже есть; `Http.Test` — in-memory запросы к обработчику через тестовый порт запроса, фейковое время; `Calmar.Test` и `Whelk.Test` строятся поверх | Brig | M |
| `Html` | `escape`, `Html.Safe`, рантайм шаблонов | Brig | M для SSR |
| `Csv`, `Cli` | CSV; разбор аргументов командной строки | Go / Brig | S |
| `Metrics` | счётчики/гистограммы, экспорт Prometheus-текстом | Brig | S |

## HTTP в stdlib, а не в пакете — почему

«Один файл» + «веб — первая ниша» ⇒ HTTP-сервер и клиент должны быть в
бинарнике: HTTP-сервис или скрипт с HTTP-клиентом должен работать без
единой зависимости. Сам фреймворк Calmar — отдельный пакет (10).
Это **не** противоречит «HTTP не в VM»: транспорт (разбор протокола,
TLS, HTTP/2) — Go `net/http` в Go-native stdlib за портом, смысл (`Conn`,
плаги, роутинг) — на Brig; VM о HTTP не знает (B2). Свой HTTP-парсер на
Brig не пишем: проверенный парсер — это и скорость, и безопасность
(request smuggling), а HTTP/2 и параллельный на других ядрах разбор
получаем бесплатно.

## База данных — главная развилка

Rails/Phoenix-опыт немыслим без БД. **Решение (Q-db): SQLite встроен
(pure-Go драйвер, ~+6–8 МБ)** как дефолт «из коробки»
(одно приложение = один файл + один файл БД — идеально для edge и малых
сервисов), `Sql`-интерфейс общий; Postgres — протокол на чистом Brig
поверх `Tcp` как пакет первой волны (D2).

## Бюджет размера (оценка, требует замера)

| Компонент | ≈ МБ |
|---|---|
| текущий `brig` | 5.7 |
| `crypto/tls`, `net/http` + HTTP/2, x509 | +4–5 |
| ACME (B1), tz-база (C1), CLDR-плюрализация (C4) | +1–1.5 |
| pure-Go SQLite | +6–8 |
| Brig-stdlib (байткод) + шаблонизатор | +1 |
| strip + `-ldflags=-s -w` | −25–30 % |
| **итог** | **≈ 14–18** |

Укладываемся в цель ≤ 25 МБ с запасом.
