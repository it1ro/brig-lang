# Как мы работаем с репозиторием

Этот документ — единственное место с правилами. Если правило меняется,
меняется здесь, и на это есть PR с меткой `docs`.

Не дублируем: правила языка — в `docs/01-language-design.md`, дизайн
VM — в `docs/02-register-based-virtual-machine.md`. Задачи ведутся и ищутся только
на Kanban-доске (GitHub Projects v2); `TASKS.md` — её локальная проекция (план волн,
зависимости, DoD, без статусов), при расхождении права доска.

## 1. TL;DR

1. Берёшь issue с доски, переводишь в In Progress.
2. Создаёшь от `main` ветку `<type>/<T-NN>-<slug>`.
3. Открываешь PR в `main` с `Closes #<issue>` в body.
4. `make all` и `BRIG_VERIFY=1 go test ./...` зелёные — squash-merge.
5. Ветка удалена, issue в Done.

Каждая сессия начинается с одного issue и заканчивается зелёным CI.

## 2. Issue и доска

- Доска: `gh project list --owner it1ro`, затем `gh project view <N> --owner it1ro --web`.
- Следующая задача — из Todo: сначала меньший Wave, внутри него — выше Priority (P0 первым).
  Задачи, которых нет на доске, не берутся в работу.
- Новый issue сразу добавляется на доску: `gh project item-add <N> --owner it1ro --url <issue-url>`.
- Один issue = одна сессия. Не помещается в Effort L — дели на несколько issue.
- Обязательные поля: Type, Effort, Model, Wave, Priority.
- Label происхождения: `audit` — finding из `AUDIT_REPORT.md`, `spec-gap` — пробел реализации относительно спеки (§16). Задача без одного из них не берётся.
- Статус задачи — только на доске. `TASKS.md` хранит план, зависимости и DoD без статусов.
- Зависимость — строкой в body, не label: `Blocked by #42`.
- Issue остаётся в Blocked, пока все issue из `Blocked by` не закрыты.

## 3. Ветки

- Формат: `<type>/<T-NN>-<slug>`, `type` — из таблицы п. 4. Примеры:
  `fix/T-14-newline-required`, `feat/T-30-multiclause-fn`, `test/T-10-audit-suite`.
- Ветка создаётся от свежего `main`, живёт не дольше одной сессии и squash-мержится в `main`.
- После merge удаляется локально и на origin:
  `git branch -D fix/T-14-newline-required && git push origin --delete fix/T-14-newline-required`.
- **Исключение — integration-ветка** `iter/<short-name>` (прецедент: `iter/regvm`), только
  когда изменение нельзя разбить на PR. До старта — baseline-тег на `main`:
  `git tag baseline/regvm main && git push origin baseline/regvm`. В `main` попадает одним
  squash-коммитом. Срок жизни ≤ 2 недели; не укладывается — дроби на обычные ветки.

## 4. Коммиты

Формат: `<type>(<scope>): <subject> [T-NN]`.

| type       | когда                                      |
| ---------- | ------------------------------------------ |
| `feat`     | новая возможность языка, CLI, REPL         |
| `fix`      | исправление ошибки                         |
| `refactor` | изменение структуры без изменения поведения |
| `test`     | тесты, golden-файлы, тест-инфраструктура   |
| `docs`     | `docs/`, `README.md`, этот файл            |
| `chore`    | служебное: конфиги, зависимости, чистка    |
| `perf`     | ускорение без изменения поведения          |
| `build`    | `Makefile`, `go.mod`, сборка               |

- `scope` — пакет или файл: `compiler`, `parser`, `vm`, `lexer`, `ast`, `sema`,
  `repl`, `cmd/brig`, `docs`.
- `[T-NN]` в конце subject обязателен, но не enforced hook-ом: проверяется глазами
  при ревью PR и в squash-сообщении merge-коммита.
- Черновые коммиты внутри integration-ветки — без `[T-NN]`. Требование действует
  для всего, что попадает в `main`.
- Body — для неочевидного контекста. Title в body не повторяется.

```
fix(parser): require NEWLINE between statements [T-14]

Grammar: stmt_list ::= stmt { NEWLINE stmt }. Раньше `x = 1 y = 2`
разбирался как два стейтмента молча.
```

## 5. Pull requests

- Из `<type>/<T-NN>-<slug>` в `main`. Title — формат коммита (п. 4).
- Body, минимум:

```
Closes #<issue>

Что сделано:
- …

Что НЕ сделано в этом PR:
- …

Как проверялось:
- make all
- BRIG_VERIFY=1 go test ./...
```

- Перед merge зелёные: `make all` и `BRIG_VERIFY=1 go test ./...`.
- **Golden-файлы.** PR меняет AST — `make update-golden`, bytecode — `make update-bytecode`.
  Diff просмотрен глазами и вынесен в отдельный коммит того же PR: `test(parser): regenerate golden files [T-14]`.
- **Апрувы.** Сейчас не нужны, self-merge при зелёном CI. Минимум один апрув от
  человека, не автора PR (team: 2+). Мерж без апрува по таймауту молчания запрещён.
- **Стратегия merge.** Squash по умолчанию. Rebase-merge — когда коммиты PR семантически
  независимы. Merge-commit не используется: integration-ветки тоже squash (п. 3).
- **Прямой push в `main`** сейчас разрешён. Branch protection в GitHub Settings → Branches включается (team: 2+).

## 6. Definition of Ready / Definition of Done

- **DoR:**
  - заполнены Type, Effort, Model, Wave, Priority;
  - указан тест-якорь: существующий (`internal/parser/parser_test.go`) или «создать»;
  - DoD в issue проверяемый: «`make test-parser` возвращает 0», а не «парсер работает лучше».
- **DoD:**
  - CI зелёный, тесты из issue зелёные;
  - issue закрыт и в Done;
  - изменён публичный синтаксис или API — раздел в `docs/` обновлён в том же PR.
- `CHANGELOG.md` в DoD не входит: генерируется через `make changelog` (git-cliff), в PR не правится.

## 7. Сессии с LLM-агентом

- Сессия начинается с одного issue с доски (п. 2): `gh issue view <N> --json title,body`.
- Effort S/M — любая модель; L — только модель из поля `Model`.
- Один тикет за сессию. Найденное по пути — новый issue на доске, не правка в этой сессии.
- Doc-файлы агент меняет только в тикетах с Type `docs`.
- Finding не воспроизводится — issue в Blocked или закрывается как false-positive.
  «Фикс на всякий случай» запрещён.
- Сессия начинается с перевода issue в In Progress и заканчивается squash-merge PR
  и переводом issue в Done. Между сессиями ничего не остаётся в промежуточном состоянии.

## 8. Мелкие правила

- Коммиты атомарные: рефакторинг и фикс — разные коммиты (`refactor(vm): …`, затем `fix(vm): …`).
- Красный CI — не мержим, даже если «локально работает».
- `gofmt -l .` пуст перед PR.
- Не коммитим `bin/`, `coverage.out`, временные `.brig` из сессий; перед PR — `git status` чист от них.
- PR затрагивает больше двух пакетов — в issue записано, почему одним PR.
