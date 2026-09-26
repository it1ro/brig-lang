package vm

// OpCode — опкод регистровой ВМ (Sprint 7, §1, §10).
//
// Инструкция — 4 байта (Instr); op занимает младший байт uint32.
// Полный набор — 51 опкод: удалены Pop/Dup/GetLocal/SetLocal/SetUpvalue
// стековой ВМ, добавлены MOVE и TAILCALL; записи (T-73) — RECORD и GETFIELD.
type OpCode byte

// Опкоды регистровой ВМ. LOADK — R[A] = K[Bx].
const (
	LOADK OpCode = iota
	MOVE         // R[A] = R[B]

	GETGLOBAL // R[A] = G[K[Bx].Str]
	SETGLOBAL // G[K[Bx].Str] = R[A]
	GETUPVAL  // R[A] = captures[B]

	ADD    // R[A] = R[B] + R[C]
	SUB    // R[A] = R[B] - R[C]
	MUL    // R[A] = R[B] * R[C]
	DIV    // R[A] = R[B] / R[C]
	INTDIV // R[A] = R[B] div R[C]
	REM    // R[A] = R[B] rem R[C]
	POW    // R[A] = R[B] ** R[C]

	EQ // R[A] = Bool(R[B] == R[C])
	NEQ
	LT
	GT
	LE
	GE

	NEG // R[A] = -R[B]
	NOT // R[A] = not R[B]

	JMP      // ip += 1 + sBx
	JMPIFNOT // if R[A] == Bool(false) { ip += 1 + sBx }; R[A] не Bool → raise (:type_error, (:expected_bool, v))
	JMPIF    // if R[A] == Bool(true)  { ip += 1 + sBx }; R[A] не Bool → raise (:type_error, (:expected_bool, v))

	CALL     // R[C] = R[A](R[A+1..A+B])
	TAILCALL // замена кадра: R[A](R[A+1..A+B])
	RETURN   // return R[A]

	TUPLE  // R[A] = (R[B..B+C-1])
	LIST   // R[A] = [R[B..B+C-1]]
	VECTOR // R[A] = %[R[B..B+C-1]]
	MAP    // R[A] = %{ R[B..B+2C-1] } (C пар k,v)
	RANGE  // R[A] = R[B] to R[C]
	INDEX  // R[A] = R[B][R[C]]

	RAISE // raise R[A]

	MAKECLOSURE // R[A] = Closure(R[B], R[B+1..B+C])

	TRAPBEGIN // handler = {ip: ip+1+sBx, errReg: A}
	TRAPEND   // снять верхний handler
	MAKEOK    // R[A] = Ok(R[B])
	MAKEERROR // R[A] = Error(R[B])

	SPAWN       // R[A] = spawn(R[B]); C == 1 — linked
	SEND        // R[A] = send(pid=R[B], msg=R[C])
	SELF        // R[A] = Pid(a.pid)
	MAKEREF     // R[A] = Ref(next)
	WATCH       // R[A] = Ref наблюдения за R[B]
	UNWATCH     // снять наблюдение R[B]; R[A] = ()
	MAILBOXSIZE // R[A] = len(mailbox(R[B]))
	RECVTIMER   // дедлайн = now + R[A] ms
	RECVTAKE    // R[A] = сообщение; sBx → after (0 — after отсутствует)
	MATCHLOCAL  // if MatchPattern(R[A], Patterns[Bx], regs) { ip += 2 } else { ip += 1 }
	YIELD       // отдать квант

	// RECORD: R[A] = запись по форме R[B] и значениям R[B+1..B+C] (§4.7).
	// Форма — константа (Str тип, Tuple объявленных полей, Tuple слотов);
	// слот — имя поля или ".." (спред записи). Тип "" — анонимная.
	RECORD
	GETFIELD // R[A] = R[B].field, имя поля — Str в R[C]

	// Вызов со спредом (§6.3): как CALL/TAILCALL, но последний аргумент
	// R[A+B] — List, чьи элементы разворачиваются в аргументы.
	CALLSPREAD     // R[C] = R[A](R[A+1..A+B-1], ..R[A+B])
	TAILCALLSPREAD // замена кадра: R[A](R[A+1..A+B-1], ..R[A+B])
)

// opNames индексируется OpCode; размер массива фиксирован числом опкодов.
var opNames = [...]string{
	LOADK:       "LOADK",
	MOVE:        "MOVE",
	GETGLOBAL:   "GETGLOBAL",
	SETGLOBAL:   "SETGLOBAL",
	GETUPVAL:    "GETUPVAL",
	ADD:         "ADD",
	SUB:         "SUB",
	MUL:         "MUL",
	DIV:         "DIV",
	INTDIV:      "INTDIV",
	REM:         "REM",
	POW:         "POW",
	EQ:          "EQ",
	NEQ:         "NEQ",
	LT:          "LT",
	GT:          "GT",
	LE:          "LE",
	GE:          "GE",
	NEG:         "NEG",
	NOT:         "NOT",
	JMP:         "JMP",
	JMPIFNOT:    "JMPIFNOT",
	JMPIF:       "JMPIF",
	CALL:        "CALL",
	TAILCALL:    "TAILCALL",
	RETURN:      "RETURN",
	TUPLE:       "TUPLE",
	LIST:        "LIST",
	VECTOR:      "VECTOR",
	MAP:         "MAP",
	RANGE:       "RANGE",
	INDEX:       "INDEX",
	RAISE:       "RAISE",
	MAKECLOSURE: "MAKECLOSURE",
	TRAPBEGIN:   "TRAPBEGIN",
	TRAPEND:     "TRAPEND",
	MAKEOK:      "MAKEOK",
	MAKEERROR:   "MAKEERROR",
	SPAWN:       "SPAWN",
	SEND:        "SEND",
	SELF:        "SELF",
	MAKEREF:     "MAKEREF",
	WATCH:       "WATCH",
	UNWATCH:     "UNWATCH",
	MAILBOXSIZE: "MAILBOXSIZE",
	RECVTIMER:   "RECVTIMER",
	RECVTAKE:    "RECVTAKE",
	MATCHLOCAL:  "MATCHLOCAL",
	YIELD:       "YIELD",
	RECORD:      "RECORD",
	GETFIELD:    "GETFIELD",

	CALLSPREAD:     "CALLSPREAD",
	TAILCALLSPREAD: "TAILCALLSPREAD",
}

func (op OpCode) String() string {
	if int(op) < len(opNames) {
		return opNames[op]
	}
	return "?"
}
