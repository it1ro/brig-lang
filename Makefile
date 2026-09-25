# Brig — референсный интерпретатор (Go)
# Дизайн: docs/01-language-design.md
GO      ?= go
BIN     ?= bin
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

.PHONY: all build test test-race lint fmt vet check-examples ebnf-check \
	git-hooks changelog fuzz update-golden clean \
	test-roundtrip test-ast test-parser test-lexer test-one \
	test-vm test-compiler run run-hello \
	fmt-check cover cover-html ci-quick check repl

# `make` без цели: полный локальный прогон всего, что должно быть зелёным.
# Добавлены цели Трека C: test-vm и test-compiler.
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

fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "unformatted files:"; echo "$$out"; exit 1; \
	fi

cover:
	$(GO) test -coverprofile=coverage.out ./...
	@$(GO) tool cover -func=coverage.out | tail -1

cover-html: cover
	$(GO) tool cover -html=coverage.out

## ---- Фокусные тесты: фронтенд (Треки A/B) ----
test-roundtrip:
	$(GO) test ./internal/ast/ -run 'TestRoundTrip|TestFormatIdempotent' -v
test-ast:
	$(GO) test ./internal/ast/ -v
test-parser:
	$(GO) test ./internal/parser/ -v
test-lexer:
	$(GO) test ./internal/lexer/ -v

## ---- Фокусные тесты: бэкенд (Трек C) ----
test-vm:
	$(GO) test ./internal/vm/ -v
test-compiler:
	$(GO) test ./internal/compiler/ -v

# Один тест: make test-one PKG=./internal/ast TEST=TestRoundTripModule
test-one:
	@test -n "$(PKG)" && test -n "$(TEST)" || \
		(echo "usage: make test-one PKG=./internal/ast TEST=TestRoundTripModule"; exit 2)
	$(GO) test $(PKG) -run '^$(TEST)$$' -v

## ---- Запуск программ (Трек C) ----
# make run FILE=examples/hello.brig
run:
	@test -n "$(FILE)" || (echo "usage: make run FILE=<path.brig>"; exit 2)
	$(GO) run ./cmd/brig run $(FILE)

# Быстрая проверка: исполнить все примеры.
run-examples:
	@for f in examples/*.brig; do \
		echo "== $$f =="; \
		$(GO) run ./cmd/brig run $$f || exit 1; \
	done

run-hello:
	$(GO) run ./cmd/brig run examples/hello.brig

## ---- Быстрый локальный прогон ----
# Не требует golangci-lint. Включает фронтенд + бэкенд + исполнение примеров.
# Хорошо для pre-commit / pre-push, когда полный `all` избыточен.
ci-quick: fmt-check vet test-lexer test-parser test-roundtrip test-vm test-compiler run-examples
	@echo "ci-quick: ok"

## ---- Документация и грамматика (A1, A2, A6) ----
check-examples:
	$(GO) run ./cmd/check-examples -- docs/01-language-design.md

ebnf-check:
	@test -f brig.ebnf || (echo "brig.ebnf missing (extract from docs, A1)" && exit 1)
	@echo "brig.ebnf present (regeneration from §16 pending)"

## ---- Git ----
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
update-golden:
	$(GO) test ./internal/parser/ -run=TestGolden -update

fuzz:
	$(GO) test ./internal/lexer/  -run=^$$ -fuzz=FuzzLex        -fuzztime=60s
	$(GO) test ./internal/parser/ -run=^$$ -fuzz=FuzzParse      -fuzztime=60s
	$(GO) test ./internal/ast/    -run=^$$ -fuzz=FuzzRoundTrip  -fuzztime=60s

## ---- CLI без сборки ----
check:
	@test -n "$(FILE)" || (echo "usage: make check FILE=<path.brig>"; exit 2)
	$(GO) run ./cmd/brig check $(FILE)

repl:
	$(GO) run ./cmd/brig repl

check-smallint:
	@if grep -rn '\.Int\b' internal/ --include='*.go' | grep -v '_test\.go'; then \
		echo "found direct .Int field access; use AsBig() instead"; \
		exit 1; \
	fi
