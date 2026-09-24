#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

echo "=== [1/2] Создание testdata/golden/recv_inline.brig ==="
mkdir -p testdata/golden
cat > testdata/golden/recv_inline.brig <<'BRIG'
fn main() ->
    x = recv (:value, v) -> v
    x
BRIG

echo "=== [2/2] Создание testdata/negative/*.brig + *.err ==="
mkdir -p testdata/negative

# Спред без операнда в list-литерале (expression position, §4.2)
cat > testdata/negative/spread_no_operand_list.brig <<'BRIG'
fn main() ->
    xs = [1, ..]
BRIG
printf 'expression' > testdata/negative/spread_no_operand_list.err

# Спред без операнда в аргументах вызова (§5.2)
cat > testdata/negative/spread_no_operand_call.brig <<'BRIG'
fn main() ->
    f(..)
BRIG
printf 'expression' > testdata/negative/spread_no_operand_call.err

# Спред без операнда в map-литерале (expression position)
cat > testdata/negative/spread_no_operand_map.brig <<'BRIG'
fn main() ->
    m = %{ .. }
BRIG
printf 'expression' > testdata/negative/spread_no_operand_map.err

# Спред без операнда в record-литерале (expression position)
cat > testdata/negative/spread_no_operand_record.brig <<'BRIG'
fn main() ->
    r = User{ .. }
BRIG
printf 'expression' > testdata/negative/spread_no_operand_record.err

echo
echo "=== Созданные файлы ==="
ls -1 testdata/golden/recv_inline.brig
ls -1 testdata/negative/spread_no_operand_*.brig
ls -1 testdata/negative/spread_no_operand_*.err
echo
echo "=== Следующие шаги ==="
echo "  1. make update-golden     # сгенерирует .ast/.round.brig для recv_inline"
echo "  2. make check-examples    # ожидается: blocks: checked 64, failed 0"
echo "  3. make all               # полный прогон (fmt, vet, test, lint, build)"
echo "  4. make test-race         # race-detector"
echo "  5. make fuzz              # 3 фаззера по 60s"
echo "  6. make changelog         # требует git-cliff"
