#!/bin/sh
# Тест хука .githooks/commit-msg (T-112): subject'ы, которые хук обязан
# принять и отвергнуть. Правила — WORKFLOW.md §4.
set -eu

HOOK="$(dirname "$0")/../.githooks/commit-msg"
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
fail=0

check() {
  want="$1"
  subject="$2"
  printf '%s\n\nbody\n' "$subject" >"$TMP"
  if sh "$HOOK" "$TMP" >/dev/null 2>&1; then got=accept; else got=reject; fi
  if [ "$got" != "$want" ]; then
    echo "FAIL: want $want, got $got: $subject"
    fail=1
  fi
}

check accept 'build(make): add plan-check target [T-111]'
check accept 'ci: run race tests on pull requests'
check accept 'fix(cmd/brig): classify exit codes [T-45]'
check accept 'docs(tasks): link wave 7 issues'
check accept 'feat(vm): add stream ports [T-228]'
check accept 'test(compiler): add probes for leftover placeholder errors [T-150]'
check accept 'chore: remove kanban setup prompts'
check accept 'perf(vm): reuse frame registers'
check accept 'refactor(e2e): split helpers'
check accept 'Merge branch main into feature'

check reject 'fix(vm): исправить панику'
check reject 'fix(vm): handle тип error'
check reject "feat(parser): $(printf 'x%.0s' $(seq 1 70))"
check reject 'feature(vm): add opcode'
check reject 'Fix(parser): require newline'
check reject 'fix(Parser): require newline'
check reject 'fix(parser): Require newline'
check reject 'fix(parser) require newline'
check reject 'fix(): empty scope'

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "commit-msg: all cases pass"
