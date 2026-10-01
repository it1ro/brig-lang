package runtime

import (
	"math"
	"math/rand"
	"testing"
)

// buildVector — вектор 0..n-1 цепочкой VecPush.
func buildVector(n int) Value {
	v := Vector()
	for i := range n {
		v = v.VecPush(Int(int64(i)))
	}
	return v
}

func checkVectorAgainst(t *testing.T, v Value, model []Value) {
	t.Helper()
	if v.Len() != len(model) {
		t.Fatalf("Len = %d, want %d", v.Len(), len(model))
	}
	for i, want := range model {
		if got := v.At(i); !Equal(got, want) {
			t.Fatalf("At(%d) = %s, want %s", i, got.Inspect(), want.Inspect())
		}
	}
	elems := v.Elems()
	if len(elems) != len(model) {
		t.Fatalf("len(Elems) = %d, want %d", len(elems), len(model))
	}
	for i, want := range model {
		if !Equal(elems[i], want) {
			t.Fatalf("Elems[%d] = %s, want %s", i, elems[i].Inspect(), want.Inspect())
		}
	}
	i := 0
	for e := range v.Items() {
		if !Equal(e, model[i]) {
			t.Fatalf("Items[%d] = %s, want %s", i, e.Inspect(), model[i].Inspect())
		}
		i++
	}
	if i != len(model) {
		t.Fatalf("Items yielded %d, want %d", i, len(model))
	}
}

// Границы уровней trie: 32 (лист), 1024 (второй уровень), 32768 (третий).
func TestVectorPushAcrossLevels(t *testing.T) {
	for _, n := range []int{0, 1, 31, 32, 33, 1023, 1024, 1025, 32768, 32769} {
		v, model := Vector(), make([]Value, 0, n)
		for i := range n {
			e := Int(int64(i))
			v = v.VecPush(e)
			model = append(model, e)
		}
		if v.Kind != KindVector {
			t.Fatalf("Kind = %s", v.Kind)
		}
		checkVectorAgainst(t, v, model)
	}
}

func TestVectorSetAcrossLevels(t *testing.T) {
	const n = 2000
	v := buildVector(n)
	for _, i := range []int{0, 31, 32, 33, 1023, 1024, 1025, n - 1} {
		got := v.VecSet(i, Atom("x"))
		if e := got.At(i); !Equal(e, Atom("x")) {
			t.Fatalf("VecSet(%d) then At = %s", i, e.Inspect())
		}
		if got.Len() != n {
			t.Fatalf("VecSet(%d): Len = %d, want %d", i, got.Len(), n)
		}
		// Прежняя версия не изменилась, соседи — на месте.
		if e := v.At(i); !Equal(e, Int(int64(i))) {
			t.Fatalf("VecSet(%d) changed source: At = %s", i, e.Inspect())
		}
		if i > 0 && !Equal(got.At(i-1), Int(int64(i-1))) {
			t.Fatalf("VecSet(%d) changed At(%d)", i, i-1)
		}
		if i+1 < n && !Equal(got.At(i+1), Int(int64(i+1))) {
			t.Fatalf("VecSet(%d) changed At(%d)", i, i+1)
		}
	}
}

func TestVectorPersistent(t *testing.T) {
	v1 := buildVector(100)
	v2 := v1.VecPush(Atom("tail"))
	v3 := v1.VecSet(0, Atom("head"))
	if v1.Len() != 100 || v2.Len() != 101 || v3.Len() != 100 {
		t.Fatalf("lens = %d, %d, %d", v1.Len(), v2.Len(), v3.Len())
	}
	if !Equal(v1.At(0), Int(0)) || !Equal(v3.At(0), Atom("head")) {
		t.Fatalf("At(0) = %s, %s", v1.At(0).Inspect(), v3.At(0).Inspect())
	}
	if !Equal(v2.At(100), Atom("tail")) {
		t.Fatalf("v2.At(100) = %s", v2.At(100).Inspect())
	}
}

