// Package observe — состояние и лента падений полноэкранного observe().
// Рисование — пакет view, цикл терминала — пакет run.
package observe

import (
	"errors"
	"sync"
	"time"

	"github.com/it1ro/brig-lang/internal/actorview"
	"github.com/it1ro/brig-lang/internal/termio"
	"github.com/it1ro/brig-lang/internal/vm"
)

// ErrSessionClosed — сессию закрыли, пока вид был открыт.
var ErrSessionClosed = errors.New("session closed")

// Crash — одно событие [:vm, :actor, :crash] или [:vm, :actor, :down].
type Crash struct {
	At     time.Time
	Pid    int
	Name   string
	Reason string
}

// Shot — один опрос: живые акторы и дети супервизоров (порядок старта).
type Shot struct {
	Actors []vm.ActorSnapshot
	Links  map[int][]int
}

// Ring — последние 50 падений, свежие в конце. Add и List можно звать
// с разных горутин.
type Ring struct {
	mu sync.Mutex
	ev []Crash
}

const ringCap = 50

// Add дописывает событие и забывает самое старое сверх ringCap.
func (r *Ring) Add(c Crash) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ev = append(r.ev, c)
	if len(r.ev) > ringCap {
		r.ev = append([]Crash(nil), r.ev[len(r.ev)-ringCap:]...)
	}
}

// List — копия, свежие сначала.
func (r *Ring) List() []Crash {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Crash, len(r.ev))
	for i := range r.ev {
		out[len(r.ev)-1-i] = r.ev[i]
	}
	return out
}

// Model — экран. Update не рисует и не читает терминал.
type Model struct {
	Actors []vm.ActorSnapshot
	Links  map[int][]int
	Sort   actorview.Sort
	// Pid — выбранный актор. HasPid ложь, пока выбирать некого.
	Pid    int
	HasPid bool
	Cursor int
	Detail bool
	// Err — текст сбоя снимка без префикса. Пусто, если снимок удался.
	Err string
	// Note — «актор не жив», пока пользователь не нажмёт клавишу.
	Note    string
	Crashes []Crash
	Quit    bool
}

// Msg — событие для Update.
type Msg interface{ isMsg() }

// KeyMsg — клавиша.
type KeyMsg struct{ Key termio.Key }

// ShotMsg — результат опроса. Err != nil не затирает прежний снимок.
type ShotMsg struct {
	Actors []vm.ActorSnapshot
	Links  map[int][]int
	Err    error
}

// CrashMsg — лента падений, свежие сначала.
type CrashMsg struct{ Crashes []Crash }

func (KeyMsg) isMsg()   {}
func (ShotMsg) isMsg()  {}
func (CrashMsg) isMsg() {}

// Update применяет msg и возвращает новое состояние.
func Update(m Model, msg Msg) Model {
	switch msg := msg.(type) {
	case KeyMsg:
		return onKey(m, msg.Key)
	case ShotMsg:
		return onShot(m, msg)
	case CrashMsg:
		m.Crashes = msg.Crashes
		return m
	default:
		return m
	}
}

func onKey(m Model, k termio.Key) Model {
	m.Note = ""
	if k.Code == termio.KeyInterrupt || k.Rune == 'q' || k.Rune == 'Q' {
		m.Quit = true
		return m
	}
	if k.Code == termio.KeyEsc {
		if m.Detail {
			m.Detail = false
			return m
		}
		m.Quit = true
		return m
	}
	switch k.Code {
	case termio.KeyUp:
		return move(m, -1)
	case termio.KeyDown:
		return move(m, +1)
	case termio.KeyEnter:
		if m.HasPid {
			m.Detail = true
		}
		return m
	}
	if k.Code == termio.KeyRune && (k.Rune == 's' || k.Rune == 'S') {
		m.Sort = actorview.Next(m.Sort)
	}
	return m
}

func onShot(m Model, msg ShotMsg) Model {
	if msg.Err != nil {
		m.Err = msg.Err.Error()
		return m
	}
	m.Err = ""
	prev, had := m.Pid, m.HasPid
	m.Actors = msg.Actors
	m.Links = msg.Links
	rows := m.rows()
	if len(rows) == 0 {
		if had {
			m.Note = deadNote
		}
		m.HasPid = false
		return m
	}
	if !had {
		m.Cursor = 0
		m.Pid = rows[0].Actor.Pid
		m.HasPid = true
		return m
	}
	for i, r := range rows {
		if r.Actor.Pid == prev {
			m.Cursor = i
			m.HasPid = true
			return m
		}
	}
	m.Note = deadNote
	i := m.Cursor
	if i >= len(rows) {
		i = len(rows) - 1
	}
	if i < 0 {
		i = 0
	}
	m.Cursor = i
	m.Pid = rows[i].Actor.Pid
	m.HasPid = true
	return m
}

const deadNote = "actor is not alive"

func move(m Model, d int) Model {
	rows := m.rows()
	if len(rows) == 0 {
		m.HasPid = false
		return m
	}
	i := 0
	if m.HasPid {
		i = m.Cursor
		for j, r := range rows {
			if r.Actor.Pid == m.Pid {
				i = j
				break
			}
		}
	}
	i += d
	if i < 0 {
		i = 0
	}
	if i >= len(rows) {
		i = len(rows) - 1
	}
	m.Cursor = i
	m.Pid = rows[i].Actor.Pid
	m.HasPid = true
	return m
}

func (m Model) rows() []actorview.Row {
	return actorview.Rows(m.Actors, m.Links, m.Sort)
}
