// Package runtime — JSON encode/decode (§4.7, Must из §16).
//
// Encode (Value → JSON):
//
//	Unit         → null
//	Bool         → true/false
//	Int          → number
//	Float        → number (Inf/NaN — unsupported, RFC 8259)
//	Str          → string
//	Atom         → ":name" (строка с ведущим ':')
//	Decimal      → "dec\"...\"" (строка — точность важнее)
//	Bytes        → {"$bytes": "<base64>"} (маркер для round-trip)
//	List/Vector/Set/Tuple → array
//	Map          → object (ключи только Str/Atom; ключи с префиксом "$"
//	               экранируются как "$$…", чтобы не пересекаться с маркером)
//	None         → null
//	Some(x)      → прозрачно, encode(x)
//	иной вариант → {"tag": "Name", "args": [...]}
//	запись       → object по полям; номинальная с TypeTag —
//	               {"__type__": "Name", ...} (§4.7)
//	Ok/Error, Function/Closure/Pid/Ref/Port, Range, ключ Map не Str/Atom →
//	               *JSONError{(:unsupported, v)}
//
// Decode (JSON → Value); ошибка — *JSONError с причиной :syntax,
// (:number, s), :depth_limit, :trailing_data или :invalid_utf8:
//
//	null         → None
//	true/false   → Bool
//	integer      → Int
//	fractional   → Float (в т.ч. "1.0" остаётся Float)
//	string       → Str
//	array        → List
//	object       → Map (специальный случай {"$bytes": "..."} → Bytes;
//	               ключи "$$…" снимают одно экранирование)
package runtime

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

const jsonMaxDepth = 64

// JSONError — ошибка Json.encode/Json.decode. Reason кладётся в
// `(:json, reason)`: у encode это `(:unsupported, v)` или `:depth_limit`, у
// decode — `:syntax`, `(:number, s)`, `:depth_limit`, `:trailing_data`,
// `:invalid_utf8`.
type JSONError struct{ Reason Value }

func (e *JSONError) Error() string { return "(:json, " + e.Reason.Inspect() + ")" }

func jsonErr(reason Value) error { return &JSONError{Reason: reason} }

func jsonUnsupported(v Value) error {
	return jsonErr(Tuple(Atom("unsupported"), v))
}

// JSONOptions — опции Json.encode (§4.7).
type JSONOptions struct {
	// TypeTag — номинальные записи получают ключ "__type__" с именем типа.
	TypeTag bool
}

// JSONEncode — сериализует значение в строку JSON.
func JSONEncode(v Value) (string, error) {
	return JSONEncodeOpts(v, JSONOptions{})
}