// Общие ветви: set копирует путь, а не вектор целиком, поэтому число
// аллокаций на set не растёт с длиной (§4.4, O(log₃₂ n)).
func TestVectorSetSharesBranches(t *testing.T) {
	small, big := buildVector(64), buildVector(16384)
	perSet := func(v Value) float64 {
		return testing.AllocsPerRun(50, func() { _ = v.VecSet(1, Atom("x")) })
	}
	a, b := perSet(small), perSet(big)
	if b > a+6 {
		t.Fatalf("allocs per VecSet: 64 элементов %v, 16384 элементов %v", a, b)
	}
	// Неизменённые листья разделяются: указатели совпадают. Длина 4100 —
	// tail неполный, поэтому push в него дерево не трогает.
	v := buildVector(4100)
	w := v.VecSet(0, Atom("x"))
	if v.vector.root.kids[1] != w.vector.root.kids[1] {
		t.Fatal("VecSet скопировал ветвь вне пути")
	}
	// push разделяет всё дерево, пока tail не полон.
	p := v.VecPush(Atom("y"))
	if v.vector.root != p.vector.root {
		t.Fatal("VecPush скопировал дерево")
	}
}

func TestVectorBuilderMatchesPush(t *testing.T) {
	for _, n := range []int{0, 1, 32, 33, 1025} {
		var zero VectorBuilder
		b := NewVectorBuilder(n)
		for i := range n {
			b.Add(Int(int64(i)))
			zero.Add(Int(int64(i)))
		}
		fromBuilder, fromZero, fromPush := b.Vector(), zero.Vector(), buildVector(n)
		if !Equal(fromBuilder, fromPush) || !Equal(fromZero, fromPush) {
			t.Fatalf("n = %d: builder = %s, zero = %s, push = %s",
				n, fromBuilder.Inspect(), fromZero.Inspect(), fromPush.Inspect())
		}
		if fromBuilder.Len() != n {
			t.Fatalf("n = %d: Len = %d", n, fromBuilder.Len())
		}
	}
}

func TestVectorBuilderFromSharesBase(t *testing.T) {
	base := buildVector(1000)
	b := VectorBuilderFrom(base)
	b.Add(Atom("a"))
	b.Add(Atom("b"))
	got := b.Vector()
	if got.Len() != 1002 {
		t.Fatalf("Len = %d, want 1002", got.Len())
	}
	if !Equal(got.At(999), Int(999)) || !Equal(got.At(1001), Atom("b")) {
		t.Fatalf("At(999) = %s, At(1001) = %s", got.At(999).Inspect(), got.At(1001).Inspect())
	}
	if base.Len() != 1000 {
		t.Fatalf("base Len = %d", base.Len())
	}
	if base.vector.root.kids[0] != got.vector.root.kids[0] {
		t.Fatal("VectorBuilderFrom скопировал ветви базы")
	}
	// Не вектор — пустой построитель.
	empty := VectorBuilderFrom(List(Int(1)))
	if v := empty.Vector(); v.Kind != KindVector || v.Len() != 0 {
		t.Fatalf("VectorBuilderFrom(List) = %s", v.Inspect())
	}
}

// Короткий вектор отдаёт tail без копии, длинный — собранный срез.
func TestVectorElemsShareTail(t *testing.T) {
	short := buildVector(5)
	if a, b := short.Elems(), short.Elems(); &a[0] != &b[0] {
		t.Fatal("Elems короткого вектора копирует tail")
	}
	long := buildVector(100)
	if n := len(long.Elems()); n != 100 {
		t.Fatalf("len(Elems) = %d", n)
	}
	// Ранний выход из Items не обходит остаток.
	seen := 0
	for range long.Items() {
		seen++
		if seen == 3 {
			break
		}
	}
	if seen != 3 {
		t.Fatalf("Items early stop = %d", seen)
	}
}

