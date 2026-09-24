---
name: git-conventional
description: Use when committing, rewriting history, branching, or releasing in this repo. Enforces Conventional Commits (English, imperative), scoped messages, git hooks, and changelog generation.
---

# Git и Conventional Commits

Все коммиты в репозитории — **Conventional Commits**, на **английском
языке**, в **императиве**. Русские сообщения коммитов запрещены (история
переписана под это правило). Этот скилл описывает контракт коммитов и грабли
процесса, а не дублирует `git help`.

## Когда применять / не применять

**Применять:** коммит, amend, rebase, переписывание истории, force-push,
создание ветки, тег релиза, обновление `CHANGELOG.md`, настройка git-хуков.

**Не применять:** правки кода — свои скиллы (`brig-lexer`, `brig-parser`,
...). Здесь Git-процесс, а не содержимое изменений.

## Контракт сообщения

```

<type>(<scope>): <subject>

<body>

<footer>
```

- **`<type>`** — из закрытого списка (см. таблицу ниже).
- **`<scope>`** — из проектного списка (см. ниже), в скобках, обязателен
  для `feat`/`fix`/`refactor`, опционален для `docs`/`test`/`chore`/`ci`/`perf`.
- **`<subject>`** — императив, **строчными буквами**, без точки в конце,
  ≤ 72 символов, **только латиница**.
- **`<body>`** — «что и зачем», контраст со старым поведением. Перенос
  строки после subject.
- **`<footer>`** — `BREAKING CHANGE:` (если ломает API) или ссылки на issue.

## Types

| type       | Когда                                               |
| ---------- | --------------------------------------------------- |
| `feat`     | новая возможность (токен, конструкция, CLI-команда) |
| `fix`      | исправление бага                                    |
| `refactor` | изменение без смены поведения                       |
| `docs`     | только документация (`.md`, `.ebnf`, комментарии)   |
| `test`     | тесты, `testdata`, golden-файлы                     |
| `chore`    | сборка, зависимости, конфигурация                   |
| `ci`       | CI/CD, Makefile-цели                                |
| `perf`     | оптимизация                                         |

## Scopes (проект Brig)

Закрытый список. Новый scope = правка этого скилла и хука.

```
lexer  parser  ast  vm  runtime  prelude  cli  repl
test   tools   docs  ci  build    git
```

## Примеры

**Правильно:**

```
feat(lexer): emit NEWLINE before INDENT per A5.2
fix(parser): reject trap in call argument position (KR-003)
refactor(ast): unify Expr/Stmt visitor interface
docs: sync formalization with v0.4.5 Track A changes
test: add golden files for decimal literals (P-004)
chore(ci): add Go 1.27 to build matrix
```

**Неправильно:**

```
fix lexer: emit NEWLINE        # нет скобок вокруг scope
Fix(lexer): Emit newline       # type и subject с заглавной
feat(lexer): добавить NEWLINE  # кириллица в subject
feat(lexer): emit NEWLINE.     # точка в конце
```

## Инварианты

Это критично. Ломать нельзя без правки этого скилла и хуков.

1. **Язык subject — только латиница.** Кириллица запрещена хуком
   `commit-msg`. История была переписана под это правило; возврат к
   русским коммитам — регресс.
2. **Type — из закрытого списка.** `feature`, `bugfix`, `wip` и прочее —
   отклоняются хуком.
3. **Scope — из закрытого списка.** Новый scope = правка хука + этого
   скилла + `CHANGELOG`-конфига **до** коммита.
4. **Subject — императив, строчными, без точки, ≤ 72.** Хук проверяет
   regex `^(feat|fix|...)(\([a-z]+\))?: [a-z]` и запрет кириллицы.
5. **`main` защищена.** Только PR + зелёный CI. Прямой push в `main`
   невозможен.
6. **Force-push — только `--force-with-lease`.** Никогда `--force`.
7. **Тег релиза — `vX.Y.Z` по SemVer.** CI собирает через goreleaser.
8. **`CHANGELOG.md` генерируется**, не пишется вручную: `make changelog`
   (git-cliff или conventional-changelog).
9. **Скрипты переписывания истории живут в репозитории**, не в `/tmp/`.
   Временные пути (`/tmp/...`) не переживают сессию и не воспроизводимы.

## Хуки

Ставятся один раз:

```sh
make git-hooks
```

Что устанавливается в `.git/hooks/`:

| Хук                  | Действие                                                                               |
| -------------------- | -------------------------------------------------------------------------------------- |
| `prepare-commit-msg` | Подсказка: список type/scope в шаблоне                                                 |
| `commit-msg`         | Валидация через regex; запрет кириллицы в subject; блокирует коммит при несоответствии |
| `pre-push`           | `make test` + `make lint`; без них push блокируется                                    |

