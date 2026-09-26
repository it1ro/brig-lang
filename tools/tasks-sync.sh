#!/usr/bin/env bash
# tasks-sync — переписывает статусы в TASKS.md по доске (GitHub Projects v2).
#
# Доска — источник истины, TASKS.md — её проекция. Обновляются:
#   - строки блоков   `- **Issue:** [#N](…) · **Статус:** <статус>`;
#   - строки таблиц   `| T-NN | [#N](…) | … | <статус> |` (последняя ячейка).
# Статус issue на доске — поле Status. Issue вне доски (design decisions):
# закрыт → Done, открыт → «Open — design decision, ждёт автора языка».
#
# Требует `gh` с доступом к проекту (scope `read:project`).
# Использование: tools/tasks-sync.sh [TASKS.md]   (или `make tasks-sync`)
set -euo pipefail

file=${1:-TASKS.md}
owner=${BRIG_O:-it1ro}
project=${BRIG_P:-5}
repo=${BRIG_R:-it1ro/brig-lang}

map=$(mktemp)
trap 'rm -f "$map" "$map.board" "$map.out"' EXIT

gh project item-list "$project" --owner "$owner" --limit 1000 --format json \
  --jq '.items[] | select(.content.number != null) | "\(.content.number)\t\(.status // "No status")"' >"$map.board"
gh issue list --repo "$repo" --state all --limit 1000 --json number,state \
  --jq '.[] | "\(.number)\t\(.state)"' |
  awk -F'\t' 'NR == FNR { board[$1] = $2; next }
    ($1 in board) { print $1 "\t" board[$1]; next }
    $2 == "CLOSED" { print $1 "\tDone"; next }
    { print $1 "\tOpen — design decision, ждёт автора языка" }' "$map.board" - >"$map"

awk -F'\t' '
  NR == FNR { st[$1] = $2; next }
  # Блок задачи.
  /^- \*\*Issue:\*\* \[#[0-9]+\]/ {
    n = $0; sub(/^- \*\*Issue:\*\* \[#/, "", n); sub(/\].*/, "", n)
    if ((n in st) && index($0, " · **Статус:** ") > 0) {
      sub(/ · \*\*Статус:\*\* .*/, " · **Статус:** " st[n])
    }
    print; next
  }
  # Строка таблицы: вторая ячейка — ссылка на issue, последняя — статус.
  /^\| T-[0-9]+ \| \[#[0-9]+\]/ {
    n = $0; sub(/^\| T-[0-9]+ \| \[#/, "", n); sub(/\].*/, "", n)
    if (n in st) {
      k = split($0, c, " \\| ")
      c[k] = st[n] " |"
      line = c[1]; for (i = 2; i <= k; i++) line = line " | " c[i]
      $0 = line
    }
    print; next
  }
  { print }
' "$map" "$file" >"$map.out"

if cmp -s "$map.out" "$file"; then
  echo "tasks-sync: $file актуален"
else
  cp "$map.out" "$file"
  echo "tasks-sync: $file обновлён"
fi
