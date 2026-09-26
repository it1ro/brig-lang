# 06. Архитектура фреймворка

Имя — **Calmar** (утверждено, Q-name в 07).

## Идея в одной фразе

**Чистое ядро, акторная оболочка.** Обработка запроса — чистая функция
`Conn -> Conn`, которую легко тестировать; всё, что живёт во времени
(соединения, фон, подписки, пулы), — супервизируемые акторы. Фреймворк
не прячет акторы за колбэками: стриминг и фон пишутся обычным `recv`.

## Дерево процессов

```text
Lookout.Application (Supervisor, :one_for_one)
├── Lookout.Repo            (Sql-пул: N акторов-соединений + диспетчер)
├── Calmar.PubSub           (register :pubsub)
├── Lookout.Monitors.Sup    (Supervisor: по актору-чекеру на монитор)
│   ├── Checker #1
│   └── Checker #N
└── Calmar.Endpoint         (Supervisor)
    ├── HttpServer-порт     (Go net/http: соединения, TLS, HTTP/1.1+HTTP/2, keep-alive)
    └── Request ×K          (temporary: актор на запрос)
```

Транспорт — Go `net/http` (решение B2): соединения, keep-alive, TLS,
HTTP/2 и разбор протокола живут в Go, **вне** однопоточного run-loop, на
других ядрах. На каждый запрос порт присылает сообщение, `Endpoint`
спавнит **актор запроса**: он собирает `Conn`, исполняет pipeline в
`trap` и командами порта пишет ответ. Падение handler'а = 500, остальные
запросы не затронуты. Лимит одновременных запросов держит Go-сторона
(сверх лимита — `503` до Brig), таймауты по умолчанию безопасные.

Бонус shared-heap: скомпилированная таблица маршрутов и конфиг
передаются каждому актору запроса при `spawn` **без копирования**.

## `Conn`

`Conn` и протокол плагов живут в **stdlib `Http`** — общий слой с
микро-фреймворком Whelk (Q-micro): плаги и обработчики переносимы между
ними, приложение на Whelk может вырасти в Calmar без переписывания.

Номинальная запись; всё изменение — через функции модуля `Conn`,
возвращающие новую запись.

```text
Conn{
    method: Atom,            # :get, :post ...
    path: Str, segments: List<Str>,
    query: Str, headers: List<(Str, Str)>, body: Bytes,
    params: Map<Str, Any>,   # path + query + body, строковые ключи
    assigns: Map<Atom, Any>, # данные между плагами (текущий пользователь …)
    status: Option<Int>, resp_headers: List<(Str, Str)>,
    resp: Option<Body>,      # Some(...) ⇒ ответ задан ⇒ конвейер остановлен
    remote: (Str, Int), request_id: Str,
    port: Port,              # порт запроса (02/R2): тело, запись, (:http_closed, port)
    trace: Trace,            # контекст трейса, пробрасывается явно (Q-trace, 13)
}
```

## Плаги и конвейер

Плаг — функция `Conn -> Conn`. Конвейер — список плагов; раннер
вызывает их по порядку и **останавливается, как только `conn.resp`
стал `Some`** (ответ задан — дальше идти незачем). Никакого отдельного
флага `halted` и никаких колбэков (Q-halt).

```brig
pub fn require_token(conn) -> authorize(conn, Conn.bearer(conn), Config.get(:api_token))

fn authorize(conn, Some(token), Some(expected)) ->
    if Crypto.secure_compare(token, expected) then conn else deny(conn)
fn authorize(conn, _, _) -> deny(conn)

fn deny(conn) -> conn |> Conn.json(401, %{ "error" => "unauthorized" })
```

## Роутер — данные, а не DSL

Без макросов маршруты — значение, собранное функциями:

```brig
pub fn routes() ->
    [
        Router.scope("/", :browser, [
            Router.get("/",             MonitorController.index),
            Router.get("/monitors/:id", MonitorController.show),
        ]),
        Router.scope("/api", :api, [
            Router.get("/monitors/:id/events", Api.events),
        ]),
    ]
```

Пайплайны (`:browser`, `:api`) — мапа «имя → список плагов» из
`pipelines()`. `Router.compile(routes())` на старте строит префиксное дерево
по сегментам; ошибки (дубли, конфликт `:id` vs `new`) — на старте, до
`listen`. Хелпер путей ключуется **функцией-действием**, а не строковым
именем: `Router.path(MonitorController.show, m)` → `/monitors/42`
(параметр — через `to_param`, L6); опечатка ловится компилятором.
Сахар `Router.resources("/monitors", MonitorController, [:index, :show])`
раскрывается в те же маршруты и требует модуль-значение (03/L5).
Полный пример — `demo/lib/lookout_web/router.brig`.

## Контроллеры

Модуль с `pub fn action(conn, params) -> Conn`. Ошибки — `Result`, общий
`Calmar.Fallback` превращает `Error((:not_found, _))` в 404 и т.д. (03/L14).
Контроллер **возвращает** либо `Conn`, либо `Result<Conn, E>`; раннер
разворачивает `Ok(conn)` и передаёт `Error(e)` в fallback. Это даёт `with`
в контроллерах без шума.

