package vm

import (
	"math"
	"math/big"
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// fastOperands — операнды, на которых быстрый путь (T-276) сверяется с
// медленным: границы smallint, big.Int, Float с NaN/±Inf/±0.0, Decimal и
// не-числа.
func fastOperands() []runtime.Value {
	big1 := new(big.Int).Add(new(big.Int).SetInt64(math.MaxInt64), big.NewInt(1))
	return []runtime.Value{
		runtime.Int(0),
		runtime.Int(1),
		runtime.Int(-1),
		runtime.Int(3),
		runtime.Int(math.MaxInt64),
		runtime.Int(math.MinInt64),
		runtime.IntBig(big1),
		runtime.IntBig(new(big.Int).Neg(big1)),
		runtime.Int(9007199254740993), // 2^53+1: точность Float теряется
		runtime.Float(0),
		runtime.Float(math.Copysign(0, -1)),
		runtime.Float(1),
		runtime.Float(-1.5),
		runtime.Float(3),
		runtime.Float(9007199254740992), // 2^53 как Float
		runtime.Float(math.NaN()),
		runtime.Float(math.Inf(1)),
		runtime.Float(math.Inf(-1)),
		runtime.Decimal(big.NewRat(3, 1)),
		runtime.Str("x"),
		runtime.Atom("a"),
		runtime.Unit,
	}
}

// sameValue — равенство результата с точностью до вида и представления: 1 и
// 1.0 — разные результаты, поэтому runtime.Equal одного недостаточно. Float
// сверяется побитово: NaN не равен себе по runtime.Equal, а знак нуля
// результата у быстрого и медленного путей обязан совпадать.
func sameValue(a, b runtime.Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == runtime.KindFloat {
		return math.Float64bits(a.Float) == math.Float64bits(b.Float)
	}
	return a.IsSmall == b.IsSmall && runtime.Equal(a, b)
}

// TestFastArithMatchesSlow — быстрый путь ADD/SUB/MUL (T-276) там, где он
// берётся, даёт ровно тот же результат, что add/sub/mul, и никогда не
// берётся там, где медленный путь поднимает ошибку.
func TestFastArithMatchesSlow(t *testing.T) {
	ops := []struct {
		name string
		fast func(dst, a, b *runtime.Value) bool
		slow func(a, b runtime.Value) (runtime.Value, error)
	}{
		{"add", fastAdd, add},
		{"sub", fastSub, sub},
		{"mul", fastMul, mul},
	}
	vals := fastOperands()
	for _, op := range ops {
		for _, a := range vals {
			for _, b := range vals {
				var got runtime.Value
				if !op.fast(&got, &a, &b) {
					continue
				}
				want, err := op.slow(a, b)
				if err != nil {
					t.Fatalf("%s(%s, %s): быстрый путь дал %s, медленный — ошибку %v",
						op.name, a.Inspect(), b.Inspect(), got.Inspect(), err)
				}
				if !sameValue(got, want) {
					t.Errorf("%s(%s, %s) = %s, want %s",
						op.name, a.Inspect(), b.Inspect(), got.Inspect(), want.Inspect())
				}
			}
		}
	}
}

// TestFastArithCoversSmallInt — быстрый путь обязан брать на себя пару
// smallint без переполнения и пару Float: иначе оптимизация не работает.
func TestFastArithCoversSmallInt(t *testing.T) {
	var dst runtime.Value
	pairs := [][2]runtime.Value{
		{runtime.Int(2), runtime.Int(3)},
		{runtime.Float(2.5), runtime.Float(0.5)},
	}
	for _, p := range pairs {
		for _, op := range []struct {
			name string
			fast func(dst, a, b *runtime.Value) bool
		}{{"add", fastAdd}, {"sub", fastSub}, {"mul", fastMul}} {
			if !op.fast(&dst, &p[0], &p[1]) {
				t.Errorf("%s(%s, %s): быстрый путь не взят", op.name, p[0].Inspect(), p[1].Inspect())
			}
		}
	}
	// Переполнение smallint уходит на медленный путь (результат — big.Int).
	maxInt := runtime.Int(math.MaxInt64)
	one := runtime.Int(1)
	if fastAdd(&dst, &maxInt, &one) {
		t.Error("fastAdd взял переполнение MaxInt64+1")
	}
}

// TestFastEqMatchesSlow — быстрое равенство совпадает с runtime.Equal, и
// Decimal×Float (ловимый :type_error, §7.4) на него не попадает.
func TestFastEqMatchesSlow(t *testing.T) {
	vals := fastOperands()
	for _, a := range vals {
		for _, b := range vals {
			eq, ok := fastEq(&a, &b)
			if !ok {
				continue
			}
			if err := checkMixedEq(a, b); err != nil {
				t.Fatalf("fastEq(%s, %s) взят, хотя медленный путь даёт %v",
					a.Inspect(), b.Inspect(), err)
			}
			if want := runtime.Equal(a, b); eq != want {
				t.Errorf("fastEq(%s, %s) = %v, want %v", a.Inspect(), b.Inspect(), eq, want)
			}
		}
	}
}

// TestFastCmpMatchesSlow — быстрые LT/GT/LE/GE совпадают с медленным путём,
// включая правило §7.4: NaN в операнде — false для всех четырёх.
func TestFastCmpMatchesSlow(t *testing.T) {
	vals := fastOperands()
	for _, op := range []OpCode{LT, GT, LE, GE} {
		for _, a := range vals {
			for _, b := range vals {
				res, ok := fastCmp(op, &a, &b)
				if !ok {
					continue
				}
				if err := checkMixedCmp(a, b); err != nil {
					t.Fatalf("fastCmp(%s, %s, %s) взят, хотя медленный путь даёт %v",
						opNames[op], a.Inspect(), b.Inspect(), err)
				}
				want := false
				if !runtime.IsNaNOperand(a, b) {
					c, err := runtime.Compare(a, b)
					if err != nil {
						t.Fatalf("fastCmp(%s, %s, %s) взят, хотя Compare даёт %v",
							opNames[op], a.Inspect(), b.Inspect(), err)
					}
					switch op {
					case LT:
						want = c < 0
					case GT:
						want = c > 0
					case LE:
						want = c <= 0
					case GE:
						want = c >= 0
					}
				}
				if res != want {
					t.Errorf("fastCmp(%s, %s, %s) = %v, want %v",
						opNames[op], a.Inspect(), b.Inspect(), res, want)
				}
			}
		}
	}
}
