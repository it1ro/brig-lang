package vm

import "github.com/it1ro/brig-lang/internal/runtime"

// globalEntry — привязка в таблице Global (§12.11). Значение неизменяемо;
// put заменяет только запись.
type globalEntry struct {
	name runtime.Value
	val  runtime.Value
}

func (s *Scheduler) globalIndex(name runtime.Value) int {
	for i, e := range s.globalTable {
		if runtime.KeyEqual(e.name, name) {
			return i
		}
	}
	return -1
}

// GlobalPut связывает name со значением. Имя уже есть — привязка
// заменяется (last-write-wins). Удаления нет.
func (s *Scheduler) GlobalPut(name, val runtime.Value) {
	if i := s.globalIndex(name); i >= 0 {
		s.globalTable[i] = globalEntry{name: name, val: val}
		return
	}
	s.globalTable = append(s.globalTable, globalEntry{name: name, val: val})
}

// GlobalGet — Some(значение) или None.
func (s *Scheduler) GlobalGet(name runtime.Value) runtime.Value {
	if i := s.globalIndex(name); i >= 0 {
		return runtime.Variant("Some", s.globalTable[i].val)
	}
	return runtime.Variant("None")
}

// installGlobal регистрирует Global.put/get (§12.11). Таблица — на
// планировщике, не на акторе: её читает и пишет любой актор.
func installGlobal(def func(name string, arity int, fn runtime.NativeFunc)) {
	def("Global.put", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if err := globalKey(args[0], "put"); err != nil {
			return runtime.Unit, err
		}
		c.(*VM).scheduler.GlobalPut(args[0], args[1])
		return runtime.Unit, nil
	})
	def("Global.get", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if err := globalKey(args[0], "get"); err != nil {
			return runtime.Unit, err
		}
		return c.(*VM).scheduler.GlobalGet(args[0]), nil
	})
}

// globalKey — имя пригодно как ключ Map (§4.8), если равно само себе.
// Иначе get после put не увидит значение: (:type_error, (:put|:get, name)).
func globalKey(name runtime.Value, op string) error {
	if !runtime.KeyEqual(name, name) {
		return typeErr(op, name)
	}
	return nil
}
