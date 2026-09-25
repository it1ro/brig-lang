package vm

// OpCode — инструкция стековой ВМ.
//
// Sprint 6.2: удалены мёртвые OpCloseUpvalue / OpDefineLocalFn —
// компилятор их не эмитит, VM падала на них как "not implemented".
// Если понадобятся для upvalue-by-ref в будущем, вводятся заново
// вместе с реализацией в stepFrame.
type OpCode byte

const (
	// OpConstant — загрузить константу.
	OpConstant OpCode = iota
	OpPop
	OpDup
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
	OpJump
	OpJumpFalse
	OpJumpTrue
	OpGetLocal
	OpSetLocal
	OpGetGlobal
	OpSetGlobal
	OpCall
	OpReturn
	OpTuple
	OpList
	OpVector
	OpMap
	OpRaise

	// OpMakeClosure — создать замыкание.
	OpMakeClosure
	OpGetUpvalue
	OpSetUpvalue

	// OpTrapBegin — начать блок trap.
	OpTrapBegin
	OpTrapEnd
	OpMakeOk
	OpMakeError

	// v0.4.8: акторы (§12).
	OpMakeRef
	OpWatch
	OpUnwatch
	OpMailboxSize
	OpRecvTimer
	OpRecvTake
	OpMatchLocal
	OpYield
	OpSpawn
	OpSend
	OpSelf

	// v0.4.9 (Sprint 5.1–5.3): Range, Index.
	OpRange
	OpIndex
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
		OpSetUpvalue:  "SETUPVAL",
		OpTrapBegin:   "TRAPBEGIN",
		OpTrapEnd:     "TRAPEND",
		OpMakeOk:      "MAKEOK",
		OpMakeError:   "MAKEERROR",
		OpSpawn:       "SPAWN",
		OpSend:        "SEND",
		OpSelf:        "SELF",
		OpMakeRef:     "MAKEREF",
		OpWatch:       "WATCH",
		OpUnwatch:     "UNWATCH",
		OpMailboxSize: "MAILBOXSIZE",
		OpRecvTimer:   "RECVTIMER",
		OpRecvTake:    "RECVTAKE",
		OpMatchLocal:  "MATCHLOCAL",
		OpYield:       "YIELD",
		OpRange:       "RANGE",
		OpIndex:       "INDEX",
	}
	if n, ok := names[op]; ok {
		return n
	}
	return "?"
}
