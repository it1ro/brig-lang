# Lookout — uptime-монитор на Calmar (фантазийный проект)

Написан так, будто Brig 0.6 и Calmar 0.1 уже вышли. Каждое место, где нужна
несуществующая фича, помечено в заголовке файла строкой `# NEEDS: …` со
ссылкой на требование (02/R*, 03/L*, 04, 05, 06, Q-* в 07).

## Что делает

- Хранит список URL, каждые N секунд проверяет их, пишет историю в SQLite.
- HTML-интерфейс (список, карточка, форма) и JSON API с токеном.
- Живое обновление статуса в браузере через SSE.
- CLI-скрипт импорта мониторов из CSV.
- Собирается в один исполняемый файл вместе с БД-миграциями и статикой.

## Как выглядит работа

```text
$ brig install brig.dev/calmar          # один раз: ~/.local/bin/calmar
$ calmar new lookout                    # скаффолд проекта
$ cd lookout && calmar server           # http://localhost:4000, перезапуск при сохранении
$ brig test                             # без сети и sleep: фейковое время, :memory: БД
$ brig build --target linux/arm64 -o lookout
$ scp lookout pi@edge-01: && ssh pi@edge-01 'LOOKOUT_DB=/var/lib/lookout.db ... ./lookout'
$ ./scripts/import_monitors.brig list.csv --api http://edge-01:8080
```

## Карта файлов

| Файл | Что показывает |
|---|---|
| `project.brig` | манифест как Brig-модуль (Q-manifest) |
| `config/config.brig` | слои конфига записями + env |
| `lib/main.brig` | `fn main()`, graceful shutdown = `recv` сигнала |
| `lib/lookout/application.brig` | дерево супервизии |
| `lib/lookout/monitors.brig` | контекст домена, `with` по `Result`, транзакция |
| `lib/lookout/monitors/monitor.brig` | запись, changeset, `to_json` по конвенции |
| `lib/lookout/monitors/checker.brig` | **актор на монитор**: таймер, `call`, изоляция падений |
| `lib/lookout/monitors/supervisor.brig` | динамический супервизор, реестр имён |
| `lib/lookout/repo.brig` | SQL без ORM, маппинг в записи |
| `lib/lookout_web/router.brig` | маршруты — данные (17): `resources`, `{id: Int}`, вложенные маршруты |
| `lib/lookout_web/plugs.brig` | плаг = `Conn -> Conn` |
| `lib/lookout_web/controllers/…` | HTML/JSON, ошибки через fallback |
| `lib/lookout_web/controllers/api/monitor_controller.brig` | **SSE как `recv`-цикл** |
| `lib/lookout_web/templates/**/*.html.bt` | шаблоны `.bt` (15): параметры, `:if`/`:else`, лэйаут |
| `lib/lookout_web/components/*.html.bt` | компоненты `<StatusBadge>` (`:case`/`:of`) и `<Field>` |
| `priv/migrations/…` | миграции модулями |
| `test/…` | юнит-тесты домена и in-memory интеграционные тесты |
| `scripts/import_monitors.brig` | серверный скрипт на shebang, общий код с приложением |
| `tasks/db/seed.brig` | задача проекта `brig task db.seed` — аналог rake (16) |

## Чем это лучше аналога на Rails/Phoenix/Go

- **Нет инфраструктуры вокруг:** нет Redis/Sidekiq/cron для проверок — это
  акторы под супервизором; нет Postgres для старта — встроенный SQLite;
  нет рантайма на хосте — один файл.
- **Real-time без отдельной подсистемы:** SSE — 15 строк `recv`, без
  Channels/ActionCable/горутин с каналами и мьютексами.
- **Отказоустойчивость по умолчанию:** зависший/упавший чекер или запрос
  не трогает остальных; супервизор перезапускает.
- **Тестируемость:** обработчики — чистые функции над `Conn`; интеграционные
  тесты с фейковым временем детерминированы.
- **Один язык везде:** манифест, конфиг, миграции, скрипты, тесты.

## Что выяснилось, пока писали (находки для требований)

1. Без `await(ref)` (02/R12) чекер не может синхронно звать HTTP-клиент:
   `recv` клиента съест `:tick`. Самая важная находка.
2. Модули/типы как значения нужны почти в каждом файле (03/L5, L19).
3. `m["k"]` → `Option` делает навигацию по JSON многословной (03/L16).
4. `_ <-` в `with` незаметно глотает `Error` (03/L17) — ошиблись трижды.
5. Непонятно, сохраняет ли `{ ..m, f: v }` номинальный вид (03/L18).
6. Периодические задачи нельзя строить на `recv … after` (02/R6).
7. `Config.get` из любого актора требует чего-то вроде `Global` (02/R13).
8. Идиома «let it crash» (`Ok(_) = …`) упирается в незаданную семантику
   опровержимого паттерна в `=` (03/L21).

## Проверка текущим парсером

`bin/brig check` по всем 18 `.brig`-файлам Calmar-демо и `demo-whelk/hook.brig`
(с заменой `pub fn` → `fn`): **15 из 19 разбираются без ошибок** (компилятор
не запускался: модулей Calmar нет). Оставшиеся 4 падают только на ещё не
реализованном синтаксисе, который уже решён:
- многострочные строки `"""` — L9 (`monitors.brig`, миграция);
- блочная лямбда `fn ->` в аргументах вызова — L22 (#169) и offside внутри
  скобок (#2) (оба теста).

Шаблоны `.bt` — отдельный формат (15), парсером Brig не проверяются.
