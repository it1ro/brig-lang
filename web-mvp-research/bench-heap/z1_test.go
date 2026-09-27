//go:build heapbench

package benchheap

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"testing"
	"time"
)

// TestZ1ProcessRSS — размер бинарника, пиковый RSS и время процесса `brig`
// (Z1; hello-сервера нет — HTTP ещё не реализован, берём hello-скрипт).
func TestZ1ProcessRSS(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "brig")
	build := exec.Command("go", "build", "-trimpath", "-o", bin, "../../cmd/brig")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	st, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Z1 binary=%s", mib(uint64(st.Size())))

	cases := []struct {
		name string
		args []string
	}{
		{"usage (без аргументов)", nil},
		{"run hello.brig", []string{"run", "testdata/hello.brig"}},
	}
	for _, c := range cases {
		const runs = 21
		var rss []int64
		var wall []time.Duration
		for range runs {
			cmd := exec.Command(bin, c.args...)
			start := time.Now()
			_ = cmd.Run() // usage завершается с ненулевым кодом — это нормально
			wall = append(wall, time.Since(start))
			ru := cmd.ProcessState.SysUsage().(*syscall.Rusage)
			rss = append(rss, ru.Maxrss) // KiB в Linux
		}
		sort.Slice(rss, func(i, j int) bool { return rss[i] < rss[j] })
		sort.Slice(wall, func(i, j int) bool { return wall[i] < wall[j] })
		t.Logf("Z1 %-24s maxrss_median=%.1f MiB wall_median=%s wall_p90=%s",
			c.name, float64(rss[runs/2])/1024, wall[runs/2].Round(10*time.Microsecond), wall[runs*9/10].Round(10*time.Microsecond))
	}
}
