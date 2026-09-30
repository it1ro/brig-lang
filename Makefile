# Brig — референсный интерпретатор (Go)
# Дизайн: docs/01-language-design.md
GO      ?= go
BIN     ?= bin
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

.PHONY: all build test test-race lint fmt vet check-examples spec-tables \
	git-hooks fuzz update-golden update-bytecode update-examples run-examples clean \
	corpus update-corpus plan-check bench \
	test-roundtrip test-ast test-parser test-lexer test-one \
	test-vm test-compiler test-stdlib run run-hello \
	fmt-check cover cover-html ci-quick check repl

# `make` без цели: полный локальный прогон всего, что должно быть зелёным.
# Добавлены цели Трека C: test-vm и test-compiler.
all: check-smallint fmt vet test lint build check-examples run-examples corpus plan-check

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

# Встроенная stdlib на Brig (T-146): доктесты `##` модулей через brig test.
# Те же проверки идут в `go test ./stdlib/ ./cmd/brig/`, поэтому в all
# отдельно не входит.
test-stdlib: build
	$(BIN)/brig test stdlib

# Один тест: make test-one PKG=./internal/ast TEST=TestRoundTripModule
test-one:
	@test -n "$(PKG)" && test -n "$(TEST)" || \
		(echo "usage: make test-one PKG=./internal/ast TEST=TestRoundTripModule"; exit 2)
	$(GO) test $(PKG) -run '^$(TEST)$$' -v

## ---- Запуск программ (Трек C) ----
# make run FILE=examples/hello.brig
run:
	@test -n "$(FILE)" || (echo "usage: make run FILE=<path.brig>"; exit 2)
	$(GO) run ./cmd/brig $(FILE)

# Долгоживущие примеры (серверы) не завершаются сами: run-examples и
# update-examples их пропускают, проверяют их e2e-тесты в cmd/brig
# (http_hello — TestHttpServerE2E).
EXAMPLES_SERVE := examples/http_hello.brig

# Исполнить все примеры и сравнить stdout с examples/X.out (падает с diff).
run-examples:
	@for f in examples/*.brig; do \
		case " $(EXAMPLES_SERVE) " in *" $$f "*) echo "== $$f == (server, skipped)"; continue;; esac; \
		echo "== $$f =="; \
		$(GO) run ./cmd/brig $$f 2>&1 | diff -u $${f%.brig}.out - || exit 1; \
	done

# Перезаписать examples/*.out по текущему выводу (diff смотреть глазами).
update-examples:
	@for f in examples/*.brig; do \
		case " $(EXAMPLES_SERVE) " in *" $$f "*) continue;; esac; \
		$(GO) run ./cmd/brig $$f > $${f%.brig}.out 2>&1 || exit 1; \
	done

# Корпус библиотечного кода (T-115): каждый файл из corpus/manifest.tsv —
# до своего уровня parse/check/run. Падает на регрессии, неожиданном проходе
# и needs с несуществующей задачей; печатает сводку по уровням и needs.
corpus: build
	$(GO) run ./cmd/corpus -brig $(BIN)/brig

# Согласованность плана tasks/ (T-111): уникальность блоков, depends_on,
# ссылки, обязательные части блока. Без сети; `make plan-check ONLINE=1`
# дополнительно сверяет номера с titles issues через gh (в all не входит).
plan-check:
	$(GO) run ./cmd/plan-check $(if $(ONLINE),-online)

# Гигиена меток (T-246): needs манифеста корпуса и pending(T-NNN) спеки не
# ссылаются на закрытые задачи. ONLINE=1 — закрытые по titles issues (gh);
# CLOSED=T-NNN[,T-MMM] — явный список (CI на PR: задача из title). Без них
# не проверяется. В all не входит.
.PHONY: markers-check
markers-check:
	$(GO) run ./cmd/corpus -markers $(if $(ONLINE),-online) $(if $(CLOSED),-closed '$(CLOSED)')

# Поднять уровни неожиданно прошедших файлов и перезаписать X.out
# (diff манифеста смотреть глазами, выполненные задачи из needs снять вручную).
update-corpus: build
	$(GO) run ./cmd/corpus -brig $(BIN)/brig -update

run-hello:
	$(GO) run ./cmd/brig examples/hello.brig

## ---- Быстрый локальный прогон ----
# Не требует golangci-lint. Включает фронтенд + бэкенд + исполнение примеров.
# Хорошо для pre-commit / pre-push, когда полный `all` избыточен.
ci-quick: fmt-check vet test-lexer test-parser test-roundtrip test-vm test-compiler run-examples
	@echo "ci-quick: ok"

## ---- Документация и грамматика (A1, A2, A6) ----
check-examples:
	$(GO) run ./cmd/check-examples -- docs/01-language-design.md

# Таблицы спеки против кода: ключевые слова (§1.3, §B.1, brig.ebnf, лексер),
# прелюдия (§11.5, §12.6), авто-raise (§10.4), pipe-запрет (§7.5). Входит в make test.
spec-tables:
	$(GO) test ./internal/examples -run '^TestSpec'

## ---- Git ----
# Каталог хуков берётся у git: в worktree `.git` — файл, а не каталог.
git-hooks:
	@d=$$(git rev-parse --git-path hooks) && mkdir -p "$$d" && \
	for h in .githooks/*; do \
		[ -f "$$h" ] && cp "$$h" "$$d/$$(basename "$$h")" && chmod +x "$$d/$$(basename "$$h")"; \
	done
	@echo "hooks installed: $$(ls .githooks | paste -sd ' ' -)"

## ---- Тест-инфраструктура ----
update-golden:
	$(GO) test ./internal/parser/ -run=TestGolden -update

fuzz:
	$(GO) test ./internal/lexer/  -run=^$$ -fuzz=FuzzLex        -fuzztime=60s
	$(GO) test ./internal/parser/ -run=^$$ -fuzz=FuzzParse      -fuzztime=60s
	$(GO) test ./internal/ast/    -run=^$$ -fuzz=FuzzRoundTrip  -fuzztime=60s

## ---- Бенчмарки (T-152) ----
# Микро-бенчмарки VM с -benchmem, BENCH_COUNT повторов (для benchstat).
# Не входят в all; CI сравнивает PR с main (.github/workflows/bench.yml).
BENCH_COUNT ?= 10
bench:
	$(GO) test -run '^$$' -bench . -benchmem -count $(BENCH_COUNT) ./internal/vm/

## ---- CLI без сборки ----
check:
	@test -n "$(FILE)" || (echo "usage: make check FILE=<path.brig>"; exit 2)
	$(GO) run ./cmd/brig check $(FILE)

repl:
	$(GO) run ./cmd/brig

check-smallint:
	@if grep -rn '\.Int\b' internal/ --include='*.go' \
		| grep -v '_test\.go' \
		| grep -vE '(runtime|big)\.Int'; then \
		echo "found direct .Int access; use AsBig() instead"; \
		exit 1; \
	fi

update-bytecode:
	$(GO) test ./internal/compiler/ -run=TestBytecodeGolden -update-bytecode
