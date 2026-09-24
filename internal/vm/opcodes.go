// Package vm — стековая байткод-машина Брига.
//
// Этап 4.2/4.3: вертикальный срез. ОСОЗНАННОЕ отступление от §15.1:
// первая машина стековая, не регистровая — ради быстрого получения
// исполняемого пайплайна и отладки семантики. Миграция на регистровую
// (с дизассемблером --dump-bytecode) — отдельный подэтап.
package vm

// OpCode — инструкция стековой ВМ.
type OpCode byte

const (
	OpConstant OpCode = iota // push constants[op]
	OpPop                    // discard top
	OpDup                    // duplicate top
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpIntDiv
	OpRem
	OpPow
	OpNeg
	OpNot
	OpEq
	OpNeq
	OpLt
	OpGt
	OpLe
	OpGe
	OpJump      // безусловный переход
	OpJumpFalse // pop; переход если ложь
	OpJumpTrue  // pop; переход если истина
	OpGetLocal
	OpSetLocal
	OpGetGlobal
	OpSetGlobal
	OpCall   // вызов значения-функции
	OpReturn // возврат из функции
	OpTuple  // собрать n значений в кортеж
	OpList   // собрать n значений в список
	OpVector // собрать n значений в вектор
	OpMap    // собрать n пар (2n значений) в мапу
	OpRaise  // raise(top)

	// Трек α (замыкания, локальные функции) — заглушки.
	OpMakeClosure
	OpGetUpvalue
	OpSetUpvalue
	OpCloseUpvalue
	OpDefineLocalFn
)

func (op OpCode) String() string {
	names := map[OpCode]string{
		OpConstant: "CONSTANT", OpPop: "POP", OpDup: "DUP",
		OpAdd: "ADD", OpSub: "SUB", OpMul: "MUL", OpDiv: "DIV",
		OpIntDiv: "INTDIV", OpRem: "REM", OpPow: "POW",
		OpNeg: "NEG", OpNot: "NOT",
		OpEq: "EQ", OpNeq: "NEQ", OpLt: "LT", OpGt: "GT",
		OpLe: "LE", OpGe: "GE",
		OpJump: "JMP", OpJumpFalse: "JMPFALSE", OpJumpTrue: "JMPTRUE",
		OpGetLocal: "GETLOCAL", OpSetLocal: "SETLOCAL",
		OpGetGlobal: "GETGLOBAL", OpSetGlobal: "SETGLOBAL",
		OpCall: "CALL", OpReturn: "RETURN",
		OpTuple: "TUPLE", OpList: "LIST", OpVector: "VECTOR",
		OpMap: "MAP", OpRaise: "RAISE",
		OpMakeClosure: "MAKECLOSURE", OpGetUpvalue: "GETUPVAL",
		OpSetUpvalue: "SETUPVAL", OpCloseUpvalue: "CLOSEUPVAL",
		OpDefineLocalFn: "DEFLOCALFN",
	}
	if n, ok := names[op]; ok {
		return n
	}
	return "?"
}
