# Brig

<p align="center"><img src="doc/brig-logo.png" height="100" alt="Brig logo"/></p>

Референсный интерпретатор языка программирования Brig на Go.

Brig — иммутабельный, offside-ориентированный язык с акторами, TCO и
единственным присваиванием. Дизайн и формальная спецификация — в
`docs/01-language-design.md`; она **нормативна**.

---

## Что такое Brig

Один абзац вместо списка фич:

> Язык сочетает indentation-driven синтаксис, иммутабельные значения как
> языковую гарантию, стековую байткод-ВМ с гарантированным TCO вне активного
> `ensure`, акторную модель в духе Erlang/Elixir (спавн, `recv`, `watch`,
> HWM), встроенные алгебраические варианты (`Option`/`Result` вместо `nil`),
> мультиклозные функции и паттерн-матчинг. Модули — неймспейсы без
> состояния; акторы — рантайм. Один бинарник, общий heap, ноль FFI в MVP.

Полный список принципов — §0 в дизайн-документе. Полный список фич по
приоритетам — §16 там же.

---

## Быстрый старт

```sh
git clone <repo> brig && cd brig

make build            # bin/brig + bin/check-examples
make test             # go test ./...
make ci-quick         # fmt-check + vet + focused tests + все examples
make all              # полный прогон: check-smallint, fmt, vet, test, lint, build
```
