# 12. Whelk — микро-фреймворк в духе Sinatra (утверждено 2026-09-27)

**Whelk** (трубач, морской моллюск) — «сервис в одном файле»: webhook-
приёмники, маленькие API, внутренние инструменты, прототипы, edge.
Сосед Calmar по семейству «фреймворки — морские обитатели».

## Решения (Q-micro)

| # | Решение |
|---|---|
| 1 | `Conn`, протокол плагов и базовые плаги (сессии, CSRF, статика, CORS) — в **stdlib `Http`**; Calmar и Whelk — два фреймворка над одним ядром. Плаги и обработчики переносимы, приложение на Whelk растёт в Calmar без переписывания |
| 2 | Однофайловый скрипт объявляет зависимости **`fn project()` прямо в файле** (те же литералы, что в `project.brig`); lock — рядом (`hook.brig.lock`) |
| 3 | Роутинг — **только мультиклозная функция** по `(method, segments)`: guards, порядок клауз виден, компилятор уже всё умеет. Хелперов путей и групп маршрутов нет — когда понадобились, пора в Calmar |
| 4 | Обработчик возвращает **`Conn` или `Result<Conn, E>`**; `Str`/`Map` сами в ответ не превращаются |
| 5 | Состояние — **акторы** (реестр имён, `Global`); хелпера `Whelk.state` в v0.1 нет, есть пример в документации |
| 6 | Границы: **есть** — match-роутинг, `Conn`/плаги, JSON, статика, SSE, тесты в памяти; **нет** — генераторов, миграций, `.html.bt`, контроллеров, конфигов по окружениям. HTML — `Html`-хелперы stdlib с экранированием |
| 7 | Своей CLI нет: `brig run hook.brig`, `brig build hook.brig`, `brig install` |

## API (весь)

```text
Whelk.run(opts, handler)                 # opts: { port, domain?, max_concurrent?, plugs? }
Whelk.text(conn, str) / json(conn, v) / html(conn, safe)
Whelk.status(conn, code) / redirect(conn, url)
Whelk.not_found(conn) / bad_request(conn, why)
Whelk.body_json(conn) -> Result<Any, E>  # потоковое чтение тела через порт запроса
Whelk.Test.request(handler, method, path, opts) -> Resp   # in-memory
```

SSE и плаги — из `Http` (`Http.Sse`, `Http.Plug.*`), общие с Calmar.
`--domain` включает автоматический HTTPS (B1), как у Calmar.

## Пример: приёмник GitHub-вебхуков в одном файле

Файл целиком — [demo-whelk/hook.brig](demo-whelk/hook.brig). Показывает:
манифест внутри скрипта, проверку HMAC-подписи плагом, match-роутинг,
состояние в акторе (кольцевой буфер последних событий), SSE как
`recv`-цикл, запуск одной командой.

```text
$ GITHUB_SECRET=… ./hook.brig                      # shebang, dev
$ brig build hook.brig -o hook && ./hook --domain hooks.example.com
```

## Путь роста в Calmar

1. `dispatch`-клаузы превращаются в действия контроллеров (сигнатура
   `(conn, params)` вместо `(conn, method, segments)`).
2. Плаги переносятся как есть — они из общего `Http`.
3. Актор состояния переезжает под супервизор приложения.
4. `fn project()` → `project.brig`.

Помощник миграции (`calmar new --from hook.brig`) — Nice.

## Lock и права скрипта (утверждено 2026-09-27)

- **Lock** — `hook.brig.lock` рядом со скриптом; если каталог только для
  чтения (например, `/usr/local/bin`) — в кэше по хешу абсолютного пути
  скрипта, с предупреждением.
- **Права** ограничивают только **зависимости** (10/#12): Whelk получает
  `allow: [:net]` в `deps`. Сам скрипт — твой код и ничем не ограничен, как
  bash/python; скрипт без манифеста — тоже.
