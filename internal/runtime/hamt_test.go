package runtime

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

// hamtModel — эталон: список пар, равенство по KeyEqual.
type hamtModel []MapEntry

func (m hamtModel) find(k Value) int {
	for i, e := range m {
		if KeyEqual(e.Key, k) {
			return i
		}
	}
	return -1
}

func checkMapAgainst(t *testing.T, m Value, model hamtModel) {
	t.Helper()
	if m.Len() != len(model) {
		t.Fatalf("Len = %d, want %d", m.Len(), len(model))
	}
	for _, e := range model {
		got, ok := m.MapGet(e.Key)
		if !ok || !Equal(got, e.Val) {
			t.Fatalf("MapGet(%s) = %v, %v; want %s", e.Key.Inspect(), got.Inspect(), ok, e.Val.Inspect())
		}
	}
	if n := len(m.Entries()); n != len(model) {
		t.Fatalf("len(Entries) = %d, want %d", n, len(model))
	}
}

func TestHamtRandomOpsAgainstModel(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	m := Map(nil)
	var model hamtModel
	for step := 0; step < 4000; step++ {
		k := Int(int64(rng.Intn(300)))
		switch rng.Intn(3) {
		case 0, 1:
			v := Int(int64(step))
			m = m.MapPut(k, v)
			if i := model.find(k); i >= 0 {
				model[i].Val = v
			} else {
				model = append(model, MapEntry{Key: k, Val: v})
			}
		case 2:
			m = m.MapRemove(k)
			if i := model.find(k); i >= 0 {
				model = append(model[:i], model[i+1:]...)
			}
		}
		if step%250 == 0 {
			checkMapAgainst(t, m, model)
		}
	}
	checkMapAgainst(t, m, model)
}

func TestHamtPersistent(t *testing.T) {
	m1 := Map(nil).MapPut(Str("a"), Int(1)).MapPut(Str("b"), Int(2))
	m2 := m1.MapPut(Str("a"), Int(10)).MapPut(Str("c"), Int(3))
	m3 := m2.MapRemove(Str("b"))
	checkMapAgainst(t, m1, hamtModel{{Str("a"), Int(1)}, {Str("b"), Int(2)}})
	checkMapAgainst(t, m2, hamtModel{{Str("a"), Int(10)}, {Str("b"), Int(2)}, {Str("c"), Int(3)}})
	checkMapAgainst(t, m3, hamtModel{{Str("a"), Int(10)}, {Str("c"), Int(3)}})
	if got := m1.MapRemove(Str("zzz")); got.hamt != m1.hamt {
		t.Error("remove of a missing key must return the same table")
	}
}

// Форма дерева зависит только от набора ключей: put/remove в любом
// порядке дают равные по обходу таблицы (до порядка цепочек коллизий).
func TestHamtShapeCanonical(t *testing.T) {
	keys := make([]Value, 200)
	for i := range keys {
		keys[i] = Int(int64(i * 7919))
	}
	build := func(order []int, extra ...Value) Value {
		m := Map(nil)
		for _, i := range order {
			m = m.MapPut(keys[i], Int(1))
		}
		for _, e := range extra {
			m = m.MapPut(e, Int(1))
		}
		for _, e := range extra {
			m = m.MapRemove(e)
		}
		return m
	}
	rng := rand.New(rand.NewSource(2))
	a := build(rng.Perm(len(keys)))
	b := build(rng.Perm(len(keys)), Int(-5), Int(1<<40), Str("x"), Int(9999999))
	var ha, hb []uint64
	a.hamt.each(func(l *hamtLeaf) bool { ha = append(ha, l.hash); return true })
	b.hamt.each(func(l *hamtLeaf) bool { hb = append(hb, l.hash); return true })
	if len(ha) != len(hb) {
		t.Fatalf("len %d vs %d", len(ha), len(hb))
	}
	for i := range ha {
		if ha[i] != hb[i] {
			t.Fatalf("walk order differs at %d", i)
		}
	}
	if !Equal(a, b) {
		t.Error("tables with equal content must be Equal")
	}
}

// collide — ключи с общим полным хешем: проверка цепочек. Хеш подменить
// нельзя, поэтому работаем с hamt напрямую.
func TestHamtFullHashCollision(t *testing.T) {
	var h *hamt
	const hash = 0xdeadbeefcafebabe
	keys := []Value{Str("a"), Str("b"), Str("c")}
	for i, k := range keys {
		h = h.put(hash, k, Int(int64(i)))
	}
	other := Str("other")
	h = h.put(hash^1<<40, other, Int(99)) // общий префикс, разные хеши
	if h.len() != 4 {
		t.Fatalf("len = %d", h.len())
	}
	for i, k := range keys {
		if l := h.get(hash, k); l == nil || l.val.SmallInt != int64(i) {
			t.Fatalf("get %s", k.Inspect())
		}
	}
	h = h.put(hash, Str("b"), Int(50))
	if h.len() != 4 || h.get(hash, Str("b")).val.SmallInt != 50 {
		t.Fatal("replace in chain")
	}
	h = h.remove(hash, Str("a"))
	h = h.remove(hash, Str("c"))
	h = h.remove(hash, Str("b"))
	if h.len() != 1 || h.get(hash^1<<40, other) == nil {
		t.Fatalf("after removes len = %d", h.len())
	}
	if h.root == nil || len(h.root.slots) != 1 || h.root.slots[0].leaf == nil {
		t.Error("single leaf must collapse into the root slot")
	}
	h = h.remove(hash^1<<40, other)
	if h.len() != 0 {
		t.Fatalf("len = %d", h.len())
	}
}

