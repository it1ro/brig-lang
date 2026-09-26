---
name: brig-workflow
description: >
  Протокол рабочей сессии в репозитории Brig: взять issue с Kanban-доски
  (GitHub Projects v2, it1ro/brig-lang, проект 5), ветка, коммиты [T-NN],
  PR, статусы, зависимости Blocked by, t.Skip("blocked: T-NN"), что делать
  с находками по пути. Использовать в начале и в конце ЛЮБОЙ задачи,
  которая приходит как issue, номер T-NN или ссылка на TASKS.md, а также
  при создании новых issues.
---

# Brig — протокол сессии

Правила — `CONTRIBUTING.md` (§2 доска, §3 ветки, §4 коммиты, §5 PR, §6 DoR/DoD, §7 LLM-сессии). Пошаговые команды для людей — `MAINTAINING.md`. Здесь — то, что агент делает сам.

## Вход

- Работа начинается только с issue, у которого label `audit` (finding аудита) или `spec-gap` (пробел относительно спеки §16) и карточка на доске 5 в статусе **Todo**. Нет issue — сначала создать (раздел «Новый issue»), не работать «просто так».
- Прочитать: `gh issue view <N> --repo it1ro/brig-lang --json title,body,labels`. Body — это контракт: **Файлы**, **Тест-якорь**, **DoD**, **НЕ делать**, строки `> Blocked by #M`.
- Проверить, что все `Blocked by` закрыты: `gh issue view <M> --json state`. Хоть один открыт — стоп, сообщить.
- Label `design-decision` или `human` — не брать, это работа человека.
- Effort large — работать только моделью из поля Model (`opus` и т.п.).
- Подтянуть skills затронутых подсистем (`brig-compiler`, `brig-vm`, …) и соответствующий finding в `AUDIT_REPORT.md` по ID из meta (`findings: [...]`).

## Старт

1. Статус карточки → **In Progress** (`gh project item-edit`; функции `board_set` — в `MAINTAINING.md` §3).
2. Ветка от свежего `main`: `<type>/<T-NN>-<slug>`, `type` — из таблицы `CONTRIBUTING.md` §4 (`fix`, `feat`, `test`, `docs`, `refactor`, `chore`, `perf`, `build`).
3. **Исключение — Wave 0 (label `wave-0`, T-01…T-06):** работа идёт прямо в `iter/regvm`, без отдельной ветки и PR; T-07 (merge) делает человек.

## Работа

- Сначала тест-якорь: написать тест из поля «Тест-якорь» и убедиться, что он **падает** на текущем коде (или снять `t.Skip("blocked: T-NN")` со своего теста и увидеть падение). Потом фикс.
- Пробных программ `p/*.brig` из `AUDIT_REPORT.md` в репозитории нет — восстановить минимальную программу по описанию finding и оформить тестом.
- DoD выполняется дословно; «НЕ делать» — жёсткий запрет, включая соседние findings.
- Коммиты атомарные: `<type>(<scope>): <subject> [T-NN]`; рефакторинг и фикс — разными коммитами; обновление golden/bytecode — отдельным коммитом `test(<scope>): regenerate golden files [T-NN]` после чтения diff.
- Doc-файлы (`docs/`, `README.md`, `CONTRIBUTING.md`, `brig.ebnf`) — только в задачах с Task type `docs`. Если фикс требует правки спеки или публичного синтаксиса — стоп: комментарий в issue с нужным разделом, статус **Blocked**.
- Найдено по пути (другой баг, неточность, идея) — новый issue, не правка в этой сессии. «Фикс на всякий случай» запрещён.
- Finding не воспроизводится — комментарий с командами и выводом; issue в **Blocked** или закрыть с label `false-positive`.
- Force-push — только в свою ветку задачи и только `--force-with-lease=<ветка>:<sha>` (`CONTRIBUTING.md` §3): rebase на свежий `main`, перестройка стека PR после squash-merge нижнего. `main`, `iter/*`, теги и ветки других задач — никогда.

## Финиш

1. Прогон по `brig-testing-workflow`: минимум `make all` и `BRIG_VERIFY=1 go test ./...` — оба 0.
2. `rg -n 'blocked: T-NN' internal` — пусто для своего T-NN.
3. `rg -n 'T-NN' .claude/skills` — строки skills, описывающие ограничение, которое задача сняла, поправить в том же PR (skills — рабочие заметки агента, а не doc-файлы).
4. `gofmt -l .` пуст; `git status` чист от временных `.brig`, `bin/`, `coverage.out`.
5. PR в `main`, title = формат коммита, body по шаблону `CONTRIBUTING.md` §5: `Closes #<N>`, «Что сделано», «Что НЕ сделано», «Как проверялось» с реальными командами и результатом. PR трогает больше двух пакетов — объяснить почему.
6. Статус → **In Review**. Режим «со сдачей на ревью» — здесь сессия заканчивается. Автономный режим — после зелёного CI `gh pr merge --squash --delete-branch`.
7. После merge: `gh issue list --search '"Blocked by #<N>" in:body' --state open` — для каждого, у кого все блокеры закрыты, статус → **Todo**.

## Новый issue

- Title `T-NN · <имя>`; T-NN — следующий свободный номер в десятке волны (занятые — `TASKS.md`, `PROMPT_SETUP_KANBAN.log.md`).
- Body по образцу любого существующего issue того же типа: meta-комментарий (priority, type, effort, model, wave, depends_on, findings), **Файлы**, **Тест-якорь** (существующий или «создать»), бинарный **DoD** (команда → результат, без «улучшить»), **НЕ делать** (≥3 пункта). Зависимости — первыми строками `> Blocked by #M`.
- Labels: `<audit|spec-gap>,<task type>,<P>,wave-<N>,<model>`, плюс `blocker` для P0 и `must` для Must-пробела §16. Task type `feature` — реализация фичи из спеки (label `feature`, ветка `feat/`).
- Добавить на доску (`gh project item-add 5 --owner it1ro --url <url>`) и заполнить Priority, Task type, Effort, Model, Wave, Status. Sprint не ставить.

## Что уже известно (не переоткрывать)

- `AUDIT_REPORT.md` — все известные findings; перед «новой» находкой проверить, нет ли её там и нет ли issue.
- A-F1 модель scheduler решён: вариант C (#40) — спека даёт только наблюдаемые гарантии G1–G4 (§15.2). A-F3 truthiness решён: вариант A (#41) — строгий `Bool` в `if` и в обоих операндах `and`/`or`, не-`Bool` → `:type_error`; правый операнд `and`/`or` не хвостовой (T-81 #106 делается, T-82 #107 — won't-fix по ветке A своего DoD). A-F4 решён: вариант A (#42) — все `:type_error` ловимы `trap`, фатальны только `internal:` (T-83 #108; остаток — T-96 #150). I-F8 решён (#43, 1: A, 2: A, 3: A): паттерны строго по виду (T-84 #109), `==`/`<` Int×Float точно (T-85, в main), Decimal×Float — T-86 #111. Код в областях нерешённых DD не менять.
- Задачи T-86 (Wave 3) и T-62 (docs) по I-F8 (#43, решён): issues #111–#112 (T-80, #105 — по A-F1 — делается; T-81 #106 и T-82 #107 — по A-F3; T-83 #108 — по A-F4). После решения — в Todo или won't-fix по их DoD.
- T-60 (#38) переоткрыт: PR #52 шёл в `iter/regvm` и не был вмержен. «Done» на доске проверять по наличию `[T-NN]` в `git log origin/main`.