func TestVectorEqualAndCompare(t *testing.T) {
	a, b := buildVector(1000), buildVector(1000)
	if !Equal(a, b) || !MatchEqual(a, b) {
		t.Fatal("равные векторы не равны")
	}
	if Equal(a, a.VecSet(500, Atom("x"))) {
		t.Fatal("вектор равен изменённому")
	}
	if Equal(a, a.VecPush(Int(1000))) {
		t.Fatal("вектор равен удлинённому")
	}
	// MatchEqual строг по виду (§4.8): 1 не матчит 1.0, Equal — матчит.
	one, onef := Vector(Int(1)), Vector(Float(1))
	if MatchEqual(one, onef) {
		t.Fatal("MatchEqual вектора из Int с вектором из Float")
	}
	if !Equal(one, onef) {
		t.Fatal("Equal вектора из Int с вектором из Float даёт false")
	}
	// Term order (§7.4): сначала длина, потом элементы.
	cmp := func(x, y Value) int {
		c, err := Compare(x, y)
		if err != nil {
			t.Fatalf("Compare: %v", err)
		}
		return c
	}
	if cmp(buildVector(3), buildVector(4)) >= 0 {
		t.Fatal("короткий вектор не меньше длинного")
	}
	if cmp(a, b) != 0 {
		t.Fatal("равные векторы не равны по Compare")
	}
	if cmp(a.VecSet(999, Int(1000)), a) <= 0 {
		t.Fatal("больший элемент не даёт больший вектор")
	}
}

// NaN: равенство нерефлексивно (§4.8), поэтому пропуск общих поддеревьев
// для таких векторов выключен — `v == v` остаётся false, как у списка.
func TestVectorEqualNaN(t *testing.T) {
	nan := Float(math.NaN())
	v := buildVector(100).VecSet(50, nan)
	if Equal(v, v) || MatchEqual(v, v) {
		t.Fatal("вектор с NaN равен себе")
	}
	if Equal(Vector(nan), Vector(nan)) {
		t.Fatal("два вектора из NaN равны")
	}
	// Контрольная точка: у List поведение то же.
	if Equal(List(nan), List(nan)) {
		t.Fatal("два списка из NaN равны")
	}
	// Compare определён и для NaN (cmpNaN): вектор равен себе по порядку.
	if c, err := Compare(v, v); err != nil || c != 0 {
		t.Fatalf("Compare(v, v) = %d, %v", c, err)
	}
	// Вектор без NaN пропуск поддеревьев не меняет.
	plain := buildVector(100)
	if !Equal(plain, plain) {
		t.Fatal("вектор без NaN не равен себе")
	}
}

func TestVectorRandomOpsAgainstModel(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	v, model := Vector(), []Value{}
	for step := range 3000 {
		if n := len(model); n > 0 && rng.Intn(4) == 0 {
			i := rng.Intn(n)
			e := Int(int64(-step))
			v = v.VecSet(i, e)
			model[i] = e
			continue
		}
		e := Int(int64(step))
		v = v.VecPush(e)
		model = append(model, e)
		if step%500 == 0 {
			checkVectorAgainst(t, v, model)
		}
	}
	checkVectorAgainst(t, v, model)
}

func TestVectorAsMapKey(t *testing.T) {
	k1, k2 := buildVector(100), buildVector(100)
	m := Map(nil).MapPut(k1, Atom("v"))
	got, ok := m.MapGet(k2)
	if !ok || !Equal(got, Atom("v")) {
		t.Fatalf("MapGet по равному вектору = %s, %v", got.Inspect(), ok)
	}
	if _, ok := m.MapGet(k2.VecPush(Int(0))); ok {
		t.Fatal("длиннее вектор нашёлся ключом")
	}
	if _, ok := m.MapGet(Vector(Float(1))); ok {
		t.Fatal("другой вектор нашёлся ключом")
	}
	// Int и Float с целым значением — один ключ (§4.8).
	if _, ok := Map(nil).MapPut(Vector(Int(1)), Unit).MapGet(Vector(Float(1))); !ok {
		t.Fatal("%[1] и %[1.0] — разные ключи")
	}
}

func TestVectorInspect(t *testing.T) {
	if got := Vector().Inspect(); got != "%[]" {
		t.Fatalf("Inspect пустого = %s", got)
	}
	if got := Vector(Int(1), Str("a"), Atom("b")).Inspect(); got != `%[1, "a", :b]` {
		t.Fatalf("Inspect = %s", got)
	}
	if got := buildVector(33).At(32); !Equal(got, Int(32)) {
		t.Fatalf("At(32) = %s", got.Inspect())
	}
}
