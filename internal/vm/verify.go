package vm

import "fmt"

// Verify — линейный верификатор чанка (§12).
//
// Проверки:
//  1. все регистры и окна < NumRegs;
//  2. цели переходов внутри кода;
//  3. за каждым MATCHLOCAL идёт JMP;
//  4. TAILCALL вне активных trap-регионов;
//  5. нет «падения с конца» кода;
//  6. definite assignment (регистр определён на всех путях).
//
// Включается флагом compiler.Verify (в тестах) или BRIG_VERIFY=1.
// Не является частью fast-path VM; вызывается только из тестов и
// по явному запросу.
func Verify(c *Chunk) error {
	if c == nil {
		return fmt.Errorf("verify: nil chunk")
	}
	if err := verifyRegsAndJumps(c); err != nil {
		return err
	}
	if err := verifyMatchLocal(c); err != nil {
		return err
	}
	if err := verifyTailCall(c); err != nil {
		return err
	}
	if err := verifyFallThrough(c); err != nil {
		return err
	}
	return verifyDefiniteAssignment(c)
}

// verifyRegsAndJumps: регистры и цели переходов внутри кода.
func verifyRegsAndJumps(c *Chunk) error {
	n := len(c.Code)
	for ip, in := range c.Code {
		switch in.Op() {
		case JMP, JMPIFNOT, JMPIF, TRAPBEGIN:
			target := ip + 1 + in.SBx()
			if target < 0 || target >= n {
				return fmt.Errorf(
					"verify: %s at %d: jump target %d out of range [0,%d)",
					in.Op(), ip, target, n)
			}
		case RECVTAKE:
			if in.SBx() != 0 {
				target := ip + 1 + in.SBx()
				if target < 0 || target >= n {
					return fmt.Errorf(
						"verify: RECVTAKE at %d: after target %d out of range",
						ip, target)
				}
			}
		}
		reads, writes, err := RegUse(in)
		if err != nil {
			return fmt.Errorf("verify: at %d: %w", ip, err)
		}
		for _, r := range reads {
			if r < 0 || r >= c.NumRegs {
				return fmt.Errorf(
					"verify: %s at %d: read reg %d >= NumRegs %d",
					in.Op(), ip, r, c.NumRegs)
			}
		}
		for _, r := range writes {
			if r < 0 || r >= c.NumRegs {
				return fmt.Errorf(
					"verify: %s at %d: write reg %d >= NumRegs %d",
					in.Op(), ip, r, c.NumRegs)
			}
		}
	}
	return nil
}

// RegUse возвращает читаемые/записываемые регистры инструкции.
// Общая таблица чтения/записи для verify и compiler.emit (I-4).
func RegUse(in Instr) (reads, writes []int, err error) {
	a, b, cc := in.A(), in.B(), in.C()
	switch in.Op() {
	case LOADK, GETGLOBAL, GETUPVAL, SELF, MAKEREF, RECVTIMER, RECVTAKE:
		return nil, []int{a}, nil
	case MOVE, NEG, NOT, MAKEOK, MAKEERROR, WATCH, UNWATCH, MAILBOXSIZE:
		return []int{b}, []int{a}, nil
	case ADD, SUB, MUL, DIV, INTDIV, REM, POW,
		EQ, NEQ, LT, GT, LE, GE, RANGE, INDEX:
		return []int{b, cc}, []int{a}, nil
	case SETGLOBAL, RETURN, RAISE:
		return []int{a}, nil, nil
	case JMPIFNOT, JMPIF:
		return []int{a}, nil, nil
	case JMP, TRAPEND, YIELD:
		return nil, nil, nil
	case TRAPBEGIN:
		// errReg пишется неявно при raise; формально регистр определён
		// только на пути обработчика, поэтому в reads/writes не входит.
		return nil, nil, nil
	case CALL:
		// R[A] — callee; R[A+1..A+B] — аргументы; результат — в R[C].
		reads = []int{a}
		for i := 1; i <= b; i++ {
			reads = append(reads, a+i)
		}
		return reads, []int{cc}, nil
	case TAILCALL:
		reads = []int{a}
		for i := 1; i <= b; i++ {
			reads = append(reads, a+i)
		}
		return reads, nil, nil
	case TUPLE, LIST, VECTOR:
		reads = nil
		for i := 0; i < cc; i++ {
			reads = append(reads, b+i)
		}
		return reads, []int{a}, nil
	case MAP:
		reads = nil
		for i := 0; i < 2*cc; i++ {
			reads = append(reads, b+i)
		}
		return reads, []int{a}, nil
	case MAKECLOSURE:
		// R[B] — функция; R[B+1..B+C] — захваты.
		reads = nil
		for i := 0; i <= cc; i++ {
			reads = append(reads, b+i)
		}
		return reads, []int{a}, nil
	case SPAWN:
		return []int{b}, []int{a}, nil
	case SEND:
		return []int{b, cc}, []int{a}, nil
	case MATCHLOCAL:
		// R[A] читается; регистры паттерна пишутся неявно (не в A/B/C).
		return []int{a}, nil, nil
	}
	return nil, nil, fmt.Errorf("unknown opcode %d", in.Op())
}

// verifyMatchLocal: за каждым MATCHLOCAL обязана идти JMP.
func verifyMatchLocal(c *Chunk) error {
	for ip, in := range c.Code {
		if in.Op() != MATCHLOCAL {
			continue
		}
		if ip+1 >= len(c.Code) {
			return fmt.Errorf("verify: MATCHLOCAL at %d: no following JMP", ip)
		}
		if c.Code[ip+1].Op() != JMP {
			return fmt.Errorf(
				"verify: MATCHLOCAL at %d: next op is %s, want JMP",
				ip, c.Code[ip+1].Op())
		}
	}
	return nil
}

