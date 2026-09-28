// Package actorview строит дерево акторов из снимка планировщика и
// супервизорских связей. Без ввода-вывода: печать — у вызывающего.
package actorview

import (
	"sort"

	"github.com/it1ro/brig-lang/internal/vm"
)

// SupervisorInitialFn — initial_fn актора, которого спавнит Supervisor.start.
// Строка обязана совпадать с Observer.supervisor_initial_fn и
// test_supervisor_initial_fn: по ней дерево отличает супервизор от чужого
// протокола, которому нельзя слать :which_children.
const SupervisorInitialFn = "Supervisor.start$lambda$0$"

// Sort — порядок строк вида.
type Sort int

const (
	// SortTree — дерево: корни и соседи по pid, дети — по связям.
	SortTree Sort = iota
	// SortReductions — плоский список, reductions по убыванию.
	SortReductions
	// SortMailbox — плоский список, ящик по убыванию.
	SortMailbox
	// SortPid — плоский список, pid по возрастанию.
	SortPid
)

// Next переключает сортировку по кругу: tree → reductions → mailbox → pid.
func Next(s Sort) Sort {
	switch s {
	case SortTree:
		return SortReductions
	case SortReductions:
		return SortMailbox
	case SortMailbox:
		return SortPid
	default:
		return SortTree
	}
}

// Label — подпись режима для строки сводки.
func Label(s Sort) string {
	switch s {
	case SortReductions:
		return "reductions ↓"
	case SortMailbox:
		return "mailbox ↓"
	case SortPid:
		return "pid ↑"
	default:
		return "tree"
	}
}

// Node — актор и его дети в порядке связей.
type Node struct {
	Actor    vm.ActorSnapshot
	Children []Node
}

// Row — актор в плоском порядке экрана.
type Row struct {
	Actor vm.ActorSnapshot
	Depth int
	// Cont[i] — на глубине i (i < Depth-1) вертикаль продолжается.
	Cont []bool
	Last bool
}

// Forest — корни в порядке snaps. Ребёнок — pid из links, который есть
// в снимке; порядок детей — порядок links. Актор из любого списка детей
// корнем не бывает. Цикл предков обрезается.
func Forest(snaps []vm.ActorSnapshot, links map[int][]int) []Node {
	by := make(map[int]vm.ActorSnapshot, len(snaps))
	for _, a := range snaps {
		by[a.Pid] = a
	}
	claimed := map[int]bool{}
	for _, kids := range links {
		for _, pid := range kids {
			if _, ok := by[pid]; ok {
				claimed[pid] = true
			}
		}
	}
	var roots []Node
	for _, a := range snaps {
		if claimed[a.Pid] {
			continue
		}
		roots = append(roots, node(a, by, links, map[int]bool{}))
	}
	return roots
}

func node(a vm.ActorSnapshot, by map[int]vm.ActorSnapshot, links map[int][]int, stack map[int]bool) Node {
	n := Node{Actor: a}
	if stack[a.Pid] {
		return n
	}
	stack[a.Pid] = true
	defer delete(stack, a.Pid)
	for _, pid := range links[a.Pid] {
		if stack[pid] {
			continue
		}
		child, ok := by[pid]
		if !ok {
			continue
		}
		n.Children = append(n.Children, node(child, by, links, stack))
	}
	return n
}

// Walk обходит лес в порядке Forest: сначала узел, потом дети.
func Walk(snaps []vm.ActorSnapshot, links map[int][]int, fn func(depth int, a vm.ActorSnapshot)) {
	var rec func(Node, int)
	rec = func(n Node, depth int) {
		fn(depth, n.Actor)
		for _, c := range n.Children {
			rec(c, depth+1)
		}
	}
	for _, n := range Forest(snaps, links) {
		rec(n, 0)
	}
}

// Rows — строки экрана. SortTree сортирует соседей по pid; остальные
// режимы — плоский список без глубины.
func Rows(snaps []vm.ActorSnapshot, links map[int][]int, mode Sort) []Row {
	if mode != SortTree {
		flat := append([]vm.ActorSnapshot(nil), snaps...)
		sort.Slice(flat, func(i, j int) bool {
			a, b := flat[i], flat[j]
			switch mode {
			case SortReductions:
				if a.Reductions != b.Reductions {
					return a.Reductions > b.Reductions
				}
			case SortMailbox:
				if a.Mailbox != b.Mailbox {
					return a.Mailbox > b.Mailbox
				}
			}
			return a.Pid < b.Pid
		})
		rows := make([]Row, len(flat))
		for i, a := range flat {
			rows[i] = Row{Actor: a}
		}
		return rows
	}
	var rows []Row
	var walk func([]Node, int, []bool)
	walk = func(nodes []Node, depth int, cont []bool) {
		nodes = append([]Node(nil), nodes...)
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Actor.Pid < nodes[j].Actor.Pid })
		for i, n := range nodes {
			last := i == len(nodes)-1
			rows = append(rows, Row{
				Actor: n.Actor,
				Depth: depth,
				Cont:  append([]bool(nil), cont...),
				Last:  last,
			})
			next := append(append([]bool(nil), cont...), !last)
			walk(n.Children, depth+1, next)
		}
	}
	walk(Forest(snaps, links), 0, nil)
	return rows
}
