# bench-heap — стенд замеров модели heap

Замеры Z1–Z4 из [11-memory.md](../11-memory.md); результаты и выводы —
[23-heap-measurements.md](../23-heap-measurements.md) (T-101, #170).

```sh
go test -tags heapbench -v ./web-mvp-research/bench-heap/            # всё, ~1 мин, пик ~5 ГиБ
go test -tags heapbench -v -run Z2 ./web-mvp-research/bench-heap/    # один замер
go test -tags heapbench -run Z2 ./web-mvp-research/bench-heap/ -args -prof /tmp  # + heap-профиль Z2
```

Без тега `heapbench` пакет пуст: в `go test ./...` и `make all` стенд не
входит.

| Файл | Замер |
|---|---|
| `z1_test.go` | Z1: размер бинарника, пиковый RSS и время процесса `brig` |
| `z2_test.go`, `testdata/idle.brig`, `testdata/idle_timer.brig` | Z0: размеры структур; Z2: память на простаивающего актора; Z2b: таймер рядом с ними |
| `z3_test.go`, `testdata/gcload.brig` | Z3: паузы и CPU GC при живом heap 0 / 0.5 / 2 ГиБ |
| `z4_test.go`, `deepcopy_test.go`, `testdata/sendpp.brig` | Z4: `send` сейчас и цена глубокой копии (текущий `Value` и компактная модель) |
| `harness_test.go` | запуск `.brig` с нативными глобалами стенда (`probe`, `mark`, `clock_ns`, `bench_*`) |
