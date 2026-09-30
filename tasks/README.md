# tasks/ — карта плана

Карта плана работ: milestones, волны (архив), зависимости, ссылки на issues. Статусы — только на [доске](https://github.com/users/it1ro/projects/5) «Brig — разработка» (GitHub Projects v2, проект 5). Конвенции и команды для доски — `WORKFLOW.md`.

Волны 7–13 спланированы по второму аудиту: [AUDIT_REPORT-2.md](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT-2.md), промпт — [AUDIT_PROMPT-2.md](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_PROMPT-2.md).
Третий аудит ([AUDIT_REPORT-3.md](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT-3.md), промпт — [AUDIT_PROMPT-3.md](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_PROMPT-3.md)) спланирован уже не волнами, а milestones M1…M5 (раздел «Milestones» ниже); задачи — sub-issues эпика [#276](https://github.com/it1ro/brig-lang/issues/276). Номера волн 16–19 в отчёте — исторические.

## Правила

**С третьего аудита — milestones, не волны.** Новые задачи сразу заводятся issues: тело issue — единственный источник DoD, milestone — цель, `Blocked by` — порядок. Файлов `wave-N.md` для них нет; задачи по итогам аудита или сессии планирования — sub-issues эпика этого аудита. Правила ниже про файлы волн относятся к архиву волн 0–15.

**Один файл — одна волна (архив).** В файле волны лежат:
- вход и выход;
- таблица порядка и параллельности;
- все задачи волны.

У задачи с issue в файле остаётся строка таблицы со ссылкой: источник DoD, файлов, тест-якоря и «НЕ делать» — тело issue. У задачи без issue — полный блок (meta, «Файлы», «Тест-якорь», «DoD», «НЕ делать»), из которого issue и заводится. Блок без заголовка `###` — это body issue; к нему добавляются строки `Blocked by #M` для задач из `depends_on`, у которых уже есть issue. После заведения блок в файле волны заменяется строкой таблицы со ссылкой.

**Label источника.**
- `audit` — finding первого ([`AUDIT_REPORT.md`](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT.md)) или второго ([`AUDIT_REPORT-2.md`](https://github.com/it1ro/brig-lang/blob/28003da/AUDIT_REPORT-2.md)) аудита.
- `spec-gap` — пробел реализации относительно спеки §16.
- `edit-spec` — перенос утверждённого решения research в спеку (research, D4).

**Нумерация.**
- Каждая волна начинается с нового десятка: Wave 7 — T-110…, Wave 8 — T-120…, …, Wave 13 — T-170…; открытые вопросы §17 — T-190…. Третий аудит: Wave 16 — T-240…, DD — T-250…, Wave 17 — T-260…, Wave 18 — T-270…, Wave 19 — T-280…; волны 14–15 заняли T-200…T-239. С T-240 номера сквозные, без десятков: T-283 и T-284 заведены в сессии планирования третьего аудита.
- `depends_on` ссылается только на меньший номер, граф ацикличен.
- Исключения, оставшиеся в истории: T-80…T-86 и T-62 ждали DD T-90…T-93. T-100…T-104 (#169–#173) заведены до перепланирования и живут в волнах 8 и 11.
- Следующий свободный номер — максимальный номер десятка в titles issues и в `tasks/` плюс один. Проверка — `make plan-check ONLINE=1` (T-111); до неё — `gh issue list --repo it1ro/brig-lang --state all --limit 300 --json title --jq '.[].title'`.

**Effort** — шкала доски: low / medium / large. Старые блоки волн 0–6 и
PR #167 писали S / M / L — это то же самое.

**Имя каталога.** В Brig-проекте `tasks/` — задачи `brig task` (research
16). Этот репозиторий — не Brig-проект, здесь `tasks/` — карта плана.
Если `brig task` начнёт сканировать корень репозитория, каталог
переименовывается.

## Milestones

Milestone — проверяемый результат, а не порядок работ. Внутри milestone порядок задаёт `Blocked by`, при прочих равных — Priority; между milestones — номер M. Описание milestone на GitHub — его критерий выхода.

| Milestone | Критерий выхода (кратко) | Эпик |
|---|---|---|
| [M1 · Проект тестируется](https://github.com/it1ro/brig-lang/milestone/2) | `brig test`/`check` работают в проекте из модулей; контуры `bench-scaling`, диагностик, `needs` в CI; спека описывает `<>` | [#276](https://github.com/it1ro/brig-lang/issues/276) |
| [M2 · Коллекции без квадратов](https://github.com/it1ro/brig-lang/milestone/3) | `List` cons, `Map`/`Set` HAMT, `Vector` trie; `bench-scaling` зелёный | #276 |
| [M3 · Акторы надёжны](https://github.com/it1ro/brig-lang/milestone/4) | fan-in без тихих потерь; нет сирот супервизора | #276 |
| [M4 · Язык для библиотек](https://github.com/it1ro/brig-lang/milestone/5) | `cond`, `Enum`, JSON, term order, диагностики, справочник API | #276 |
| [M5 · Спека v0.5](https://github.com/it1ro/brig-lang/milestone/6) | §0.11, история вынесена, версия сверяется | #276 |
| [pre-alpha](https://github.com/it1ro/brig-lang/milestone/1) | Must §16 работает (волны 7–10) | — |

T-104 (#173) и T-153 (#275) — sub-issues #276 без milestone: решаются после M2.

## Волны (архив)

| Волна | Файл | Тема | Эпик | Состояние |
|---|---|---|---|---|
| 0 | [wave-0.md](wave-0.md) | fail-fast на `iter/regvm`, merge | — | закрыта |
| 1 | [wave-1.md](wave-1.md) | test-infra и разблокировка | — | закрыта |
| 2 | [wave-2.md](wave-2.md) | small fixes слоя 1 | — | закрыта |
| 3 | [wave-3.md](wave-3.md) | major fixes | — | закрыта |
| 4 | [wave-4.md](wave-4.md) | blockers full-fix | — | закрыта |
| 5 | [wave-5.md](wave-5.md) | docs cleanup | [#49](https://github.com/it1ro/brig-lang/issues/49) | закрыта, кроме T-65 |
| 6 | [wave-6.md](wave-6.md) | Must-пробелы §16 | [#124](https://github.com/it1ro/brig-lang/issues/124) | закрыта |
| 7 | [wave-7.md](wave-7.md) | контуры обратной связи и согласование | — | следующая; все задачи заведены (pre-alpha) |
| 8 | [wave-8.md](wave-8.md) | решения: DD и спека | — | DD T-120…T-125 решены; все задачи заведены (pre-alpha) |
| 9 | [wave-9.md](wave-9.md) | язык: остаток Must и модули | — | все задачи заведены (pre-alpha) |
| 10 | [wave-10.md](wave-10.md) | язык для библиотек и stdlib на Brig | — | все задачи заведены (pre-alpha) |
| 11 | [wave-11.md](wave-11.md) | укрепление и производительность | — | все задачи заведены |
| 12 | [wave-12.md](wave-12.md) | рантайм-механизмы | — | план; issues заведены |
| 13 | [wave-13.md](wave-13.md) | Should §16 и политика на Brig | — | план; issues заведены, T-175 — won't-fix |
| 14 | [wave-14.md](wave-14.md) | REPL как рабочая консоль | — | план; issues заведены |
| 15 | [wave-15.md](wave-15.md) | observer и TUI | — | план; issues заведены |
| — | [decisions.md](decisions.md) | решённые DD, открытые вопросы §17 (T-190…), DD третьего аудита (T-250…T-259) | — | T-190…T-195 заведены; T-250…T-259 решены 2026-09-30 |

«Состояние» — снимок на 2026-09-28; правду о статусе знает только доска.

```mermaid
flowchart LR
  W0["Wave 0: iter/regvm"] --> W1["Wave 1: test-infra"]
  W1 --> W2["Wave 2: small"]
  W1 --> W3["Wave 3: major"]
  W2 --> W4["Wave 4: blockers"]
  W3 --> W4
  W1 --> W5["Wave 5: docs"]
  W4 --> W5
  W5 --> W6["Wave 6: Must §16"]
  W6 --> W7["Wave 7: контуры обратной связи"]
  W7 --> W8["Wave 8: DD и спека"]
  W8 --> W9["Wave 9: остаток Must, модули"]
  W9 --> W10["Wave 10: язык для библиотек, stdlib на Brig"]
  W7 -.->|"T-102, T-103, T-152"| W11["Wave 11: укрепление, производительность"]
  W10 --> W11
  W8 --> W12["Wave 12: рантайм-механизмы"]
  W11 -.->|"T-102"| W12
  W10 --> W13["Wave 13: Should, политика на Brig"]
  W12 --> W13
  D17["§17: T-190..T-195"] -.-> W13
  W9 -.->|"T-137"| W14["Wave 14: REPL-консоль"]
  W12 --> W15["Wave 15: observer, TUI"]
  W14 -.->|"T-209"| W15
```

Пунктир — ранний старт части задач. DD Wave 8 (T-120…T-125) — работа
человека, их можно решать параллельно Wave 7.

## Pre-alpha

**Критерий:** всё, что §16 называет Must, работает; спека не противоречит
ни себе, ни реализации; CI это проверяет. Стабильность и web-функции не
требуются — это alpha.

Задачи — milestone [pre-alpha](https://github.com/it1ro/brig-lang/milestone/1), 25 issues (#175–#199):
- Wave 7: T-113, T-115, T-116, T-117;
- Wave 8: T-120…T-125 (DD, решены), T-126, T-127, T-128;
- Wave 9: T-130…T-139;
- Wave 10: T-147, T-149.

Критический путь:
- T-113 → T-116 → T-126 → T-128 → T-135 → T-137 → T-139;
- T-126 → T-127 → T-138;
- T-120 → T-130.

Остальные задачи волн 7–13 — alpha и дальше.

## Перед Wave 7 — действия мейнтейнера

Это работа на доске и в PR. Агенту она не отдаётся.

1. Смержить этот план. Он содержит коммиты T-65 (PR #167, перенесены на
   свежий `main`), поэтому #167 закрывается вместе с ним, а #166 (T-65) и
   эпик #49 — после merge.
2. Закрыть эпик #124: все задачи Wave 6 закрыты.
3. Labels: #169 — `wave-8`; #171, #172, #173 — `wave-11` (вместо `wave-7`,
   P-2).
4. PR #174 (T-101): сменить base на `main` — ветка `docs/web-mvp-research`
   уже вмержена. После merge стенд `bench-heap` доступен T-153.
5. PR #165: решить судьбу (открыт с 2026-09-26).
6. Добавить в поле доски **Wave** значения 7–13 (веб-интерфейс: через
   API замена списка значений сбрасывает Wave у существующих карточек) и
   проставить их карточкам pre-alpha.
7. Завести оставшиеся задачи Wave 7 (T-110, T-111, T-112, T-114, T-118,
   T-119) из блоков `wave-7.md`.

## Метрика прогресса для библиотек

После T-115 у каждого файла корпуса (`corpus/`) есть ожидаемый уровень
(`parse` / `check` / `run`) и список `needs`. Выход волн 9–13
сформулирован через корпус. Сводка `make corpus` (топ задач по числу
заблокированных файлов) — вход для приоритизации: при прочих равных
раньше берётся задача, которая разблокирует больше библиотечного кода.

Метрика — «файлы на уровне check/run» из первой строки сводки
(`corpus: N files — run R, check C, parse P, none X`). В неё не входят
файлы, у которых в `needs` есть `horizon` (T-249): их уровень не поднимет
ни одна задача языка, пока нет модулей горизонта (`Calmar.*`, `Sql`,
`Http.Client`, …), а заглушки этих модулей не пишутся — они заморозили бы
API фреймворков, которые ещё не спроектированы. Такие файлы сводка
печатает отдельной строкой `horizon: N`. Уровень `check` проверяет граф
модулей от корня проекта (`project.brig` вверх от файла, `lib/`, если
есть), как `brig check`: соседний модуль проекта найден, первая ошибка —
то, чего действительно нет. `run` исполняет файл из его каталога.
Программы dogfooding третьего аудита — `corpus/apps/` (`csv`, `semver` —
`run`, `kv` — `check` и e2e `TestCorpusKvE2E`).

## Горизонт после Wave 13

Research (`web-mvp-research/08-roadmap.md`, фазы 2–5) утвердил ещё
многое. Здесь — без блоков задач: они заводятся, когда предыдущие волны
закрыты, и после третьего аудита (T-154).

- **Скриптинг:** режим `script` (Q-script), `Env`, `Sys.exit`, порты
  `File`, `Proc`, `Stdin`, `Cli.parse`, пул Go-воркеров для тяжёлых
  нативов (R15).
- **Язык для фреймворков:**
  - модули и типы как значения, `Type.fields` (L5, L11);
  - «модуль типа» (Q-poly);
  - контракты: §14.4, `CHECK_KIND`/`CHECK_TAG`, `Json.decode_as` (09);
  - `quote` — RFC-0001 и процесс `rfcs/` (21, Q-rfc).
- **Сеть:** `Tcp`, `Tls`, HTTP на Go `net/http` за портами (B2), `Conn` и
  плаги в `Http`, ACME (B1), фейковые часы и in-memory транспорт (R9),
  `Test.isolated` на экземплярах рантайма (16).
- **Stdlib:** `Log`, `Config`, `Crypto`, `Cache`, `Term` (CBOR), `Telemetry`,
  `Time`-типы (`Instant`, `Date`, `Duration`), `Sql` и встроенный SQLite,
  `PubSub`, `Registry`.
- **Тулчейн:** `brig new`, `fmt`, `build` (один файл), `fix`, `task`,
  `observe`, пакетный менеджер (10), `brig doc`, tree-sitter, LSP.
- **Фреймворки:** Whelk (12), Calmar (06, 14–18) — отдельными пакетами.
- **Производительность:** макро-бенчмарки и дашборд (19), решение по
  модели heap до 1.0 (11), N:M (R10), WASM (B6).

## Verification needed

Findings с тегом `[inferred]` из первого аудита. Все три проверены в
Wave 1 (T-13…T-15). Таблица — образец для следующих аудитов: второй аудит
пометил свои `[inferred]` findings прямо в отчёте. `I-F4`, `I-F6`, `I-F11`
тоже `[inferred]`, но аудит сам пометил их `ok` / «не дыра».

| Finding | Что проверить | Команда/тест | Если подтвердится | Если нет |
|---|---|---|---|---|
| A-F1 (T-13) | Все акторы исполняются в одной goroutine кооперативным run-loop (`internal/vm/scheduler.go:313-428`) | `rg -n 'go func\|go s\.' internal/vm`; тест `TestVerifyAF1SingleGoroutineScheduler`: spawn 100 акторов, `runtime.NumGoroutine()` растёт меньше чем на 100 | T-90 решён: C (#40); T-80 — правка доков | A-F1 → `false-positive`, T-90 закрывается как неактуальный, T-80 не создаётся |
| A-F7 (T-14) | (1) «срез: не реализовано» → exit 3 вместо 1; (2) `internal: upvalue out of range` → exit 2 вместо 3; (3) `runModule` (`internal/compiler/compiler_test.go:13-33`) не прогоняет sema | (1)(2) `go run ./cmd/brig run <probe>.brig; echo $?` по `cmd/brig/main.go:162-166, 208-211`; (3) тест `TestVerifyAF7RunModuleSkipsSema`: `print(trap(1+1))` компилируется через `runModule`, а `brig check` отвергает | Подтвердилось: T-45, T-46 | Неподтверждённый пункт → `false-positive` |
| I-F14 (T-15) | (1) Большой `ms` в `RECVTIMER` молча усекается или переполняется (`scheduler.go:888-894`); (2) `wakeExpired` (`scheduler.go:366`) даёт недетерминированный порядок в `ready` | (1) тест `TestVerifyIF14HugeTimerMs`: `after 9223372036854775807`; (2) программа из N акторов с одинаковым таймаутом: `for i in $(seq 20); do go run ./cmd/brig run p.brig; done \| sort \| uniq -c` — больше одной строки значит недетерминизм | Подтвердилось: T-47, T-48 | `false-positive` |

## Design decisions

Код по вопросу, ждущему решения, не пишется, пока автор языка не решит. DD-задачи имеют label `design-decision` и модель `human`. Задачи, ждущие DD, стоят на доске в Blocked, а после решения переходят в Todo или закрываются как won't-fix, если так сказано в их DoD. У каждого нового DD есть рекомендация аудита или research: DD её утверждает или отвергает.

- DD, которые блокируют волну, лежат в файле волны: T-120…T-125 — [wave-8.md](wave-8.md), T-104 — [wave-11.md](wave-11.md), T-174 — [wave-13.md](wave-13.md).
- Решённые DD и открытые вопросы §17 (T-190…) — в [decisions.md](decisions.md).
