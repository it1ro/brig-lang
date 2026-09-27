# 17. Роутер — аналог `routes.rb` (утверждено 2026-09-27)

Маршруты — **данные**: `routes()` возвращает список, собранный функциями
`Router.*`; макросов нет (§16). `Router.compile` на старте строит
префиксное дерево по сегментам.

## Решения

| Вопрос | Решение |
|---|---|
| Ресурсы | `Router.resources(path, Controller, [действия], [вложенные])`. Набор — 7 REST-действий; **список действий всегда явный** (нет «всё по умолчанию»). Путь ресурса содержит параметр элемента явно: `"/monitors/{id: Int}"` → коллекция `/monitors`, элемент `/monitors/{id}`. Вложенные маршруты — обычные `Router.get/post/…` и `resources`, прикреплённые к пути элемента. `member`/`collection`/`shallow`/`concerns` не вводятся |
| Параметры пути | **тип в шаблоне**: `{id: Int}` (синтаксис поля записи), `{slug}` без типа = `Str`. Типы: `Int`, `Str`, `Uuid`, атом из списка `{status: [:up, :down]}`. Не приводится — **маршрут не совпал**, поиск идёт дальше; в действие приходит уже `Int`. Поэтому `/monitors/new` и `/monitors/{id: Int}` различаются без правил приоритета |
| Хелперы путей | ключ — **функция-действие**: `Router.path(CheckController.index, [monitor])` — параметры по порядку сегментов, каждый через `to_param` (L6); запрос — `{ query: { page: 2 } }`; `Router.url(…)` — абсолютный URL. Неверное число параметров — raise с подсказкой ожидаемого шаблона |
| Монтирование | `Router.forward(prefix, handler)` — любой `Conn -> Conn` (роутер пакета, приложение Whelk, веб-вид `brig observe`); префикс снимается из `conn.segments`. Работает благодаря общему `Conn` в `Http` (Q-micro) |
| Методы | `HEAD` обслуживается `GET`-маршрутом без тела; `OPTIONS` и `405` с заголовком `Allow` строятся из таблицы; явный маршрут перекрывает автоматику |
| Завершающий слэш | канон — без слэша; `/monitors/` → `308` на `/monitors` (метод и тело сохраняются) |
| Проверки | на старте и в `brig check`: конфликты, недостижимые маршруты, действие не `pub fn`, неизвестный пайплайн, неизвестный тип параметра |
| Инструменты | `brig task calmar.routes` — таблица маршрутов (метод, шаблон, действие, пайплайны); `Router.match(table, :get, "/monitors/42")` — чистая функция для юнит-тестов |
| Пайплайны | у скоупа — **список**: `Router.scope("/admin", [:browser, :require_admin], [...])`; вложенный скоуп добавляет свои к родительским, порядок — снаружи внутрь. Одиночный атом — сахар для списка из одного |

## Пример

```brig
pub fn routes() ->
    [
        Static.serve("/assets", "priv/static"),

        Router.scope("/", :browser, [
            Router.get("/", MonitorController.index),
            Router.resources("/monitors/{id: Int}", MonitorController, [:new, :create, :show]),
        ]),

        Router.scope("/api", :api, [
            Router.resources("/monitors/{id: Int}", Api, [:index, :create], [
                Router.post("/check", Api.check_now),     # POST /api/monitors/{id}/check
                Router.get("/events", Api.events),        # GET  /api/monitors/{id}/events (SSE)
            ]),
        ]),

        Router.scope("/ops", [:browser, :require_admin], [
            Router.forward("/observe", Observe.Web.call),
        ]),
    ]
```

Действие получает уже приведённые параметры:

```brig
pub fn show(conn, %{ "id" => id }) ->       # id: Int
    with
        Ok(m) <- Monitors.get(id)
        conn |> Conn.render(T.show, { monitor: m })
```

## REST-действия `resources`

| Действие | Метод и путь |
|---|---|
| `:index` | `GET /monitors` |
| `:new` | `GET /monitors/new` |
| `:create` | `POST /monitors` |
| `:show` | `GET /monitors/{id}` |
| `:edit` | `GET /monitors/{id}/edit` |
| `:update` | `PATCH /monitors/{id}` (и `PUT`) |
| `:delete` | `DELETE /monitors/{id}` |

## Как устроено

- Таблица маршрутов — неизменяемое значение; после `compile` хранится в
  `Global` (R13) и без копирования доступна каждому актору запроса.
- Сопоставление — по сегментам; внутри узла порядок: статический сегмент →
  типизированный параметр → `Str`-параметр → `forward`.
- Параметры пути сливаются в `conn.params` со строковыми ключами
  (`"id" => 42`), рядом с query и телом.