// verifyTailCall: линейный счётчик глубины trap-регионов.
// TRAPBEGIN +1, TRAPEND -1; TAILCALL при глубине > 0 — ошибка.
func verifyTailCall(c *Chunk) error {
	depth := 0
	for ip, in := range c.Code {
		switch in.Op() {
		case TRAPBEGIN:
			depth++
		case TRAPEND:
			depth--
			if depth < 0 {
				return fmt.Errorf("verify: TRAPEND at %d underflow", ip)
			}
		case TAILCALL:
			if depth > 0 {
				return fmt.Errorf(
					"verify: TAILCALL at %d under active trap (depth=%d)",
					ip, depth)
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("verify: unbalanced trap regions: depth=%d at EOF", depth)
	}
	return nil
}

// verifyFallThrough: последняя инструкция должна быть завершающей —
// RETURN, JMP, TAILCALL, RAISE.
func verifyFallThrough(c *Chunk) error {
	if len(c.Code) == 0 {
		return fmt.Errorf("verify: empty code")
	}
	last := c.Code[len(c.Code)-1].Op()
	switch last {
	case RETURN, JMP, TAILCALL, RAISE:
		return nil
	}
	return fmt.Errorf(
		"verify: code falls off end: last op is %s, want RETURN/JMP/TAILCALL/RAISE",
		last)
}

// verifyDefiniteAssignment: прямой анализ «регистр определён на всех путях».
func verifyDefiniteAssignment(c *Chunk) error {
	n := len(c.Code)
	if n == 0 {
		return nil
	}

	initial := make([]bool, c.NumRegs)
	for i := 0; i < c.NumParams && i < c.NumRegs; i++ {
		initial[i] = true
	}

	in := make([][]bool, n)
	in[0] = initial

	changed := true
	for changed {
		changed = false
		for ip := 0; ip < n; ip++ {
			if in[ip] == nil {
				continue
			}
			ins := c.Code[ip]
			out := cloneBoolSlice(in[ip])
			applyWrites(ins, out)

			propagate := func(succ int, state []bool) {
				if succ < 0 || succ >= n {
					return
				}
				if in[succ] == nil {
					in[succ] = state
					changed = true
					return
				}
				merged := intersectBoolSlices(in[succ], state)
				if !equalBoolSlices(merged, in[succ]) {
					in[succ] = merged
					changed = true
				}
			}

			// MATCHLOCAL: fail → ip+1 (без слотов паттерна), success → ip+2
			// (слоты Patterns[Bx]). Не гоняем generic successors, иначе
			// оба ребра получат одинаковый out без слотов.
			if ins.Op() == MATCHLOCAL {
				propagate(ip+1, out)
				successOut := cloneBoolSlice(out)
				bx := ins.Bx()
				if bx < 0 || bx >= len(c.Patterns) {
					return fmt.Errorf(
						"verify: MATCHLOCAL at %d: pattern %d out of range",
						ip, bx)
				}
				for _, slot := range c.Patterns[bx].Slots() {
					if slot >= 0 && slot < len(successOut) {
						successOut[slot] = true
					}
				}
				propagate(ip+2, successOut)
			} else {
				for _, succ := range successors(c.Code, ip) {
					propagate(succ, out)
				}
			}

			if ins.Op() == TRAPBEGIN {
				handlerOut := cloneBoolSlice(out)
				errReg := ins.A()
				if errReg >= 0 && errReg < len(handlerOut) {
					handlerOut[errReg] = true
				}
				propagate(ip+1+ins.SBx(), handlerOut)
			}
		}
	}

	for ip, ins := range c.Code {
		if in[ip] == nil {
			continue
		}
		reads, _, err := RegUse(ins)
		if err != nil {
			continue
		}
		for _, r := range reads {
			if r < 0 || r >= c.NumRegs {
				continue
			}
			if !in[ip][r] {
				return fmt.Errorf(
					"verify: %s at %d reads undefined register r%d",
					ins.Op(), ip, r)
			}
		}
	}
	return nil
}

func applyWrites(in Instr, defined []bool) {
	_, writes, err := RegUse(in)
	if err != nil {
		return
	}
	for _, r := range writes {
		if r >= 0 && r < len(defined) {
			defined[r] = true
		}
	}
	switch in.Op() {
	case MATCHLOCAL:
		if in.A() >= 0 && in.A() < len(defined) {
			defined[in.A()] = true
		}
	}
}

// successors возвращает индексы инструкций-преемников.
func successors(code []Instr, ip int) []int {
	in := code[ip]
	switch in.Op() {
	case JMP:
		return []int{ip + 1 + in.SBx()}
	case JMPIFNOT, JMPIF:
		return []int{ip + 1, ip + 1 + in.SBx()}
	case MATCHLOCAL:
		// fail → JMP at ip+1; success → body at ip+2 (skip JMP).
		return []int{ip + 1, ip + 2}
	case RECVTAKE:
		if in.SBx() != 0 {
			// message → ip+1 (writes A); after/timeout → ip+1+sBx (no write).
			return []int{ip + 1, ip + 1 + in.SBx()}
		}
		return []int{ip + 1} // block, no after
	case RETURN, TAILCALL, RAISE:
		return nil
	default:
		return []int{ip + 1}
	}
}

func cloneBoolSlice(s []bool) []bool {
	out := make([]bool, len(s))
	copy(out, s)
	return out
}

func intersectBoolSlices(a, b []bool) []bool {
	out := make([]bool, len(a))
	for i := range a {
		out[i] = a[i] && b[i]
	}
	return out
}

func equalBoolSlices(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
