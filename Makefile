# Brig — референсный интерпретатор (Go)
# Дизайн: docs/01-language-design.md

GO      ?= go
BIN     ?= bin
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

.PHONY: all build test test-race lint fmt vet check-examples ebnf-check \
        git-hooks changelog fuzz update-golden clean \
        test-roundtrip test-ast test-parser test-lexer test-one \
        fmt-check cover cover-html ci-quick check

all: fmt vet test lint build

## ---- Сборка ----

build:
	$(GO) build -trimpath -o $(BIN)/brig ./cmd/brig
	$(GO) build -trimpath -o $(BIN)/check-examples ./cmd/check-examples

clean:
	rm -rf $(BIN) dist coverage.out

## ---- Качество ----

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

# CI-friendly проверка форматирования: exit 1, если gofmt найдёт отклонения.
# В отличие от fmt, ничего не перезаписывает.
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "unformatted files:"; echo "$$out"; exit 1; \
	fi

# Покрытие по всем пакетам; печатает общую строку "total:".
cover:
	$(GO) test -coverprofile=coverage.out ./...
	@$(GO) tool cover -func=coverage.out | tail -1

cover-html: cover
	$(GO) tool cover -html=coverage.out

## ---- Фокусные тесты (этап 3: AST, formatter, round-trip) ----

# Round-trip и идемпотентность форматтера (internal/ast/format_test.go).
# Закрывает шаг 1 итерации: parse → format → parse ≡ parse.
test-roundtrip:
	$(GO) test ./internal/ast/ -run 'TestRoundTrip|TestFormatIdempotent' -v

test-ast:
	$(GO) test ./internal/ast/ -v

test-parser:
	$(GO) test ./internal/parser/ -v

test-lexer:
	$(GO) test ./internal/lexer/ -v

# Один тест: make test-one PKG=./internal/ast TEST=TestRoundTripModule
test-one:
	@test -n "$(PKG)" && test -n "$(TEST)" || \
		(echo "usage: make test-one PKG=./internal/ast TEST=TestRoundTripModule"; exit 2)
	$(GO) test $(PKG) -run '^$(TEST)$$' -v

## ---- Быстрый локальный прогон ----

# Не требует golangci-lint: только то, что должно быть установлено всегда.
# Хорошо для pre-commit / pre-push, когда полный `all` избыточен.
ci-quick: fmt-check vet test-roundtrip test-lexer test-parser
	@echo "ci-quick: ok"

## ---- Документация и грамматика (A1, A2, A6) ----

# Прогон всех brig-примеров из дизайн-доков через парсер (A2).
check-examples:
	$(GO) run ./cmd/check-examples -- docs/01-language-design.md

# Проверка соответствия brig.ebnf и §16 (A6): на данном этапе — дифф-предупреждение.
ebnf-check:
	@test -f brig.ebnf || (echo "brig.ebnf missing (extract from docs, A1)" && exit 1)
	@echo "brig.ebnf present (regeneration from §16 pending)"

## ---- Git ----

# Установка хуков: prepare-commit-msg, commit-msg (conventional), pre-push (test+lint).
git-hooks:
	@mkdir -p .git/hooks
	@for h in .githooks/*; do \
		[ -f "$$h" ] && cp "$$h" ".git/hooks/$$(basename "$$h")" && chmod +x ".git/hooks/$$(basename "$$h")"; \
	done
	@echo "hooks installed: $$(ls .githooks | paste -sd ' ' -)"

## ---- Релизы ----

changelog:
	@command -v git-cliff >/dev/null 2>&1 || (echo "install git-cliff"; exit 1)
	git-cliff -o CHANGELOG.md

## ---- Тест-инфраструктура ----

# TestGolden живёт только в internal/parser; флаг -update объявлен там.
# Нельзя гонять `go test ./internal/... -update` — остальные пакеты флага
# не знают и падают с "flag provided but not defined: -update".
update-golden:
	$(GO) test ./internal/parser/ -run=TestGolden -update

# Три фаззера в трёх пакетах. FuzzRoundTrip живёт в internal/ast/format_test.go,
# FuzzLex — в internal/lexer/lexer_test.go, FuzzParse — в internal/parser/parser_test.go.
fuzz:
	$(GO) test ./internal/lexer/  -run=^$$ -fuzz=FuzzLex        -fuzztime=60s
	$(GO) test ./internal/parser/ -run=^$$ -fuzz=FuzzParse      -fuzztime=60s
	$(GO) test ./internal/ast/    -run=^$$ -fuzz=FuzzRoundTrip  -fuzztime=60s

## ---- CLI без сборки ----

# make check FILE=examples/hello.brig
check:
	@test -n "$(FILE)" || (echo "usage: make check FILE=<path.brig>"; exit 2)
	$(GO) run ./cmd/brig check $(FILE)

repl:
	$(GO) run ./cmd/brig repl
