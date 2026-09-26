package runtime

import (
	"math"
	"strings"
	"testing"
)

func mustEncode(t *testing.T, v Value) string {
	t.Helper()
	s, err := JSONEncode(v)
	if err != nil {
		t.Fatalf("JSONEncode(%s): %v", v.Inspect(), err)
	}
	return s
}

func TestJSONEncodeScalars(t *testing.T) {
	cases := []struct {
		in   Value
		want string
	}{
		{Unit, "null"},
		{Bool(true), "true"},
		{Bool(false), "false"},
		{Int(42), "42"},
		{Int(-7), "-7"},
		{Str("hi"), `"hi"`},
		{Str(`a"b\n`), `"a\"b\\n"`},
		{Atom("ok"), `":ok"`},
	}
	for _, c := range cases {
		got := mustEncode(t, c.in)
		if got != c.want {
			t.Errorf("encode %s: got %q, want %q", c.in.Inspect(), got, c.want)
		}
	}
}

func TestJSONEncodeCollections(t *testing.T) {
	l := List(Int(1), Int(2), Int(3))
	if got := mustEncode(t, l); got != "[1,2,3]" {
		t.Errorf("list: got %q", got)
	}

	m := Map([]MapEntry{
		{Key: Str("a"), Val: Int(1)},
		{Key: Str("b"), Val: Str("x")},
	})
	got := mustEncode(t, m)
	// порядок Map сохраняется как есть, но для теста устойчивости
	// проверим через парсинг обратно
	back, err := JSONDecode(got)
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(back, m) {
		t.Errorf("round-trip map mismatch: %s vs %s", back.Inspect(), m.Inspect())
	}
}

func TestJSONEncodeVariant(t *testing.T) {
	if got := mustEncode(t, Variant("None")); got != "null" {
		t.Errorf("None: got %q", got)
	}
	if got := mustEncode(t, Variant("Some", Int(42))); got != "42" {
		t.Errorf("Some(42): got %q", got)
	}
	if got := mustEncode(t, Variant("Ok", Str("x"))); got != `"x"` {
		t.Errorf("Ok(x): got %q", got)
	}
	if got := mustEncode(t, Variant("Error", Atom("boom"))); got != `{"error":":boom"}` {
		t.Errorf("Error: got %q", got)
	}
}

func TestJSONEncodeBytes(t *testing.T) {
	b := Bytes([]byte{0x89, 'P', 'N', 'G'})
	got := mustEncode(t, b)
	if !strings.HasPrefix(got, `{"$bytes":"`) {
		t.Errorf("bytes: got %q", got)
	}
	back, err := JSONDecode(got)
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(back, b) {
		t.Errorf("bytes round-trip mismatch: %s vs %s", back.Inspect(), b.Inspect())
	}
}

func TestJSONEncodeRejectsFunction(t *testing.T) {
	fn := Func(&FuncValue{Name: "f", Arity: 0, IsNative: true})
	if _, err := JSONEncode(fn); err == nil {
		t.Fatal("encode(fn) should fail")
	}
	if _, err := JSONEncode(Value{Kind: KindPid, Pid: 1}); err == nil {
		t.Fatal("encode(pid) should fail")
	}
}

func TestJSONDecodeScalars(t *testing.T) {
	cases := []struct {
		in   string
		want Value
	}{
		{"null", Unit},
		{"true", Bool(true)},
		{"false", Bool(false)},
		{"42", Int(42)},
		{"-7", Int(-7)},
		{`"hi"`, Str("hi")},
	}
	for _, c := range cases {
		got, err := JSONDecode(c.in)
		if err != nil {
			t.Fatalf("decode %q: %v", c.in, err)
		}
		if !Equal(got, c.want) {
			t.Errorf("decode %q: got %s, want %s", c.in, got.Inspect(), c.want.Inspect())
		}
	}
}

func TestJSONDecodeCollections(t *testing.T) {
	v, err := JSONDecode(`[1,2,3]`)
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(v, List(Int(1), Int(2), Int(3))) {
		t.Errorf("decode array: got %s", v.Inspect())
	}

	v, err = JSONDecode(`{"a": 1, "b": "x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != KindMap || len(v.Map) != 2 {
		t.Errorf("decode object: got %s", v.Inspect())
	}
}

func TestJSONDecodeMalformed(t *testing.T) {
	bad := []string{
		`{`,
		`[1,2`,
		`"unterminated`,
		`{"a": }`,
		`1 2`,
		``,
	}
	for _, s := range bad {
		if _, err := JSONDecode(s); err == nil {
			t.Errorf("decode %q: want error, got nil", s)
		}
	}
}

func TestJSONBytesMarkerCollision(t *testing.T) {
	// Map with key "$bytes" must round-trip as Map, not become Bytes.
	orig := Map([]MapEntry{{Key: Str("$bytes"), Val: Str("aGk=")}})
	enc, err := JSONEncode(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := JSONDecode(enc)
	if err != nil {
		t.Fatalf("decode %q: %v", enc, err)
	}
	if got.Kind == KindBytes {
		t.Fatalf("collision: decode gave Bytes %s, want Map %s (enc=%q)", got.Inspect(), orig.Inspect(), enc)
	}
	if !Equal(got, orig) {
		t.Fatalf("round-trip: got %s, want %s (enc=%q)", got.Inspect(), orig.Inspect(), enc)
	}
	// Bytes themselves still round-trip via the $bytes marker.
	b := Bytes([]byte("hi")) // "hi" base64-encoded is aGk=
	bEnc, err := JSONEncode(b)
	if err != nil {
		t.Fatalf("encode bytes: %v", err)
	}
	bGot, err := JSONDecode(bEnc)
	if err != nil {
		t.Fatalf("decode bytes %q: %v", bEnc, err)
	}
	if !Equal(bGot, b) {
		t.Fatalf("bytes round-trip: got %s, want %s", bGot.Inspect(), b.Inspect())
	}
}

func TestJSONEncodeInfIsError(t *testing.T) {
	for _, v := range []Value{
		Float(math.Inf(1)),
		Float(math.Inf(-1)),
		Float(math.NaN()),
	} {
		s, err := JSONEncode(v)
		if err == nil {
			t.Fatalf("encode %s: want error (RFC 8259), got %q", v.Inspect(), s)
		}
		if strings.Contains(s, "Inf") || strings.Contains(s, "NaN") {
			t.Fatalf("encode %s: leaked non-finite into output %q", v.Inspect(), s)
		}
	}
}

func TestJSONFloatRoundTrip(t *testing.T) {
	orig := Float(1.0)
	enc, err := JSONEncode(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := JSONDecode(enc)
	if err != nil {
		t.Fatalf("decode %q: %v", enc, err)
	}
	if got.Kind != KindFloat {
		t.Fatalf("round-trip Float(1.0): got kind %s value %s (enc=%q), want Float", got.Kind, got.Inspect(), enc)
	}
	if got.Float != 1.0 {
		t.Fatalf("round-trip Float(1.0): got %v", got.Float)
	}
}