`make commit` как обёртка **не вводится** — хуки работают прозрачно для
обычного `git commit`.

## Ветвление

| Ветка                          | Назначение                       |
| ------------------------------ | -------------------------------- |
| `main`                         | Защищена: только PR + зелёный CI |
| `feature/<scope>-<short-desc>` | Новая функциональность           |
| `fix/<scope>-<short-desc>`     | Исправление                      |
| `docs/<scope>-<short-desc>`    | Документация                     |
| Тег `vX.Y.Z`                   | Релиз по SemVer                  |

`<short-desc>` — kebab-case, латиница.

## Changelog и релизы

```sh
make changelog           # git-cliff / conventional-changelog → CHANGELOG.md
git tag v0.1.0           # SemVer-тег
git push origin v0.1.0   # CI собирает через goreleaser
```

`CHANGELOG.md` — **производный артефакт**. Ручные правки затираются
следующим `make changelog`.

## Переписывание истории

Только для **непушенных** коммитов или с **явного согласия** команды.

```sh
# 1. Скрипт живёт в репозитории: tools/git/msg_filter.py
#    (не /tmp/opencode/... — временные пути не воспроизводимы)
git filter-branch --msg-filter 'python3 tools/git/msg_filter.py' -- --all

# 2. Почистить артефакты filter-branch
rm -rf .git/refs/original/
git reflog expire --expire=now --all
git gc --prune=now --quiet

# 3. Force-push с защитой
git push --force-with-lease
```

`--force-with-lease` (не `--force`) проверяет, что удалённая ветка не
уехала вперёд с момента твоего последнего fetch. Это защита от затирания
чужих коммитов.

## Проверка

```sh
# Валидация сообщения вручную (без коммита)
echo "feat(lexer): emit NEWLINE" | .git/hooks/commit-msg /dev/stdin

# Проверка, что хуки установлены
ls -la .git/hooks/prepare-commit-msg .git/hooks/commit-msg .git/hooks/pre-push

# Changelog-превью без записи
make changelog-dry-run    # если есть; иначе — git-cliff --dry-run
```

Перед PR: `make test` и `make lint` локально — `pre-push` сделает то же,
но лучше видеть результат до пуша.

## Частые ошибки

- **Коммит на русском** — отклоняется хуком `commit-msg`. Симптом: коммит
  не проходит, терминал показывает regex-несоответствие. Правится
  сообщением на английском.
- **Non-conventional subject типа `fix lexer`** — отсутствует `(scope):`.
  Симптом: хук отклоняет; либо, если хук не установлен, `make changelog`
  не включает коммит в релиз.
- **Subject длиннее 72 символов** — обрезается `git log --oneline` или
  переносится в body. Симптом: `git log` нечитаем; PR-описания с «висящей»
  частью сообщения.
- **Force-push без `--force-with-lease`** — перезаписывает чужие изменения.
  Симптом: коллега теряет коммиты; в reflog удалённой ветки — твои push'и.
- **Скрипт переписывания истории лежит в `/tmp/`** — не переживает
  перезагрузку и не воспроизводим на CI/другой машине. Симптом: через
  неделю никто не может повторить массовое переписывание. Перенести в
  `tools/git/`.
- **Новый scope добавлен коммитом без правки хука и скилла** — следующий
  коммит с этим scope отклоняется. Симптом: «вчера работало, сегодня хук
  ругается». Правка хука + `git-conventional.md` идёт **до** первого
  коммита с новым scope.
- **Ручная правка `CHANGELOG.md`** — затирается следующим `make changelog`.
  Симптом: после релиза пропали записи, добавленные руками. Все правки —
  через коммиты с conventional-сообщениями.
- **`main` не защищена в настройках репозитория** — прямой push проходит
  локально, но ломает правило. Симптом: в `main` появляются коммиты без
  PR. Проверять branch protection в настройках GitHub.
- **Тег релиза не SemVer** (`v1`, `release-1`) — goreleaser падает или
  сортирует неверно. Симптом: CI релиза красный; `make changelog`
  игнорирует тег.

## Ссылки

- Conventional Commits — <https://www.conventionalcommits.org/>
- SemVer — <https://semver.org/>
- git-cliff — <https://git-cliff.org/>
- goreleaser — <https://goreleaser.com/>
- Скилл `brig-docs` — соглашение об англоязычных subject'ах упомянуто там.
- Скилл `brig-test` — `make test` / `make lint` для `pre-push`.

