# Лог настройки доски — PROMPT_SETUP_KANBAN.md

- **Дата:** 2026-09-25, 22:43–23:10 (UTC+4)
- **Коммиты:** `iter/regvm` @ `8ab58cf7688dc5f74a30639e92ff03002bd2fb83`, `main` @ `41bbb704829c16a84a7d1928694a6580e5e6d399`
- **Репозиторий:** `it1ro/brig-lang`
- **Проект:** №5 «Brig — аудит и слияние RegVM», `PVT_kwHOAoQ_k84Bksrz`, https://github.com/users/it1ro/projects/5 — привязан к репозиторию, описание и README на русском
- **Источник тикетов:** `TASKS.md` (39 задач в волнах + 4 design-decision)

## Поля проекта

| Поле | Значения | Примечание |
|---|---|---|
| Status | Todo, In Progress, In Review, Blocked, Done | Встроенное поле; `In Review` и `Blocked` добавлены через GraphQL `updateProjectV2Field` |
| Priority | P0, P1, P2, P3 | |
| Task type | fail-fast, full-fix, test-infra, docs, merge | Имя `Type` GitHub отверг: `Name cannot have a reserved value` (зарезервировано под встроенные issue types) |
| Effort | S, M, L | |
| Model | sonnet, opus, human | |
| Wave | 0-branch, 1-test-infra, 2-small, 3-major, 4-blockers, 5-docs | |
| Sprint | Iteration, старт в понедельник 2026-09-28, 14 дней, Sprint 1–3 | Создано через GraphQL `createProjectV2Field` (ITERATION) — в UI у пользователя не получилось |

Status выставлен так: `Blocked`, если у задачи есть `depends_on` (33 шт.), иначе `Todo` (6 шт.). Sprint не проставлен ни одной задаче.

## Labels

Созданы 24 labels (описания на русском): `audit`, `blocker`, `P0`–`P3`, `false-positive`, `design-decision`, `verification`, `epic`, `fail-fast`, `full-fix`, `test-infra`, `docs`, `merge`, `wave-0-branch`, `wave-1-test-infra`, `wave-2-small`, `wave-3-major`, `wave-4-blockers`, `wave-5-docs`, `model-sonnet`, `model-opus`, `model-human`.

Issue получают: `audit`, `<type>`, `<priority>`, `wave-<wave>`, `model-<model>`, плюс `blocker` для P0, `verification` для T-13…T-15, `design-decision` для T-90…T-93. Зависимости — строками `> Blocked by #N` в начале body (формат `CONTRIBUTING.md`).

## Задачи