## Фронтенд (C8)

MVP: статика + import maps без JS-сборщика (путь Rails 7); `brig build`
делает кеш-бастинг ассетов (хеш в имени файла). Стратегически — серверный
UI: актор на сессию браузера держит состояние и шлёт diff HTML по
WebSocket (как Phoenix LiveView) — модель акторов Brig подходит для этого
идеально, JS почти не нужен.

## Шаблоны

Файлы `.html.bt` / `.txt.bt` компилирует тулчейн `brig` в функции модуля
(Q-tpl). Полный формат — [15-views.md](15-views.md): объявленные параметры,
теги-компоненты, экранирование по контексту, компиляция в «статика +
динамика», `:if`/`:else`, `:case`/`:of`, строгие проверки при сборке,
лэйаут в пайплайне.

## Данные

- `Sql` — интерфейс; `Sqlite` встроен. В MVP запросы — SQL-строки с
  параметрами: предсказуемо, отлаживаемо, без магии. После MVP — пакет
  data mapper в духе Ecto (запрос — значение, явный `preload`, [14](14-data.md)).
- `Repo.all(Monitor, sql, params)` маппит строки в записи через
  `Type.fields` (03/L11).
- Правила данных — `validate/1` в модуле типа из `Check`-комбинаторов (09);
  `Json.decode_as(Monitor, body)` проверяет форму и правила на границе.
- `Changeset` — ввод из форм поверх той же пары «форма + `validate/1`»:
  `params → Result<Record, (:invalid, changeset)>`; `Form` — хелперы шаблонов
  (`Form.value`, `Form.errors`).
- `Params` — приведение строковых params: `Params.int`, `Params.bool`, …
  → `Result`, ошибка превращается fallback'ом в 400.
- Миграции — модули с `pub fn up()`/`down()`, возвращающими SQL;
  `calmar db migrate` применяет в транзакции.

## Фон и real-time

- Фон — обычные супервизируемые акторы (не нужны Sidekiq/Redis/Oban в MVP);
  после MVP — пакет с персистентной очередью в той же БД (C5).
- Почта — пакет `Mailer` (C3), Calmar даёт интеграцию с шаблонами.
- Загрузки файлов — `Upload`-записи из `Http.Multipart` (C2).
- `PubSub.subscribe(:pubsub, topic)` / `PubSub.broadcast(:pubsub, topic, msg)` —
  подписчик получает обычное сообщение в ящик; подписки снимаются по `:down`.
- **SSE-хендлер — это `recv`-цикл в акторе запроса** (см. demo): никакого
  async, каналов и колбэков. WebSocket — тот же паттерн (S).
- LiveView-подобное (актор на сессию, diff HTML по WebSocket) — после MVP,
  стратегическая цель (C8).

## Безопасность по умолчанию

Пайплайн `:browser` из генератора: подписанные cookie-сессии (HMAC),
CSRF-токен, secure headers, лимит тела, таймауты чтения заголовков/тела,
`Html.Safe`-экранирование. Секреты — только из env/конфига, `calmar new`
генерирует `SECRET_KEY_BASE` в `.env` для dev.

## Конвенция: атомы или варианты (Q-status)

Статусы, теги, виды событий — **атомы** (`:up`, `:down`) с
`Check.one_of` в `validate/1`: просто отображаются в JSON и SQL,
естественны в сообщениях. **Вариантные типы** — для данных с полями
(`Result`, `type Payment { Card(CardInfo), Invoice(InvoiceInfo) }`).

## Поддерживаемость и расширяемость — принципы

1. **Никакого глобального изменяемого состояния**: только значения и
   акторы с именами. Всё, от чего зависит функция, видно в её аргументах.
2. **Соглашения, но без магии**: структура каталогов и имена модулей
   предсказуемы (генераторы), но связи явные — маршрут ссылается на
   функцию, а не на строку `"monitors#index"`.
3. **Расширение = функция или актор**: новый плаг — функция, новая
   интеграция — модуль с конвенционными функциями (адаптер), новый фон —
   актор под супервизором. Нет наследования, нет `before_action`-колбэков.
4. **Тестируемость как архитектурное требование**: pipeline чистый →
   `Calmar.Test.get(app, "/monitors", opts)` работает в памяти без сокетов;
   время — фейковое (02/R9).
5. **Ошибки — значения** на границах домена, `raise` — только для
   невозможного; каждый запрос — изолированный актор.

## Структура проекта (генератор)

```text
lookout/
├── project.brig
├── config/{config,dev,test,prod}.brig
├── lib/lookout/…            # домен: без HTTP
├── lib/lookout_web/…        # веб: роутер, контроллеры, шаблоны, плаги
├── priv/{static,migrations}/
├── scripts/                 # серверные скрипты на том же коде
└── test/
```

`calmar new --api` (Q-calmar-api) — тот же каркас без `templates/`,
`priv/static/`, сессий и CSRF; с БД, миграциями, контрактами и экспортом
OpenAPI.
