package repl

import (
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/highlight"
)

// didYouMean — ближайшее к name имя сессии, прелюдии, хелперов или
// функции модуля на расстоянии Левенштейна ≤ 2; "" — такого нет. При
// равном расстоянии берётся первое по алфавиту.
func didYouMean(name string, env highlight.Env) string {
	if env.Prelude == nil && env.Modules == nil && env.Bindings == nil {
		env = highlight.REPLEnv()
	}
	var cands []string
	if mod, fn, ok := strings.Cut(name, "."); ok {
		for m, fns := range env.Modules {
			if m == mod {
				for f := range fns {
					cands = append(cands, m+"."+f)
				}
			} else if fns[fn] {
				cands = append(cands, m+"."+fn)
			}
		}
	} else {
		for _, set := range []map[string]bool{env.Bindings, env.Prelude, env.Helpers} {
			for n := range set {
				cands = append(cands, n)
			}
		}
	}
	sort.Strings(cands)
	best, bestD := "", 3
	for _, c := range cands {
		if c == name {
			continue
		}
		if d := levenshtein(name, c, bestD); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

// levenshtein — расстояние правки по рунам; больше limit не уточняется.
func levenshtein(a, b string, limit int) int {
	ra, rb := []rune(a), []rune(b)
	if d := len(ra) - len(rb); d > limit || -d > limit {
		return limit + 1
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