| T-NN | issue# | URL | wave | status |
|---|---|---|---|---|
| T-01 | #1 | https://github.com/it1ro/brig-lang/issues/1 | 0-branch | Todo |
| T-02 | #2 | https://github.com/it1ro/brig-lang/issues/2 | 0-branch | Todo |
| T-03 | #3 | https://github.com/it1ro/brig-lang/issues/3 | 0-branch | Todo |
| T-04 | #4 | https://github.com/it1ro/brig-lang/issues/4 | 0-branch | Todo |
| T-05 | #5 | https://github.com/it1ro/brig-lang/issues/5 | 0-branch | Todo |
| T-06 | #6 | https://github.com/it1ro/brig-lang/issues/6 | 0-branch | Todo |
| T-07 | #7 | https://github.com/it1ro/brig-lang/issues/7 | 0-branch | Blocked |
| T-10 | #8 | https://github.com/it1ro/brig-lang/issues/8 | 1-test-infra | Blocked |
| T-11 | #9 | https://github.com/it1ro/brig-lang/issues/9 | 1-test-infra | Blocked |
| T-12 | #10 | https://github.com/it1ro/brig-lang/issues/10 | 1-test-infra | Blocked |
| T-13 | #11 | https://github.com/it1ro/brig-lang/issues/11 | 1-test-infra | Blocked |
| T-14 | #12 | https://github.com/it1ro/brig-lang/issues/12 | 1-test-infra | Blocked |
| T-15 | #13 | https://github.com/it1ro/brig-lang/issues/13 | 1-test-infra | Blocked |
| T-20 | #14 | https://github.com/it1ro/brig-lang/issues/14 | 2-small | Blocked |
| T-21 | #15 | https://github.com/it1ro/brig-lang/issues/15 | 2-small | Blocked |
| T-22 | #16 | https://github.com/it1ro/brig-lang/issues/16 | 2-small | Blocked |
| T-23 | #17 | https://github.com/it1ro/brig-lang/issues/17 | 2-small | Blocked |
| T-24 | #18 | https://github.com/it1ro/brig-lang/issues/18 | 2-small | Blocked |
| T-30 | #19 | https://github.com/it1ro/brig-lang/issues/19 | 3-major | Blocked |
| T-31 | #20 | https://github.com/it1ro/brig-lang/issues/20 | 3-major | Blocked |
| T-32 | #21 | https://github.com/it1ro/brig-lang/issues/21 | 3-major | Blocked |
| T-33 | #22 | https://github.com/it1ro/brig-lang/issues/22 | 3-major | Blocked |
| T-34 | #23 | https://github.com/it1ro/brig-lang/issues/23 | 3-major | Blocked |
| T-35 | #24 | https://github.com/it1ro/brig-lang/issues/24 | 3-major | Blocked |
| T-36 | #25 | https://github.com/it1ro/brig-lang/issues/25 | 3-major | Blocked |
| T-37 | #26 | https://github.com/it1ro/brig-lang/issues/26 | 3-major | Blocked |
| T-38 | #27 | https://github.com/it1ro/brig-lang/issues/27 | 3-major | Blocked |
| T-39 | #28 | https://github.com/it1ro/brig-lang/issues/28 | 3-major | Blocked |
| T-40 | #29 | https://github.com/it1ro/brig-lang/issues/29 | 3-major | Blocked |
| T-41 | #30 | https://github.com/it1ro/brig-lang/issues/30 | 3-major | Blocked |
| T-42 | #31 | https://github.com/it1ro/brig-lang/issues/31 | 3-major | Blocked |
| T-43 | #32 | https://github.com/it1ro/brig-lang/issues/32 | 3-major | Blocked |
| T-50 | #33 | https://github.com/it1ro/brig-lang/issues/33 | 4-blockers | Blocked |
| T-51 | #34 | https://github.com/it1ro/brig-lang/issues/34 | 4-blockers | Blocked |
| T-52 | #35 | https://github.com/it1ro/brig-lang/issues/35 | 4-blockers | Blocked |
| T-53 | #36 | https://github.com/it1ro/brig-lang/issues/36 | 4-blockers | Blocked |
| T-54 | #37 | https://github.com/it1ro/brig-lang/issues/37 | 4-blockers | Blocked |
| T-60 | #38 | https://github.com/it1ro/brig-lang/issues/38 | 5-docs | Blocked |
| T-61 | #39 | https://github.com/it1ro/brig-lang/issues/39 | 5-docs | Blocked |
| T-90 | #40 | https://github.com/it1ro/brig-lang/issues/40 | — | не на доске (design-decision) |
| T-91 | #41 | https://github.com/it1ro/brig-lang/issues/41 | — | не на доске (design-decision) |
| T-92 | #42 | https://github.com/it1ro/brig-lang/issues/42 | — | не на доске (design-decision) |
| T-93 | #43 | https://github.com/it1ro/brig-lang/issues/43 | — | не на доске (design-decision) |

Проверка после создания: `gh project item-list 5` — 39 items, все значения Priority / Task type / Effort / Model / Wave / Status совпадают с meta-блоками `TASKS.md`; design-decision issues и эпиков на доске нет; дублей issues нет.

## Эпики (не на доске)

| Эпик | issue# | URL | задач |
|---|---|---|---|
| Эпик: Wave 0 — на ветке iter/regvm до слияния | #44 | https://github.com/it1ro/brig-lang/issues/44 | 7 |
| Эпик: Wave 1 — тесты и разблокировка | #45 | https://github.com/it1ro/brig-lang/issues/45 | 6 |
| Эпик: Wave 2 — небольшие исправления слоя 1 | #46 | https://github.com/it1ro/brig-lang/issues/46 | 5 |
| Эпик: Wave 3 — крупные исправления | #47 | https://github.com/it1ro/brig-lang/issues/47 | 14 |
| Эпик: Wave 4 — полная реализация blocker-ов | #48 | https://github.com/it1ro/brig-lang/issues/48 | 5 |
| Эпик: Wave 5 — документация | #49 | https://github.com/it1ro/brig-lang/issues/49 | 2 |

## Упавшие команды

| Команда | Ошибка | Итог |
|---|---|---|
| `gh project field-create 5 --name "Type"` | `GraphQL: Name cannot have a reserved value, Name has already been taken (createProjectV2Field)` | Поле создано как `Task type` |
| `gh project field-create 5 --name "Task type"` (1-я попытка) | `unknown owner type` | Повтор прошёл |
| `gh issue create` T-02 | `Post "https://api.github.com/graphql": net/http: TLS handshake timeout` | Issue не создан; повтор прошёл (#2) |
| `gh issue create` T-39 | `TLS handshake timeout` | Повтор прошёл (#28), дубля нет |
| `gh project item-edit` (2 раза) | `TLS handshake timeout` | Повтор прошёл |
| `gh project item-add` T-07 | `unknown owner type` | Повтор прошёл |
| `gh project item-edit` T-32, поле Model | `non-200 OK status code: 499 body: ""` | Повтор прошёл |

## Ручные действия пользователя

- Создать Iteration-поле `Sprint` в UI не получилось — поле создано через GraphQL API, ручных действий не осталось.
- Sprint задачам не проставлен — это делает пользователь вручную.

## Расхождения с `CONTRIBUTING.md`

- Поле доски называется `Task type`, а не `Type` (имя зарезервировано GitHub). `CONTRIBUTING.md` не редактировался.
