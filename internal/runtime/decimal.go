package runtime

import (
	"fmt"
	"math/big"
	"strings"
)

// ParseDecimal парсит тело литерала dec"..." (без префикса dec" и
// закрывающей кавычки). Тело провалидировано лексером (A4.4):
// [+-]? digits [. digits], подчёркивания только между цифрами.
//
// Решение по trailing zeros: dec"1.50" и dec"1.5" эквивалентны —
// trailing zeros не хранятся. Обоснование: §3.1 требует «точную
// десятичную арифметику», §4.8 — «Decimal — точно» (равенство по
// значению); точное представление вроде "1.50" как отдельная сущность
// нигде не оговорено. Нормализация через big.Rat даёт минимум кода,
// точное сравнение 1.50 == 1.5 и корректную арифметику без
// масштабирования.
func ParseDecimal(body string) (*big.Rat, error) {
	cleaned := strings.ReplaceAll(body, "_", "")
	r, ok := new(big.Rat).SetString(cleaned)
	if !ok {
		return nil, fmt.Errorf("invalid decimal literal dec\"%s\"", body)
	}
	return r, nil
}

// FormatDecimal — каноническая запись без префикса dec"..." и кавычек.
// Для терминирующих дробей — минимальная десятичная форма без trailing
// zeros (3.00 → "3", 1.50 → "1.5"). Для нетерминирующих (1/3, 22/7)
// — форма "num/denom": такие значения получаются только арифметикой,
// не литералами, и не парсятся обратно как dec"..." — это осознанно,
// в MVP-грамматике нет литерального представления для них.
func FormatDecimal(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	for prec := 1; prec <= 34; prec++ {
		s := r.FloatString(prec)
		if check, ok := new(big.Rat).SetString(s); ok && check.Cmp(r) == 0 {
			s = strings.TrimRight(s, "0")
			s = strings.TrimRight(s, ".")
			return s
		}
	}
	return r.Num().String() + "/" + r.Denom().String()
}
