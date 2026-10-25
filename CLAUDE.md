# Brig — правила для агентов

Правила работы — в `WORKFLOW.md`; протокол сессии исполняет skill
`.claude/skills/brig-workflow`. Начинать с skill `brig-overview`.

- **Что идёт в `main`.** Только через PR, ветка `<type>/<T-NN>-<slug>`
  (`WORKFLOW.md` §3), коммиты `<type>(<scope>): <subject> [T-NN]` (§4).
  Прямой push в `main` — нет.
- **Merge — squash** (`gh pr merge <N> --squash`). Это решение проекта, оно
  важнее общего правила `--merge`: на одном коммите `[T-NN]` в `main`
  держатся проверка Done и стек PR (`WORKFLOW.md` §3, §7).
- **Что требует одобрения.** Режим работы задаёт человек (`WORKFLOW.md`
  §7); по умолчанию — «со сдачей на ревью»: merge только по явной просьбе.
  Design decisions (label `design-decision`) и задачи `human` агент не берёт.
- **Инструменты:** `board`, `wt-new`/`wt-done`, `ci-wait`, `quiet`,
  `aio-run` — `WORKFLOW.md` §2.3.
