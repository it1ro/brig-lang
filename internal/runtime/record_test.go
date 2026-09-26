package runtime

import "testing"

// T-73: записи §4.7 — равенство по §4.8, Inspect, JSON, Serialize.

func rec(typ string, kv ...any) Value {
	var fs []RecordField
	for i := 0; i < len(kv); i += 2 {
		fs = append(fs, RecordField{Name: kv[i].(string), Val: kv[i+1].(Value)})
	}
	return Record(typ, fs)
}

func TestRecordEqual(t *testing.T) {
	cases := []struct {
		a, b Value
		want bool
	}{
		{rec("User", "id", Int(1)), rec("User", "id", Int(1)), true},
		{rec("User", "id", Int(1)), rec("User", "id", Float(1)), true},
		{rec("User", "id", Int(1)), rec("User", "id", Int(2)), false},
		{rec("User", "id", Int(1)), rec("Admin", "id", Int(1)), false},
		{rec("User", "id", Int(1)), rec("", "id", Int(1)), false},
		{rec("", "a", Int(1), "b", Int(2)), rec("", "b", Int(2), "a", Int(1)), true},
		{rec("", "a", Int(1)), rec("", "a", Int(1), "b", Int(2)), false},
		{rec("", "a", Int(1)), rec("", "b", Int(1)), false},
		{rec(""), rec(""), true},
		{rec(""), Map(nil), false},
		{rec("", "a", rec("", "x", Int(1))), rec("", "a", rec("", "x", Int(1))), true},
	}
	for _, c := range cases {
		if got := Equal(c.a, c.b); got != c.want {
			t.Errorf("Equal(%s, %s) = %v, want %v", c.a.Inspect(), c.b.Inspect(), got, c.want)
		}
	}
}

func TestRecordInspect(t *testing.T) {
	cases := []struct {
		in   Value
		want string
	}{
		{rec("User", "id", Int(1), "name", Str("a")), `User{ id: 1, name: "a" }`},
		{rec("", "id", Int(1)), `{ id: 1 }`},
		{rec("User"), `User{}`},
		{rec(""), `{}`},
		{List(rec("", "x", Atom("ok"))), `[{ x: :ok }]`},
	}
	for _, c := range cases {
		if got := c.in.Inspect(); got != c.want {
			t.Errorf("Inspect = %q, want %q", got, c.want)
		}
	}
}

func TestRecordJSON(t *testing.T) {
	u := rec("User", "id", Int(1), "name", Str("a"))
	cases := []struct {
		in   Value
		opts JSONOptions
		want string
	}{
		{rec("User", "id", Int(1)), JSONOptions{}, `{"id":1}`},
		{rec("User", "id", Int(1)), JSONOptions{TypeTag: true}, `{"__type__":"User","id":1}`},
		{rec("User"), JSONOptions{TypeTag: true}, `{"__type__":"User"}`},
		{rec("", "id", Int(1)), JSONOptions{TypeTag: true}, `{"id":1}`},
		{rec(""), JSONOptions{}, `{}`},
		{u, JSONOptions{}, `{"id":1,"name":"a"}`},
		{rec("", "u", u), JSONOptions{TypeTag: true}, `{"u":{"__type__":"User","id":1,"name":"a"}}`},
	}
	for _, c := range cases {
		got, err := JSONEncodeOpts(c.in, c.opts)
		if err != nil {
			t.Fatalf("JSONEncodeOpts(%s): %v", c.in.Inspect(), err)
		}
		if got != c.want {
			t.Errorf("JSONEncodeOpts(%s, %+v) = %s, want %s", c.in.Inspect(), c.opts, got, c.want)
		}
	}
	if _, err := JSONEncode(rec("", "f", Func(&FuncValue{Name: "f"}))); err == nil {
		t.Error("JSONEncode of record with function field: want error")
	}
}

func TestRecordSerialize(t *testing.T) {
	if err := Serialize(rec("User", "id", Int(1))); err != nil {
		t.Errorf("Serialize(record): %v", err)
	}
	if err := Serialize(rec("", "p", Value{Kind: KindPid, Pid: 1})); err == nil {
		t.Error("Serialize(record with pid): want error")
	}
}
