package vm

import "github.com/it1ro/brig-lang/internal/runtime"

// TimerVisits — счётчик записей кучи, осмотренных при обслуживании таймеров.
func (s *Scheduler) TimerVisits() uint64 { return s.timerVisits }

// ArmedTimers — число таймеров в куче.
func (s *Scheduler) ArmedTimers() int { return len(s.timers) }

// FrameDepthNative — натив frame_depth(): число кадров актора, который его
// вызвал (T-173: TCO сквозь ensure не растит a.frames).
func FrameDepthNative() runtime.Value {
	return runtime.Func(&runtime.FuncValue{Name: "frame_depth", Arity: 0, IsNative: true,
		Native: func(c runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
			return runtime.Int(int64(len(c.(*VM).scheduler.active.frames))), nil
		}})
}
