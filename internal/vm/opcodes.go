package vm

// OpCode — опкод регистровой ВМ (Sprint 7, §1, §10).
//
// Инструкция — 4 байта (Instr); op занимает младший байт uint32.
// Полный набор — 54 опкода: удалены Pop/Dup/GetLocal/SetLocal/SetUpvalue
// стековой ВМ, добавлены MOVE и TAILCALL; записи (T-73) — RECORD и GETFIELD;
// спред коллекций (T-155) — LISTSPREAD, VECSPREAD, MAPSPREAD.
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

	SPAWN       // R[A] = spawn(R[B]); C&3 == 1 — linked; C&3 == 2 — watched, R[A] = (pid, ref); C&SpawnLimits — лимиты хода в R[B+1]
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

	CONCAT // R[A] = R[B] ++ R[C], оба Str; только для интерполяции (T-131)

	// Спред при конструировании (§5.2, T-155).
	// LISTSPREAD/VECSPREAD: C сегментов по 2 регистра от R[B]:
	// R[B+2i] — Bool (true = спред коллекции R[B+2i+1], false = один элемент).
	// Список принимает только List; вектор — List или Vector.
	// Иначе (:type_error, (:spread, v)).
	LISTSPREAD
	VECSPREAD
	// MAPSPREAD: C сегментов по 3 регистра от R[B]:
	// R[B+3i] — Bool; true = спред мапы R[B+3i+1] (R[B+3i+2] не используется),
	// false = пара ключ R[B+3i+1], значение R[B+3i+2].
	// Правые ключи перекрывают левые. Не Map — (:type_error, (:spread, v)).
	MAPSPREAD

	// exit и ensure (§12.7, T-163).
	EXIT       // R[A] = exit(pid=R[B], reason=R[C]); Ok(())
	TRAPENSURE // как TRAPBEGIN; handler ensure-блока: unwind от exit входит в него
	ENSEND     // конец ensure-блока: unwind от exit, вошедший в этот блок, продолжается

	// Реестр имён (§12.8, T-164).
	REGISTER   // R[A] = register(name=R[B], pid=R[C]); Result<(), Atom>
	UNREGISTER // unregister(R[B]); R[A] = ()
	WHEREIS    // R[A] = whereis(R[B]); Option<Pid>

	// Ответ по ref мимо ящика (§12.9, T-165).
	AWAIT // R[A] = await(ref=R[B], timeout=R[C]); Result<V, Atom>; ждёт — исполняется снова
	REPLY // reply(pid=R[B], ref=R[B+1], value=R[B+2]); R[A] = ()

	// Хвостовой вызов из trap с ensure (doc 02 §5.1, T-173): R[A] — callee,
	// R[A+1..A+B] — аргументы, R[A+B+1..A+B+C] — замыкания ensure (LIFO).
	// Снимает handler TRAPENSURE, кладёт запись в Frame.cleanups, дальше —
	// как TAILCALL.
	TAILCALLENS

	// Связь владелец → ребёнок (§12.2, T-252).
	LINK // link(R[B]): вызывающий становится владельцем R[B]; R[A] = ()
)

// SpawnLimits — бит операнда C у SPAWN: второй аргумент spawn (лимиты
// хода, §12.10) лежит в R[B+1]. Младшие биты (spawnModeMask) — режим.
const (
	SpawnLimits   = 4
	spawnModeMask = 3
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
	CONCAT:         "CONCAT",
	LISTSPREAD:     "LISTSPREAD",
	VECSPREAD:      "VECSPREAD",
	MAPSPREAD:      "MAPSPREAD",
	EXIT:           "EXIT",
	TRAPENSURE:     "TRAPENSURE",
	ENSEND:         "ENSEND",
	REGISTER:       "REGISTER",
	UNREGISTER:     "UNREGISTER",
	WHEREIS:        "WHEREIS",
	AWAIT:          "AWAIT",
	REPLY:          "REPLY",
	TAILCALLENS:    "TAILCALLENS",
	LINK:           "LINK",
}

func (op OpCode) String() string {
	if int(op) < len(opNames) {
		return opNames[op]
	}
	return "?"
}
