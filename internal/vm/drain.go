package vm

import (
	"errors"
	"unsafe"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- TCO сквозь ensure (doc 02 §5.1, T-173) ----

// cleanupRec — один логический уровень trap с ensure, чей кадр заменён
// хвостовым вызовом TAILCALLENS: замыкания ensure в порядке исполнения
// (LIFO). Замыкания — указатели, а не Value: запись на уровень рекурсии
// стоит несколько слов, а не размер Value на каждый ensure.
type cleanupRec struct {
	ensures []*runtime.ClosureValue
}

// cleanupRecSize — оценка байт записи для бюджета хода (§12.10) без
// замыканий: их уже списал MAKECLOSURE.
var cleanupRecSize = int64(unsafe.Sizeof(cleanupRec{}))

// pushCleanup — шаги 1–2 TAILCALLENS: снять единственный handler
// TRAPENSURE и положить уровень в f.cleanups. ens — окно регистров с
// замыканиями, копируется.
func (s *Scheduler) pushCleanup(a *Actor, f *Frame, ens []runtime.Value) error {
	if len(f.handlers) != 1 || !f.handlers[0].ensure {
		return errors.New("internal: TAILCALLENS outside single ensure trap")
	}
	f.handlers = f.handlers[:0]
	cs := make([]*runtime.ClosureValue, len(ens))
	for i, v := range ens {
		if v.Kind != runtime.KindClosure || v.ClosureVal == nil {
			return errors.New("internal: TAILCALLENS ensure is not a closure")
		}
		cs[i] = v.ClosureVal
	}
	f.cleanups = append(f.cleanups, cleanupRec{ensures: cs})
	s.chargeBytes(a, cleanupRecSize+int64(len(cs))*int64(unsafe.Sizeof(cs[0])))
	return nil
}

// startDrain превращает кадр с cleanups в drain-кадр: результат тела
// верхнего уровня — v (raised — ошибка e = v). Кадр остаётся на месте,
// поэтому результат drain уходит в callDst вызывающего.
func (s *Scheduler) startDrain(a *Actor, f *Frame, v runtime.Value, raised bool) *drainRun {
	d := &drainRun{s: s, a: a, recs: f.cleanups}
	if raised {
		d.failed, d.errv = true, v
	} else {
		d.val = v
	}
	clear(f.regs[:cap(f.regs)])
	f.regs, f.chunk, f.captures, f.ip, f.cont = f.regs[:1], nil, nil, 0, d
	f.handlers = f.handlers[:0]
	f.cleanups = nil
	return d
}

// catchCleanups — raise, всплывший до кадра parent без handlers: drain
// ловит его как ошибку ensure (или колбэка), кадр с cleanups — как raise
// тела верхнего уровня. true — raise пойман.
func (s *Scheduler) catchCleanups(a *Actor, parent *Frame, val runtime.Value) bool {
	if d, ok := parent.cont.(*drainRun); ok {
		d.catch(val)
	} else if len(parent.cleanups) > 0 {
		s.startDrain(a, parent, val, true)
	} else {
		return false
	}
	a.err = nil
	a.result = runtime.Unit
	return true
}

// drainRun исполняет записи cleanups (doc 02 §5.1). Реализует
// nativeCont: каждый resume вызывает следующее замыкание ensure обычным
// кадром. Уровень повторяет схему §5: значение тела → ensure LIFO
// (ошибка ensure замещает, побеждает последняя) → Ok(val) или Error(e),
// и это значение — тело следующего, внешнего уровня.
type drainRun struct {
	s    *Scheduler
	a    *Actor
	recs []cleanupRec // оставшиеся уровни, верхний — последний

	cur   []*runtime.ClosureValue // ensure текущего уровня
	i     int
	level bool // уровень начат: по концу cur строится Ok/Error

	val    runtime.Value // значение тела текущего уровня
	failed bool          // F: была ошибка
	errv   runtime.Value // E: последняя ошибка

	raised    bool // последний колбэк бросил raise
	raisedVal runtime.Value

	// exit — режим unwind от exit (§12.7): ensure исполняются, ошибки и
	// значения игнорируются; done — все исполнены, кадр снимается.
	exit, done bool
}

// catch — raise колбэка ensure: дойдёт до resume.
func (d *drainRun) catch(v runtime.Value) {
	d.raised, d.raisedVal = true, v
}

// enterExit переводит drain в режим exit с текущего места; колбэк,
// прерванный unwind, считается исполненным.
func (d *drainRun) enterExit() {
	d.exit = true
	d.raised = false
}

func (d *drainRun) resume(runtime.Value) (nativeStep, error) {
	if d.raised {
		d.raised = false
		if !d.exit {
			d.failed, d.errv = true, d.raisedVal
		}
		d.raisedVal = runtime.Unit
	}
	for {
		if d.i < len(d.cur) {
			c := d.cur[d.i]
			d.cur[d.i] = nil
			d.i++
			return nativeStep{fn: runtime.Value{Kind: runtime.KindClosure, ClosureVal: c}}, nil
		}
		if d.level && !d.exit {
			if d.failed {
				d.val = runtime.Variant("Error", d.errv)
			} else {
				d.val = runtime.Variant("Ok", d.val)
			}
			d.failed, d.errv = false, runtime.Unit
			if len(d.recs) > 0 {
				// Последнюю обёртку списывает stepNative как результат.
				d.s.charge(d.a, &d.val)
			}
		}
		if len(d.recs) == 0 {
			if d.exit {
				d.done = true
				return nativeStep{exit: true}, nil
			}
			return nativeStep{done: true, res: d.val}, nil
		}
		n := len(d.recs) - 1
		d.cur, d.i, d.level = d.recs[n].ensures, 0, true
		d.recs[n] = cleanupRec{}
		d.recs = d.recs[:n]
	}
}
