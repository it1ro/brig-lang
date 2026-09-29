package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
	"syscall"

	"github.com/it1ro/brig-lang/internal/vm"
)

// osFiles — порт File (§12.12) на os. Ядро VM файлов не открывает (R14):
// cmd/brig подключает эту реализацию к машине (vm.SetFiles).
type osFiles struct{}

// fileChunk — наибольшая порция чтения (§12.12: не больше 64 КиБ).
const fileChunk = 64 << 10

var fileFlags = map[string]int{
	"read":   os.O_RDONLY,
	"write":  os.O_WRONLY | os.O_CREATE | os.O_TRUNC,
	"append": os.O_WRONLY | os.O_CREATE | os.O_APPEND,
}

// Open не ждёт ОС: файл открывает goroutine порта, она же исполняет
// запросы по порядку (G5).
func (osFiles) Open(path, mode string, emit func(vm.StreamEvent)) vm.Stream {
	st := &osStream{emit: emit, wake: make(chan struct{}, 1)}
	go st.run(path, fileFlags[mode])
	return st
}

// fileOp — запрос к файлу: чтение порции или запись data.
type fileOp struct {
	read bool
	data []byte
}

// osStream — ресурс одного порта File. Очередь операций без предела:
// run-loop не должен ждать goroutine файла, а объём записи ограничивает
// порог Port.write.
type osStream struct {
	emit func(vm.StreamEvent)
	wake chan struct{}

	mu   sync.Mutex
	ops  []fileOp
	done func() // не nil — вызван Close
}

func (st *osStream) Read()          { st.push(fileOp{read: true}) }
func (st *osStream) Write(b []byte) { st.push(fileOp{data: b}) }

func (st *osStream) Close(done func()) {
	st.mu.Lock()
	st.done = done
	st.mu.Unlock()
	st.signal()
}

func (st *osStream) push(op fileOp) {
	st.mu.Lock()
	st.ops = append(st.ops, op)
	st.mu.Unlock()
	st.signal()
}

func (st *osStream) signal() {
	select {
	case st.wake <- struct{}{}:
	default:
	}
}

// next — следующая операция; ok=false — очередь пуста и порт закрыт
// (done возвращается один раз).
func (st *osStream) next() (op fileOp, done func(), ok bool) {
	for {
		st.mu.Lock()
		if len(st.ops) > 0 {
			op = st.ops[0]
			st.ops = st.ops[1:]
			st.mu.Unlock()
			return op, nil, true
		}
		done = st.done
		st.mu.Unlock()
		if done != nil {
			return fileOp{}, done, false
		}
		<-st.wake
	}
}

// run открывает файл и исполняет операции. После ошибки порт закрыт
// (§12.12): остальные операции только снимаются с очереди. Close
// дописывает принятое и закрывает файл.
func (st *osStream) run(path string, flag int) {
	f, err := os.OpenFile(path, flag, 0o644)
	failed := err != nil
	for {
		op, done, ok := st.next()
		if !ok {
			if f != nil {
				// Ошибка закрытия после Port.close некому доставить (§12.12).
				_ = f.Close()
			}
			done()
			return
		}
		if failed {
			if err != nil {
				st.emit(vm.StreamEvent{Err: fileReason(err)})
				err = nil
			}
			continue
		}
		if op.read {
			err = st.read(f)
		} else if _, err = f.Write(op.data); err == nil {
			st.emit(vm.StreamEvent{Written: len(op.data)})
		}
		if err != nil {
			failed = true
			st.emit(vm.StreamEvent{Err: fileReason(err)})
			err = nil
		}
	}
}

// read — одна порция: данные, конец файла или ошибка.
func (st *osStream) read(f *os.File) error {
	buf := make([]byte, fileChunk)
	n, err := f.Read(buf)
	switch {
	case n > 0:
		st.emit(vm.StreamEvent{Data: buf[:n]})
		return nil
	case err == io.EOF:
		st.emit(vm.StreamEvent{EOF: true})
		return nil
	case err == nil:
		return io.ErrNoProgress
	}
	return err
}

// fileReason — атом причины :port_error (§12.12).
func fileReason(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "enoent"
	case errors.Is(err, fs.ErrPermission):
		return "eacces"
	case errors.Is(err, syscall.EISDIR):
		return "eisdir"
	case errors.Is(err, syscall.ENOTDIR):
		return "enotdir"
	}
	return "eio"
}
