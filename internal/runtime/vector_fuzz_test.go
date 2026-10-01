package runtime

import "testing"

// FuzzVectorOps гоняет случайную последовательность push и set против
// эталонного среза: длина, чтение по индексу, порядок обхода и равенство
// с пересобранным вектором должны совпадать (T-273).
func FuzzVectorOps(f *testing.F) {
	f.Add([]byte{0, 0, 0})
	f.Add([]byte{0, 0, 2, 0, 2, 5})
	f.Add(make([]byte, 100))
	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 4096 {
			ops = ops[:4096]
		}
		v, model := Vector(), []Value{}
		for i, b := range ops {
			if b%3 == 2 && len(model) > 0 {
				j := int(b/3) % len(model)
				e := Int(int64(-i - 1))
				v, model[j] = v.VecSet(j, e), e
				continue
			}
			e := Int(int64(i))
			v, model = v.VecPush(e), append(model, e)
		}
		if v.Len() != len(model) {
			t.Fatalf("Len = %d, want %d", v.Len(), len(model))
		}
		for i, want := range model {
			if got := v.At(i); !Equal(got, want) {
				t.Fatalf("At(%d) = %s, want %s", i, got.Inspect(), want.Inspect())
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
		rebuilt := Vector(model...)
		if !Equal(v, rebuilt) || !MatchEqual(v, rebuilt) {
			t.Fatalf("вектор не равен пересобранному: %s vs %s", v.Inspect(), rebuilt.Inspect())
		}
		if c, err := Compare(v, rebuilt); err != nil || c != 0 {
			t.Fatalf("Compare = %d, %v", c, err)
		}
		if n := len(v.Elems()); n != len(model) {
			t.Fatalf("len(Elems) = %d, want %d", n, len(model))
		}
	})
}
