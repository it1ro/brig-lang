# Brig — референсный интерпретатор (Go)
# Дизайн: docs/01-language-design.md

GO      ?= go
BIN     ?= bin
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

.PHONY: all build test test-race lint fmt vet check-examples ebnf-check \
        git-hooks changelog fuzz update-golden clean

all: fmt vet test lint build

## ---- Сборка ----

build:
	$(GO) build -trimpath -o $(BIN)/brig ./cmd/brig
	$(GO) build -trimpath -o $(BIN)/check-examples ./cmd/check-examples

clean:
	rm -rf $(BIN) dist

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

## ---- Документация и грамматика (A1, A2, A6) ----

# Прогон всех brig-примеров из дизайн-доков через парсер-заглушку (A2).
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

fuzz:
	$(GO) test ./internal/lexer/  -run=^$$ -fuzz=FuzzLex    -fuzztime=60s
	$(GO) test ./internal/parser/ -run=^$$ -fuzz=FuzzParse  -fuzztime=60s

update-golden:
	$(GO) test ./internal/... -run=TestGolden -update
	$(GO) test ./cmd/...       -run=TestGolden -update