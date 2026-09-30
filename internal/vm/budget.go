package vm

import (
	"math"
	"unsafe"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- бюджет хода и Actor.info (§12.10, T-169) ----

// budget — счётчики актора: за текущий ход (от старта, взятия сообщения
// recv или срабатывания after) и за прошлые ходы, и лимиты хода.
// Байты — оценка: сумма поверхностных размеров значений, которые актор
// построил опкодами и получил от нативов; точнее Go-аллокатора не бывает.
// Нулевое значение — лимитов нет. Горячий путь редукции — один инкремент и
// одно сравнение: итог с рождения копится при смене хода.
type budget struct {
	turnReds, turnAlloc   int64
	pastReds, pastAlloc   int64
	limitReds, limitAlloc int64 // 0 — лимита нет
}

// newTurn начинает ход: recv взял сообщение или ушёл на ветку after.
func (b *budget) newTurn() {
	b.pastReds += b.turnReds
	b.pastAlloc += b.turnAlloc
	b.turnReds, b.turnAlloc = 0, 0
}

// reduce считает редукцию (K-4); превышение лимита — сигнал exit.
// Счётчик хода растёт на 1 и лимит проходит ровно один раз, поэтому
// равенства достаточно, а при limitReds == 0 оно не наступает.
func (s *Scheduler) reduce(a *Actor) {
	a.turnReds++
	if a.turnReds == a.limitReds {
		s.overBudget(a, "turn_reductions", a.turnReds, a.limitReds)
	}
}

// charge считает выделение значения v.
func (s *Scheduler) charge(a *Actor, v *runtime.Value) {
	s.chargeBytes(a, sizeEstimate(v))
}

// chargeBytes списывает n байт с бюджета хода актора a.
func (s *Scheduler) chargeBytes(a *Actor, n int64) {
	if n == 0 {
		return
	}
	a.turnAlloc += n
	if a.limitAlloc > 0 && a.turnAlloc >= a.limitAlloc {
		s.overBudget(a, "turn_alloc_bytes", a.turnAlloc, a.limitAlloc)
	}
}

// overBudget ставит актору exit(pid, (:resource_limit, (kind, used,
// limit))) — тот же флаг сигнала, что у exit, обработка — в ближайшей
// точке редукции. Уже завершающийся актор (ensure исполняются вне
// бюджета) повторно не сигналим.
func (s *Scheduler) overBudget(a *Actor, kind string, used, limit int64) {
	if a.exit != nil {
		return
	}
	s.signalExit(a, runtime.Tuple(runtime.Atom("resource_limit"),
		runtime.Tuple(runtime.Atom(kind), runtime.Int(used), runtime.Int(limit))))
}

var (
	valueSize = int64(unsafe.Sizeof(runtime.Value{}))
	entrySize = int64(unsafe.Sizeof(runtime.MapEntry{}))
	fieldSize = int64(unsafe.Sizeof(runtime.RecordField{}))
	// boxSize — заголовок значения за указателем (вариант, запись,
	// замыкание); порядок величины, не точный размер каждого.
	boxSize = int64(unsafe.Sizeof(runtime.VariantValue{}))
)

// sizeEstimate — поверхностный размер памяти, которую держит само значение
// v, без вложенных: вложенные учтены, когда их строили. Скаляры — 0.
func sizeEstimate(v *runtime.Value) int64 {
	switch v.Kind {
	case runtime.KindStr:
		return int64(len(v.Str))
	case runtime.KindBytes:
		return int64(len(v.Bytes))
	case runtime.KindTuple:
		return int64(len(v.Tuple)) * valueSize
	case runtime.KindList:
		return int64(len(v.List)) * valueSize
	case runtime.KindVector:
		return int64(len(v.Vector)) * valueSize
	case runtime.KindSet:
		return int64(len(v.Set)) * valueSize
	case runtime.KindMap:
		return int64(len(v.Map)) * entrySize
	case runtime.KindVariant:
		return boxSize + int64(len(v.Variant.Args))*valueSize
	case runtime.KindRecord:
		return boxSize + int64(len(v.Record.Fields))*fieldSize
	case runtime.KindClosure:
		return boxSize + int64(len(v.ClosureVal.Captures))*valueSize
	case runtime.KindInt:
		if !v.IsSmall {
			return int64(v.AsBig().BitLen()/8) + 8
		}
	}
	return 0
}

// parseLimits разбирает второй аргумент spawn: анонимная запись с
// необязательными полями turn_reductions и turn_alloc_bytes, Int > 0.
// Иначе — (:type_error, (:spawn, limits)).
func parseLimits(v runtime.Value) (reds, alloc int64, err error) {
	if v.Kind != runtime.KindRecord || v.Record.Type != "" {
		return 0, 0, typeErr("spawn", v)
	}
	for _, f := range v.Record.Fields {
		n, ok := positiveInt(f.Val)
		if !ok {
			return 0, 0, typeErr("spawn", v)
		}
		switch f.Name {
		case "turn_reductions":
			reds = n
		case "turn_alloc_bytes":
			alloc = n
		default:
			return 0, 0, typeErr("spawn", v)
		}
	}
	return reds, alloc, nil
}

// positiveInt — Int > 0; большое — насыщается до MaxInt64 (недостижимо).
func positiveInt(v runtime.Value) (int64, bool) {
	if v.Kind != runtime.KindInt {
		return 0, false
	}
	if v.IsSmall {
		return v.SmallInt, v.SmallInt > 0
	}
	return math.MaxInt64, v.AsBig().Sign() > 0
}
