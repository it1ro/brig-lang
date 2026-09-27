# 18. Аутентификация и авторизация (утверждено 2026-09-27)

## Решения

| Вопрос | Решение |
|---|---|
| Поставка | **генератор `calmar gen auth`** (как `phx.gen.auth`, Rails 8): контекст `Accounts`, контроллеры, шаблоны, миграции, плаги и тесты попадают в приложение и меняются как свой код. Безопасные примитивы — в stdlib и Calmar |
| Сессии | **токены в БД + подписанная cookie**: в cookie — случайный токен (`Crypto.random_bytes`), в БД — его хеш. Отзыв, список активных входов, «выйти везде». API — bearer-токены той же схемы (хеш в БД, показываются один раз) |
| Авторизация | **функции-политики**: модуль политики на ресурс с мультиклозной `allow(user, action, resource) -> Bool`; `Calmar.authorize(conn, Policy, action, resource) -> Result`, отказ — `Error((:forbidden, …))` → 403 через fallback (L14). Без DSL |
| Состав генератора | регистрация, вход/выход, сброс пароля по почте (`Mailer`, C3), подтверждение email, список и отзыв сессий, ограничение попыток входа (`Cache`, B3), тесты (`Test.isolated`, 16). 2FA (TOTP), magic links, passkeys, OAuth/OIDC — пакетами позже |

## Политика в стиле Brig

```brig
module Lookout.Policies.MonitorPolicy

import Lookout.Accounts.User
import Lookout.Monitors.Monitor

pub fn allow(User{ role: :admin }, _, _)             -> true
pub fn allow(_, :show, _)                            -> true
pub fn allow(user, :update, Monitor{ owner_id: id }) when id == user.id -> true
pub fn allow(_, _, _)                                -> false
```

```brig
pub fn update(conn, %{ "id" => id }) ->
    with
        Ok(m) <- Monitors.get(id)
        Ok(_) <- Calmar.authorize(conn, MonitorPolicy, :update, m)
        …
```

Порядок клауз — порядок правил; последняя клауза — явный запрет по
умолчанию. Политика — чистая функция: тестируется без HTTP и БД.

## Требования к платформе

- **R15 (02): тяжёлые native-операции — вне run-loop.** argon2id на вход
  занимает ~50–100 мс CPU; в однопоточном run-loop это заморозит все
  запросы. `Crypto.argon2id`, `bcrypt`, большие хеши, сжатие исполняются
  в пуле Go-воркеров, вызывающий актор ждёт через `await(ref)` (R12).
- Подписанные cookie и одноразовые токены (сброс пароля, подтверждение
  email) — `Crypto.sign` / `Crypto.seal` поверх `Term` (16) со сроком
  жизни в полезной нагрузке.
- Сравнение токенов и хешей — только `Crypto.secure_compare`.
