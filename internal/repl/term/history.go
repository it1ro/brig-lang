package term

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// HistoryLimit — сколько вводов хранит история.
const HistoryLimit = 10000

// History — вводы консоли от старых к новым. Файл истории — по вводу на
// строку: многострочный ввод хранится целиком, '\n' внутри записан как
// `\n`, обратная косая черта — как `\\`.
type History struct {
	// Path — файл истории; "" — история только в памяти.
	Path    string
	entries []string
}

// DefaultHistoryPath — `$XDG_STATE_HOME/brig/history`, без
// XDG_STATE_HOME — `~/.local/state/brig/history`.
func DefaultHistoryPath() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "brig", "history"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "brig", "history"), nil
}

// LoadHistory читает историю из path; нет файла — пустая история. Файл
// длиннее HistoryLimit обрезается до последних записей.
func LoadHistory(path string) (h *History, err error) {
	h = &History{Path: path}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			h.entries = append(h.entries, decodeEntry(line))
		}
	}
	if err = sc.Err(); err != nil {
		return h, err
	}
	if len(h.entries) > HistoryLimit {
		h.entries = h.entries[len(h.entries)-HistoryLimit:]
		return h, h.rewrite()
	}
	return h, nil
}

// Len — число записей.
func (h *History) Len() int { return len(h.entries) }

// At — запись i, 0 — самая старая.
func (h *History) At(i int) string { return h.entries[i] }

// Suggest — суффикс самой новой записи, для которой prefix — начало.
// Пустой prefix и точное совпадение суффикса не дают.
func (h *History) Suggest(prefix string) string {
	if h == nil || prefix == "" {
		return ""
	}
	for i := len(h.entries) - 1; i >= 0; i-- {
		e := h.entries[i]
		if strings.HasPrefix(e, prefix) && len(e) > len(prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}

// Add добавляет ввод: без пустых строк в конце, пустой ввод и повтор
// последней записи не пишутся. Запись дописывается в файл сразу.
func (h *History) Add(entry string) error {
	entry = strings.TrimRight(entry, " \t\n")
	if strings.TrimSpace(entry) == "" {
		return nil
	}
	if n := len(h.entries); n > 0 && h.entries[n-1] == entry {
		return nil
	}
	h.entries = append(h.entries, entry)
	if len(h.entries) > HistoryLimit {
		h.entries = h.entries[len(h.entries)-HistoryLimit:]
	}
	if h.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(encodeEntry(entry) + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// rewrite перезаписывает файл текущими записями.
func (h *History) rewrite() error {
	var b strings.Builder
	for _, e := range h.entries {
		b.WriteString(encodeEntry(e))
		b.WriteByte('\n')
	}
	tmp := h.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.Path)
}

// search ищет запись, содержащую q, от записи from к более старым.
// Возвращает индекс записи и позицию совпадения в рунах; -1 — нет.
func (h *History) search(q string, from int) (int, int) {
	for i := min(from, len(h.entries)-1); i >= 0; i-- {
		if at := strings.Index(h.entries[i], q); at >= 0 {
			return i, len([]rune(h.entries[i][:at]))
		}
	}
	return -1, 0
}

func encodeEntry(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`).Replace(s)
}

func decodeEntry(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			if s[i] == 'n' {
				b.WriteByte('\n')
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
