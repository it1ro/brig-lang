package vm

import "github.com/it1ro/brig-lang/internal/runtime"

// ---- реестр имён (§12.8, T-164) ----

type nameEntry struct {
	name runtime.Value
	pid  int
}

func (s *Scheduler) nameIndex(name runtime.Value) int {
	for i, e := range s.names {
		if runtime.KeyEqual(e.name, name) {
			return i
		}
	}
	return -1
}

// Register связывает name с pid. Имя занято (в том числе этим же pid) —
// Error(:already_registered); pid мёртв — Error(:not_alive).
func (s *Scheduler) Register(name runtime.Value, pid int) runtime.Value {
	a, ok := s.actors[pid]
	if !ok || a.status == actorDone || a.status == actorFailed {
		return runtime.Variant("Error", runtime.Atom("not_alive"))
	}
	if s.nameIndex(name) >= 0 {
		return runtime.Variant("Error", runtime.Atom("already_registered"))
	}
	s.names = append(s.names, nameEntry{name: name, pid: pid})
	return runtime.Variant("Ok", runtime.Unit)
}

// Unregister снимает имя, если оно есть.
func (s *Scheduler) Unregister(name runtime.Value) {
	if i := s.nameIndex(name); i >= 0 {
		s.names = append(s.names[:i], s.names[i+1:]...)
	}
}

// Whereis — Some(pid) или None.
func (s *Scheduler) Whereis(name runtime.Value) runtime.Value {
	if i := s.nameIndex(name); i >= 0 {
		return runtime.Variant("Some", runtime.Value{Kind: runtime.KindPid, Pid: s.names[i].pid})
	}
	return runtime.Variant("None")
}

// dropNames снимает все имена умершего актора.
func (s *Scheduler) dropNames(pid int) {
	kept := s.names[:0]
	for _, e := range s.names {
		if e.pid != pid {
			kept = append(kept, e)
		}
	}
	s.names = kept
}