func TestHashKeyConsistentWithKeyEqual(t *testing.T) {
	big70 := new(big.Int).Lsh(big.NewInt(1), 70)
	big70f, _ := new(big.Float).SetInt(big70).Float64()
	vals := []Value{
		Int(0), Float(0), Float(math.Copysign(0, -1)),
		Int(1), Float(1), Int(-3), Float(-3),
		Int(1 << 53), Float(1 << 53), Int(1<<53 + 1),
		Int(math.MinInt64), Float(-(1 << 63)),
		IntBig(big70), Float(big70f), IntBig(new(big.Int).Neg(big70)),
		Float(0.5), Float(math.Inf(1)), Float(math.Inf(-1)),
		Str("1"), Atom("1"), Bool(true), Bool(false), Unit,
		Tuple(Int(1), Int(2)), Tuple(Float(1), Float(2)), Tuple(),
		List(Int(1)), Vector(Int(1)), List(Float(1)), List(), Vector(),
		Map([]MapEntry{{Int(1), Int(2)}, {Str("k"), Int(3)}}),
		Map([]MapEntry{{Str("k"), Int(3)}, {Float(1), Int(2)}}),
		Set(Int(1), Int(2)), Set(Float(2), Float(1)),
		Variant("Some", Int(1)), Variant("Some", Float(1)), Variant("None"),
		Bytes([]byte("ab")), Str("ab"),
		Range(1, 5), Range(1, 6),
	}
	for i, a := range vals {
		for j, b := range vals {
			if KeyEqual(a, b) && hashKey(a) != hashKey(b) {
				t.Errorf("KeyEqual(%s, %s) but hashes differ [%d,%d]", a.Inspect(), b.Inspect(), i, j)
			}
		}
	}
}

func TestMapIntFloatSameKey(t *testing.T) {
	m := Map(nil).MapPut(Int(1), Str("a")).MapPut(Float(1), Str("b"))
	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1: 1 and 1.0 are one key", m.Len())
	}
	if v, _ := m.MapGet(Int(1)); v.Str != "b" {
		t.Errorf("got %s", v.Inspect())
	}
	m = m.MapPut(Float(-0.0), Int(0)).MapPut(Int(0), Int(7))
	if m.Len() != 2 {
		t.Errorf("Len = %d: -0.0 and 0 are one key", m.Len())
	}
}

func TestMapDecimalSeparateFromFloatAndInt(t *testing.T) {
	d := Value{Kind: KindDecimal, Dec: big.NewRat(1, 1)}
	m := Map(nil).MapPut(Int(1), Str("i")).MapPut(d, Str("d")).MapPut(Float(1), Str("f"))
	if m.Len() != 2 {
		t.Fatalf("Len = %d, want 2 (Decimal apart from Int/Float)", m.Len())
	}
	if v, _ := m.MapGet(d); v.Str != "d" {
		t.Errorf("Decimal key lost: %s", v.Inspect())
	}
	if v, _ := m.MapGet(Int(1)); v.Str != "f" {
		t.Errorf("Int/Float key: %s", v.Inspect())
	}
}

func TestMapSetTermOrderDeterministic(t *testing.T) {
	m := Map(nil)
	s := Set()
	for _, i := range rand.New(rand.NewSource(3)).Perm(100) {
		m = m.MapPut(Int(int64(i)), Int(0))
		s = s.SetAdd(Int(int64(i)))
	}
	for i, e := range m.Entries() {
		if e.Key.SmallInt != int64(i) {
			t.Fatalf("Entries[%d] = %s", i, e.Key.Inspect())
		}
	}
	for i, e := range s.Elems() {
		if e.SmallInt != int64(i) {
			t.Fatalf("Elems[%d] = %s", i, e.Inspect())
		}
	}
}

func TestSetOps(t *testing.T) {
	s := Set(Int(1), Float(1), Int(2))
	if s.Len() != 2 || !s.SetHas(Float(2)) || s.SetHas(Int(3)) {
		t.Fatalf("set = %s", s.Inspect())
	}
	if s.SetAdd(Int(2)).hamt != s.hamt {
		t.Error("adding a present element must return the same table")
	}
	s2 := s.SetRemove(Int(1))
	if s2.Len() != 1 || !s.SetHas(Int(1)) {
		t.Errorf("remove must not touch the old set: %s / %s", s2.Inspect(), s.Inspect())
	}
}

func TestMapEqualOrderIndependent(t *testing.T) {
	a := Map([]MapEntry{{Int(1), Str("x")}, {Int(2), Str("y")}})
	b := Map([]MapEntry{{Int(2), Str("y")}, {Float(1), Str("x")}})
	if !Equal(a, b) {
		t.Error("maps with equal pairs must be equal")
	}
	if Equal(a, a.MapPut(Int(2), Str("z"))) {
		t.Error("different values must not be equal")
	}
}

func TestMapAsKey(t *testing.T) {
	k1 := Map([]MapEntry{{Int(1), Int(2)}, {Int(3), Int(4)}})
	k2 := Map([]MapEntry{{Int(3), Int(4)}, {Float(1), Int(2)}})
	m := Map(nil).MapPut(k1, Str("v")).MapPut(Set(Int(1), Int(2)), Str("s"))
	if v, ok := m.MapGet(k2); !ok || v.Str != "v" {
		t.Error("map key must be found regardless of insertion order")
	}
	if v, ok := m.MapGet(Set(Int(2), Int(1))); !ok || v.Str != "s" {
		t.Error("set key must be found regardless of insertion order")
	}
}
