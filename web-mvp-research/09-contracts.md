# 09. Контракты данных: гибрид code-first (утверждено 2026-09-27)

## Решение

**Источник истины о форме данных — типы Brig.** На их основе:

1. **проверка на границе** — `decode_as(Type, payload)` глубоко проверяет
   вход (HTTP, брокер, БД, файлы) и возвращает номинальную запись или
   `Error((:invalid, errors))`;
2. **мелкие контракты на `pub fn`** — проверка вида/номинального тега
   аргументов по аннотациям, O(1) на аргумент;
3. **экспорт** JSON Schema / OpenAPI из типов и роутера Calmar; дифф
   схемы в CI ловит ломающие изменения API;
4. **импорт** чужих OpenAPI/protobuf → записи + декодеры (для API,
   которые принадлежат не нам).

Schema-first как основной путь отвергнут: в динамическом языке проверки на
границе в рантайме нужны в любом случае, а внешний IDL беднее Brig
(предикаты, `Option`/`Result`, варианты) и не даёт задела под будущий
статический анализ.

## Почему это работает дёшево: «parse, don't validate»

- Номинальную запись можно получить только через `decode_as` или
  конструктор с проверкой. Значения иммутабельны ⇒ проверенная запись
  **остаётся** валидной навсегда. Номинальный тег — доказательство.
- Внутри сервиса достаточно O(1)-проверки тега на `pub fn`; глубокая
  проверка — ровно один раз, на входе.
- Внутренние `fn` и самовызовы (циклы через TCO) не проверяются вовсе.

## Уровни проверок и цена (оценки, требуют замера)

| Уровень | Где | Стоимость | Оверхед | Режим |
|---|---|---|---|---|
| **B. Граница** | `decode_as`, слитый с JSON-декодером | O(payload), тот же проход | +10–30 % к декодированию, <1 % латентности запроса | всегда |
| **P. Публичные функции** | вход `pub fn` | O(1) на аргумент (вид + тег) | единицы %, в горячих циклах ≈0 | всегда (выключаемо) |
| **D. Глубокие** | каждый вызов, поэлементно, обёртки функций | O(размер) на вызов, O(n²) в рекурсии | 2×…100× | только dev/debug, `brig run --contracts=deep` |

Глубокие проверки функций высшего порядка требуют обёрток, а обёртка
ломает identity-равенство функций (§0.7) — поэтому D никогда не
включается в проде и не влияет на семантику программы.

Реализация P: компилятор вставляет опкоды `CHECK_KIND r, kind` /
`CHECK_TAG r, Type` в пролог `pub fn`; `vm.Verify` их знает; отказ —
авто-raise `(:contract_violation, (fn, arg_index, expected, value))`.
Опционально — бит «проверено как T» в заголовке неизменяемой коллекции
(кэш для повторно приходящих значений; решать по замерам памяти).

## Форма и правила

- **Форма** — аннотации полей типа: `interval_ms: Int`, `last_checked_at:
  Option<Instant>`. Проверяется `decode_as` и уровнем P.
- **Правила** — функция `validate/1` в модуле типа (конвенция «модуль
  типа», 03/L6), собранная из декларативных `Check`-комбинаторов, чтобы
  экспортёр мог перевести их в JSON Schema. Произвольный предикат тоже
  допустим — он проверяется в рантайме, в схему уходит как описание.
- `Changeset` (формы, ошибки по полям для UI) строится поверх той же
  пары «форма + validate», а не параллельно.

```brig module
module Lookout.Monitors.Monitor

type Monitor {
    id: Int,
    name: Str,
    url: Str,
    interval_ms: Int,
    status: Atom,
    last_checked_at: Option<Instant>,
}

pub fn validate(m) ->
    Check.all(m, [
        Check.length(:name, 1 to 200),
        Check.format(:url, :http_url),
        Check.range(:interval_ms, 5_000 to 3_600_000),
        Check.one_of(:status, [:unknown, :up, :down]),
    ])
```

`Json.decode_as(Monitor, body)` = разбор + проверка формы +
`Monitor.validate/1`. Экспорт даёт:

```text
Monitor: { type: object, required: [id, name, url, interval_ms, status],
  properties: { name: { type: string, minLength: 1, maxLength: 200 },
                url: { type: string, format: uri, pattern: "^https?://" },
                interval_ms: { type: integer, minimum: 5000, maximum: 3600000 },
                status: { enum: [unknown, up, down] },
                last_checked_at: { type: [string, "null"], format: date-time } } }
```

## Отображение типов Brig → JSON Schema

| Brig | JSON Schema | Заметка |
|---|---|---|
| `Int` / `Float` / `Decimal` | `integer` / `number` / `string` (decimal) | Decimal — строкой, без потери точности |
| `Str` / `Bytes` | `string` / `string` (base64) | |
| `Bool` / `Atom` | `boolean` / `string` (+`enum` из `Check.one_of`) | |
| `List<T>` / `Vector<T>` / `Set<T>` | `array` (`uniqueItems` для Set) | |
| `Map<Str, V>` | `object` + `additionalProperties` | ключи не-`Str` — не экспортируются, ошибка экспорта |
| номинальная запись | `object` + `$ref` по имени типа | |
| `Option<T>` | `T` или `null`, поле не в `required` | |
| вариант `type X { A(..), B(..) }` | `oneOf` с дискриминатором `"__type__"` | совместимо с `Json.encode(…, { type_tag: true })` (§4.7) |
| `Result<T, E>` | не экспортируется в тело; в OpenAPI — разные коды ответа | |

## Изменения, которые это требует

| Где | Что | Уровень |
|---|---|---|
| Спека §14.4 | «аннотации — documentation-only» → «аннотации проверяются на входе `pub fn` (вид/тег) и в `decode_as` (глубоко)» | DD, M |
| Спека §16 | runtime-контракты: Should → Must (уровни B и P) | DD |
| VM | `CHECK_KIND` / `CHECK_TAG`, `:contract_violation` | M |
| Компилятор/sema | пролог `pub fn` из аннотаций; флаг `--contracts=off\|shallow\|deep` | M |
| stdlib | `Json.decode_as`, `Check`, `Type.fields`/`Type.schema` (рефлексия, 03/L11) | M |
| Тулчейн | `brig schema export` (JSON Schema/OpenAPI из типов + роутера Calmar), `brig schema diff` для CI | S |
| Тулчейн | `brig gen client openapi.yaml` / protobuf | S |
| Дальний горизонт | опциональный статический чекер на тех же аннотациях | N |

## Порядок внедрения

1. Рефлексия типов (03/L11) + `Json.decode_as` с проверкой формы — вместе с
   Calmar (фаза 4 roadmap).
2. `Check` + `validate/1` + `Changeset` поверх них.
3. Уровень P (опкоды, пролог `pub fn`) — сразу после `pub fn` (фаза 1),
   за флагом; включить по умолчанию после замера.
4. Экспорт схем и `schema diff` в CI.
5. Импорт OpenAPI/protobuf.