// JSONEncodeOpts — JSONEncode с опциями.
func JSONEncodeOpts(v Value, opts JSONOptions) (string, error) {
	var sb strings.Builder
	if err := jsonEncode(&sb, opts, v, 0); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func jsonEncode(sb *strings.Builder, opts JSONOptions, v Value, depth int) error {
	if depth > jsonMaxDepth {
		return jsonErr(Atom("depth_limit"))
	}
	switch v.Kind {
	case KindUnit:
		sb.WriteString("null")

	case KindBool:
		if v.Bool {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}

	case KindInt:
		sb.WriteString(v.Inspect())

	case KindFloat:
		s, ok := formatJSONFloat(v.Float)
		if !ok {
			return jsonUnsupported(v)
		}
		sb.WriteString(s)

	case KindStr:
		b, _ := json.Marshal(v.Str)
		sb.Write(b)

	case KindAtom:
		b, _ := json.Marshal(":" + v.Atom)
		sb.Write(b)

	case KindDecimal:
		b, _ := json.Marshal(`dec"` + FormatDecimal(v.Dec) + `"`)
		sb.Write(b)

	case KindBytes:
		encoded := base64.StdEncoding.EncodeToString(v.Bytes)
		sb.WriteString(`{"$bytes":`)
		b, _ := json.Marshal(encoded)
		sb.Write(b)
		sb.WriteByte('}')

	case KindList, KindVector:
		// Обход без копии элементов (T-271, T-273).
		sb.WriteByte('[')
		first, encErr := true, error(nil)
		for e := range v.Items() {
			if !first {
				sb.WriteByte(',')
			}
			first = false
			if encErr = jsonEncode(sb, opts, e, depth+1); encErr != nil {
				break
			}
		}
		if encErr != nil {
			return encErr
		}
		sb.WriteByte(']')

	case KindTuple:
		sb.WriteByte('[')
		for i, e := range v.Tuple {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := jsonEncode(sb, opts, e, depth+1); err != nil {
				return err
			}
		}
		sb.WriteByte(']')

	case KindSet:
		sb.WriteByte('[')
		for i, e := range v.Elems() {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := jsonEncode(sb, opts, e, depth+1); err != nil {
				return err
			}
		}
		sb.WriteByte(']')

	case KindMap:
		sb.WriteByte('{')
		for i, e := range v.Entries() {
			if i > 0 {
				sb.WriteByte(',')
			}
			var ks string
			switch e.Key.Kind {
			case KindStr:
				ks = e.Key.Str
			case KindAtom:
				ks = e.Key.Atom
			default:
				return jsonUnsupported(e.Key)
			}
			ks = jsonEscapeKey(ks)
			b, _ := json.Marshal(ks)
			sb.Write(b)
			sb.WriteByte(':')
			if err := jsonEncode(sb, opts, e.Val, depth+1); err != nil {
				return err
			}
		}
		sb.WriteByte('}')

	case KindVariant:
		if v.Variant == nil {
			return jsonUnsupported(v)
		}
		switch v.Variant.Tag {
		case "None":
			sb.WriteString("null")
		case "Some":
			if len(v.Variant.Args) != 1 {
				return jsonUnsupported(v)
			}
			return jsonEncode(sb, opts, v.Variant.Args[0], depth+1)
		case "Ok", "Error":
			return jsonUnsupported(v)
		default:
			sb.WriteString(`{"tag":`)
			b, _ := json.Marshal(v.Variant.Tag)
			sb.Write(b)
			sb.WriteString(`,"args":[`)
			for i, a := range v.Variant.Args {
				if i > 0 {
					sb.WriteByte(',')
				}
				if err := jsonEncode(sb, opts, a, depth+1); err != nil {
					return err
				}
			}
			sb.WriteString(`]}`)
		}

	case KindRecord:
		sb.WriteByte('{')
		n := 0
		if opts.TypeTag && v.Record.Type != "" {
			sb.WriteString(`"__type__":`)
			b, _ := json.Marshal(v.Record.Type)
			sb.Write(b)
			n++
		}
		for _, f := range v.Record.Fields {
			if n > 0 {
				sb.WriteByte(',')
			}
			n++
			b, _ := json.Marshal(f.Name)
			sb.Write(b)
			sb.WriteByte(':')
			if err := jsonEncode(sb, opts, f.Val, depth+1); err != nil {
				return err
			}
		}
		sb.WriteByte('}')

	default:
		// Function, Closure, Pid, Ref, Port, Range.
		return jsonUnsupported(v)
	}
	return nil
}

// JSONDecode — парсит строку JSON в значение; ошибка — *JSONError.
func JSONDecode(s string) (Value, error) {
	if !utf8.ValidString(s) {
		return Unit, jsonErr(Atom("invalid_utf8"))
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		if strings.Contains(err.Error(), "exceeded max depth") {
			return Unit, jsonErr(Atom("depth_limit"))
		}
		return Unit, jsonErr(Atom("syntax"))
	}
	// Всё после первого значения, кроме пробелов, — хвост.
	if _, err := dec.Token(); err != io.EOF {
		return Unit, jsonErr(Atom("trailing_data"))
	}
	return fromJSON(raw, 0)
}

func fromJSON(raw any, depth int) (Value, error) {
	if depth > jsonMaxDepth {
		return Unit, jsonErr(Atom("depth_limit"))
	}
	switch x := raw.(type) {
	case nil:
		return Variant("None"), nil
	case bool:
		return Bool(x), nil
	case string:
		return Str(x), nil
	case json.Number:
		s := x.String()
		if !strings.ContainsAny(s, ".eE") {
			n, ok := new(big.Int).SetString(s, 10)
			if ok {
				return IntBig(n), nil
			}
		}
		f, err := x.Float64()
		if err != nil {
			return Unit, jsonErr(Tuple(Atom("number"), Str(s)))
		}
		return Float(f), nil
	case []any:
		elems := make([]Value, len(x))
		for i, e := range x {
			v, err := fromJSON(e, depth+1)
			if err != nil {
				return Unit, err
			}
			elems[i] = v
		}
		return List(elems...), nil
	case map[string]any:
		// Специальный случай {"$bytes": "..."}.
		if len(x) == 1 {
			if b, ok := x["$bytes"]; ok {
				if bs, ok := b.(string); ok {
					raw, err := base64.StdEncoding.DecodeString(bs)
					if err != nil {
						return Unit, jsonErr(Atom("syntax"))
					}
					return Bytes(raw), nil
				}
			}
		}
		entries := make([]MapEntry, 0, len(x))
		for k, val := range x {
			v, err := fromJSON(val, depth+1)
			if err != nil {
				return Unit, err
			}
			entries = append(entries, MapEntry{Key: Str(jsonUnescapeKey(k)), Val: v})
		}
		return Map(entries), nil
	}
	return Unit, jsonErr(Atom("syntax"))
}

// formatJSONFloat — JSON number для Float: Inf/NaN запрещены (RFC 8259);
// целочисленные значения пишутся с ".0", чтобы decode сохранил KindFloat.
func formatJSONFloat(f float64) (string, bool) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "", false
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s, true
}

// jsonEscapeKey — ключи Map с префиксом "$" получают ещё один "$",
// чтобы {"$bytes":…} от Bytes не путался с Map %{"$bytes"=>…}.
func jsonEscapeKey(k string) string {
	if strings.HasPrefix(k, "$") {
		return "$" + k
	}
	return k
}

func jsonUnescapeKey(k string) string {
	if strings.HasPrefix(k, "$$") {
		return k[1:]
	}
	return k
}
