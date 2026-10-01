// Package run — цикл observe(): опрос снимка, клавиши через Poll,
// восстановление терминала. Модель и кадр сюда не зашиты в ввод-вывод
// планировщика: снимок приходит функцией.
package run

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/it1ro/brig-lang/internal/observe"
	"github.com/it1ro/brig-lang/internal/observe/view"
	"github.com/it1ro/brig-lang/internal/termio"
)

// Size — размер экрана в колонках и строках.
type Size struct{ W, H int }

// Deps — зависимости цикла. Snapshot и Crashes можно звать не с горутины
// цикла сессии. Tick == nil — тик раз в секунду; observe() его не настраивает.
type Deps struct {
	Snapshot func() (observe.Shot, error)
	Crashes  func() []observe.Crash
	In       *os.File
	Out      io.Writer
	TTY      bool
	Color    bool
	Size     func() (w, h int)
	Resize   <-chan Size
	Tick     <-chan time.Time
}

// Run рисует вид, пока пользователь не выйдет. Без TTY (и не на unix) —
// один кадр шириной 80 без цвета и без настройки терминала.
func Run(ctx context.Context, d Deps) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !d.TTY || !interactive() {
		return once(d)
	}
	if d.In == nil || d.Out == nil {
		return errors.New("internal: observe: no terminal")
	}
	ts, err := openTerm(d.In, d.Out)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		ts.restore()
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	stopSig := make(chan os.Signal, 2)
	signal.Notify(stopSig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
		case sig := <-stopSig:
			ts.restore()
			cancel()
			if sig == syscall.SIGTERM {
				signal.Stop(stopSig)
				signal.Reset(syscall.SIGTERM)
				_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			}
		}
	}()
	defer func() {
		signal.Stop(stopSig)
		cancel()
		wg.Wait()
	}()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	shots := make(chan snapMsg, 1)
	tick := d.Tick
	wg.Add(1)
	go func() {
		defer wg.Done()
		pollShots(ctx, tick, d.Snapshot, shots)
	}()

	w, h := 80, 24
	if d.Size != nil {
		if ww, hh := d.Size(); ww > 0 && hh > 0 {
			w, h = ww, hh
		}
	}
	var (
		m    observe.Model
		last string
		bad  int
	)
	// bufio заполняет буфер одним Read, и минимум буфера — 16 байт.
	// Обёртка отдаёт по байту, иначе выход заберёт хвост stdin у REPL.
	br := bufio.NewReader(oneByte{f: d.In})
	next := time.Now().Add(time.Second)
	fd := int(d.In.Fd())

	render := func() {
		if d.Crashes != nil {
			m = observe.Update(m, observe.CrashMsg{Crashes: d.Crashes()})
		}
		frame := view.Render(m, w, h, d.Color)
		if frame == last {
			return
		}
		last = frame
		ts.write("\x1b[H" + termFrame(frame))
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		for {
			drained := false
			select {
			case <-ctx.Done():
				return nil
			case sig := <-winch:
				_ = sig
				if d.Size != nil {
					if ww, hh := d.Size(); ww > 0 && hh > 0 {
						w, h = ww, hh
					}
				}
				last = ""
			case sz, ok := <-d.Resize:
				if ok && sz.W > 0 && sz.H > 0 {
					w, h = sz.W, sz.H
					last = ""
				}
			case msg := <-shots:
				if errors.Is(msg.err, observe.ErrSessionClosed) {
					return observe.ErrSessionClosed
				}
				if msg.err != nil {
					bad++
					m = observe.Update(m, observe.ShotMsg{Err: msg.err})
					if bad >= 3 {
						return errUnavailable(msg.err)
					}
				} else {
					bad = 0
					m = observe.Update(m, observe.ShotMsg{Actors: msg.shot.Actors, Links: msg.shot.Links})
					next = time.Now().Add(time.Second)
				}
			default:
				drained = true
			}
			if drained {
				break
			}
		}
		render()
		if m.Quit {
			return nil
		}
		ready, err := termio.PollReady(fd, pollTimeout(next))
		if err != nil {
			return err
		}
		if br.Buffered() > 0 || ready {
			k, err := readCommand(fd, br)
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			m = observe.Update(m, observe.KeyMsg{Key: k})
			if m.Quit {
				return nil
			}
		}
	}
}

func once(d Deps) error {
	var m observe.Model
	if d.Snapshot != nil {
		shot, err := d.Snapshot()
		if errors.Is(err, observe.ErrSessionClosed) {
			return err
		}
		if err != nil {
			m.Err = err.Error()
		} else {
			m = observe.Update(m, observe.ShotMsg{Actors: shot.Actors, Links: shot.Links})
		}
	}
	if d.Crashes != nil {
		m.Crashes = d.Crashes()
	}
	frame := view.Render(m, 80, 24, false)
	if d.Out != nil {
		_, _ = io.WriteString(d.Out, frame+"\n")
	}
	return nil
}

type snapMsg struct {
	shot observe.Shot
	err  error
}

func pollShots(ctx context.Context, tick <-chan time.Time, snap func() (observe.Shot, error), out chan snapMsg) {
	if snap == nil {
		return
	}
	if tick == nil {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		tick = t.C
	}
	send := func() {
		if ctx.Err() != nil {
			return
		}
		shot, err := snap()
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		offer(out, snapMsg{shot: shot, err: err})
	}
	send()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			send()
		}
	}
}

func offer(ch chan snapMsg, msg snapMsg) {
	select {
	case ch <- msg:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- msg:
	default:
	}
}

// oneByte читает из файла ровно один байт за Read.
type oneByte struct{ f *os.File }

func (o oneByte) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.f.Read(p[:1])
}

func pollTimeout(next time.Time) time.Duration {
	d := time.Until(next)
	if d < 0 {
		d = 0
	}
	if d > 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	return d
}

func errUnavailable(err error) error {
	return errors.New("snapshot unavailable: " + err.Error())
}

// readCommand читает одну клавишу. Байт берётся, только если он уже
// в буфере или Poll сказал, что fd читается. Одиночный ESC без хвоста
// за 25 мс — KeyEsc. Лишние байты stdin не забираются: буфер читателя
// на один байт.
func readCommand(fd int, br *bufio.Reader) (termio.Key, error) {
	b, err := br.ReadByte()
	if err != nil {
		return termio.Key{}, err
	}
	buf := []byte{b}
	if b != 0x1b {
		return termio.ReadKey(bufio.NewReader(bytes.NewReader(buf)))
	}
	deadline := time.Now().Add(25 * time.Millisecond)
	for {
		k, err := termio.ReadKey(bufio.NewReader(bytes.NewReader(buf)))
		if err == nil {
			return k, nil
		}
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return termio.Key{}, err
		}
		if br.Buffered() == 0 {
			wait := time.Until(deadline)
			if wait < 0 {
				return termio.Key{Code: termio.KeyEsc}, nil
			}
			ok, err := termio.PollReady(fd, wait)
			if err != nil {
				return termio.Key{}, err
			}
			if !ok && br.Buffered() == 0 {
				return termio.Key{Code: termio.KeyEsc}, nil
			}
		}
		b, err = br.ReadByte()
		if err != nil {
			return termio.Key{}, err
		}
		buf = append(buf, b)
		if len(buf) > 32 {
			return termio.Key{Code: termio.KeyUnknown}, nil
		}
	}
}
