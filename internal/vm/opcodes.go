package vm

// OpCode — инструкция стековой ВМ.
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
	OpCloseUpvalue
	OpDefineLocalFn

	// OpTrapBegin — начать блок trap.
	OpTrapBegin
	OpTrapEnd
	OpMakeOk
	OpMakeError

	// v0.4.8 (подэтап 4.8): акторы (§12).
	//
	//	OpSpawn <linked>            — pop fn; spawn; push pid
	//	OpSend                      — pop msg; pop pid; send; push Result<(),Atom>
	//	OpSelf                      — push self pid
	// OpMakeRef — создать новую ссылку (ref).
	OpMakeRef // push fresh ref
	OpWatch   // pop pid; watch; push ref
	OpUnwatch // pop ref; unwatch; push ()
	OpMailboxSize
	OpRecvTimer // pop ms; set recv deadline
	OpRecvTake  // <slot> <afterAddr>; take msg into slot, or block/after
	OpMatchLocal
	OpYield
	OpSpawn
	OpSend
	OpSelf
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
		OpTrapBegin:     "TRAPBEGIN",
		OpTrapEnd:       "TRAPEND",
		OpMakeOk:        "MAKEOK",
		OpMakeError:     "MAKEERROR",
		OpSpawn:         "SPAWN",
		OpSend:          "SEND",
		OpSelf:          "SELF",
		OpMakeRef:       "MAKEREF",
		OpWatch:         "WATCH",
		OpUnwatch:       "UNWATCH",
		OpMailboxSize:   "MAILBOXSIZE",
		OpRecvTimer:     "RECVTIMER",
		OpRecvTake:      "RECVTAKE",
		OpMatchLocal:    "MATCHLOCAL",
		OpYield:         "YIELD",
	}
	if n, ok := names[op]; ok {
		return n
	}
	return "?"
}
