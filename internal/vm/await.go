package vm

import (
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- await / reply (§12.9, T-165) ----

// Слот ответа — runtime.RefSlot за указателем в самом ref: его создаёт
// make_ref, все копии ref делят один слот. Актор не хранит таблицу своих
// слотов, поэтому ref, по которому так и не ждали (correlation-идиома),
// не держит память актора.

// makeRef — ref из make_ref актора a со слотом ответа.
func (s *Scheduler) makeRef(a *Actor) runtime.Value {
	ref := s.nextRef
	s.nextRef++
	return runtime.Value{Kind: runtime.KindRef, Ref: ref, Slot: &runtime.RefSlot{Owner: a.pid}}
}

// Reply кладёт v в слот ref, если pid жив, ref создан pid и слот пуст;
// ждущего в await будит. Иначе ответ молча отбрасывается. Ящик, HWM и
// mailbox_size не затрагиваются.
func (s *Scheduler) Reply(pid int, ref, v runtime.Value) {
	slot := ref.Slot
	if slot == nil || slot.Owner != pid || slot.State != runtime.SlotEmpty {
		return
	}
	a, ok := s.actors[pid]
	if !ok || a.status == actorDone || a.status == actorFailed {
		return
	}
	slot.State, slot.Val = runtime.SlotFilled, v
	if a.awaiting == slot && a.status == actorBlocked {
		s.clearTimer(a)
		a.status = actorReady
		s.ready = append(s.ready, a)
	}
}

// await — шаг AWAIT актора a. block — ответа нет и срок не вышел: актор
// ждёт, и AWAIT исполняется снова, когда его будят (reply, таймер, exit).
// Слот закрывается и после Ok, и после Error(:timeout).
func (s *Scheduler) await(a *Actor, refVal, msVal runtime.Value) (res runtime.Value, block bool, err error) {
	slot := refVal.Slot
	if refVal.Kind != runtime.KindRef || slot == nil || slot.Owner != a.pid {
		return runtime.Unit, false, typeErr("await", refVal)
	}
	d, ok := awaitDuration(msVal)
	if !ok {
		return runtime.Unit, false, typeErr("await", msVal)
	}
	resuming := a.awaiting == slot
	switch slot.State {
	case runtime.SlotFilled:
		res = runtime.Variant("Ok", slot.Val)
	case runtime.SlotEmpty:
		if !resuming && d > 0 {
			s.armTimer(a, time.Now().Add(d))
			a.awaiting = slot
			return runtime.Unit, true, nil
		}
		if resuming && time.Now().Before(a.recvDeadline) {
			return runtime.Unit, true, nil
		}
		res = runtime.Variant("Error", runtime.Atom("timeout"))
	default:
		res = runtime.Variant("Error", runtime.Atom("timeout"))
	}
	slot.State, slot.Val = runtime.SlotClosed, runtime.Unit
	if resuming {
		s.clearTimer(a)
	}
	return res, false, nil
}

// awaitDuration — timeout await: Int ≥ 0, в пределах Duration (как after,
// I-F14).
func awaitDuration(v runtime.Value) (time.Duration, bool) {
	if v.Kind != runtime.KindInt {
		return 0, false
	}
	var ms int64
	if v.IsSmall {
		ms = v.SmallInt
	} else if b := v.AsBig(); b.IsInt64() {
		ms = b.Int64()
	} else {
		return 0, false
	}
	if ms < 0 {
		return 0, false
	}
	return recvTimerDuration(ms)
}
