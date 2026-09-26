// Package runtime — JSON encode/decode (§4.7, Must из §16).
//
// Encode (Value → JSON):
//
//	Unit         → null
//	Bool         → true/false
//	Int          → number
//	Float        → number
//	Str          → string
//	Atom         → ":name" (строка с ведущим ':')
//	Decimal      → "dec\"...\"" (строка — точность важнее)
//	Bytes        → {"$bytes": "<base64>"} (маркер для round-trip)
//	List/Vector/Set/Tuple → array
//	Map          → object (ключи только Str/Atom; ключи с префиксом "$"
//	               экранируются как "$$…", чтобы не пересекаться с маркером)
//	None         → null
//	Some(x)/Ok(x)→ прозрачно, encode(x)
//	Error(e)     → {"error": encode(e)}
//	иной вариант → {"tag": "Name", "args": [...]}
//	Function/Closure/Pid/Ref → ошибка (§14.8)
//	Float Inf/NaN → ошибка (RFC 8259)
//
// Decode (JSON → Value):
//
//	null         → Unit
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
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

const jsonMaxDepth = 64

// JSONEncode — сериализует значение в строку JSON.
func JSONEncode(v Value) (string, error) {
	var sb strings.Builder
	if err := jsonEncode(&sb, v, 0); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func jsonEncode(sb *strings.Builder, v Value, depth int) error {
	if depth > jsonMaxDepth {
		return fmt.Errorf("(:json_encode, :depth_limit)")
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
		s, err := formatJSONFloat(v.Float)
		if err != nil {
			return err
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

	case KindList, KindVector, KindTuple:
		var elems []Value
		switch v.Kind {
		case KindList:
			elems = v.List
		case KindVector:
			elems = v.Vector
		case KindTuple:
			elems = v.Tuple
		}
		sb.WriteByte('[')
		for i, e := range elems {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := jsonEncode(sb, e, depth+1); err != nil {
				return err
			}
		}
		sb.WriteByte(']')

	case KindSet:
		sb.WriteByte('[')
		for i, e := range v.Set {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := jsonEncode(sb, e, depth+1); err != nil {
				return err
			}
		}
		sb.WriteByte(']')

	case KindMap:
		sb.WriteByte('{')
		for i, e := range v.Map {
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
				return fmt.Errorf("(:json_encode, (:invalid_key, %s))", e.Key.Inspect())
			}
			ks = jsonEscapeKey(ks)
			b, _ := json.Marshal(ks)
			sb.Write(b)
			sb.WriteByte(':')
			if err := jsonEncode(sb, e.Val, depth+1); err != nil {
				return err
			}
		}
		sb.WriteByte('}')

	case KindVariant:
		if v.Variant == nil {
			return fmt.Errorf("(:json_encode, :nil_variant)")
		}
		switch v.Variant.Tag {
		case "None":
			sb.WriteString("null")
		case "Some", "Ok":
			if len(v.Variant.Args) != 1 {
				return fmt.Errorf("(:json_encode, (:bad_variant, %s))", v.Inspect())
			}
			return jsonEncode(sb, v.Variant.Args[0], depth+1)
		case "Error":
			if len(v.Variant.Args) != 1 {
				return fmt.Errorf("(:json_encode, (:bad_variant, %s))", v.Inspect())
			}
			sb.WriteString(`{"error":`)
			if err := jsonEncode(sb, v.Variant.Args[0], depth+1); err != nil {
				return err
			}
			sb.WriteByte('}')
		default:
			sb.WriteString(`{"tag":`)
			b, _ := json.Marshal(v.Variant.Tag)
			sb.Write(b)
			sb.WriteString(`,"args":[`)
			for i, a := range v.Variant.Args {
				if i > 0 {
					sb.WriteByte(',')
				}
				if err := jsonEncode(sb, a, depth+1); err != nil {
					return err
				}
			}
			sb.WriteString(`]}`)
		}

	case KindFunction, KindClosure, KindPid, KindRef:
		return fmt.Errorf("(:json_encode, (:not_serializable, %s))", v.Kind)

	default:
		return fmt.Errorf("(:json_encode, (:unknown_kind, %s))", v.Kind)
	}
	return nil
}

// JSONDecode — парсит строку JSON в значение.
func JSONDecode(s string) (Value, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return Unit, fmt.Errorf("(:json_decode, %q)", err.Error())
	}
	// Проверка на хвостовые данные после первого значения.
	var tail any
	if err := dec.Decode(&tail); err == nil {
		return Unit, fmt.Errorf("(:json_decode, :trailing_data)")
	}
	return fromJSON(raw, 0)
}

func fromJSON(raw any, depth int) (Value, error) {
	if depth > jsonMaxDepth {
		return Unit, fmt.Errorf("(:json_decode, :depth_limit)")
	}
	switch x := raw.(type) {
	case nil:
		return Unit, nil
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
			return Unit, fmt.Errorf("(:json_decode, (:number, %q))", s)
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
						return Unit, fmt.Errorf("(:json_decode, (:bytes, %q))", err.Error())
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
	return Unit, fmt.Errorf("(:json_decode, :unexpected_type)")
}

// formatJSONFloat — JSON number для Float: Inf/NaN запрещены (RFC 8259);
// целочисленные значения пишутся с ".0", чтобы decode сохранил KindFloat.
func formatJSONFloat(f float64) (string, error) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "", fmt.Errorf("(:json_encode, :non_finite)")
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s, nil
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
