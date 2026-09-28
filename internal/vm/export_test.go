package vm

// TimerVisits — счётчик записей кучи, осмотренных при обслуживании таймеров.
func (s *Scheduler) TimerVisits() uint64 { return s.timerVisits }

// ArmedTimers — число таймеров в куче.
func (s *Scheduler) ArmedTimers() int { return len(s.timers) }
