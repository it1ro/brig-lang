package vm

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// MaxRegs — лимит регистров на функцию (§1). 8 бит на поле A/B/C.
const MaxRegs = 256

// Instr — 4-байтная инструкция регистровой ВМ (§1).
//
// Раскладка (старшие биты слева):
//
//	 31      24 23      16 15       8 7        0
//	+----------+----------+----------+----------+
//	|    C     |    B     |    A     |    op    |   ABC
//	+----------+----------+----------+----------+
//	|      Bx / sBx       |    A     |    op    |   ABx / AsBx
//	+---------------------+----------+----------+
//
// sBx знаковый; target = ip + 1 + sBx.
type Instr uint32

// Op — опкод (младшие 8 бит).
func (i Instr) Op() OpCode { return OpCode(i) }

// A — 8-битное поле A.
func (i Instr) A() int { return int(i >> 8 & 0xFF) }

// B — 8-битное поле B.
func (i Instr) B() int { return int(i >> 16 & 0xFF) }

// C — 8-битное поле C.
func (i Instr) C() int { return int(i >> 24) }

// Bx — 16-битное беззнаковое поле (A/B).
func (i Instr) Bx() int { return int(i >> 16) }

// SBx — 16-битное знаковое поле (A/B).
func (i Instr) SBx() int { return int(int16(i >> 16)) }

// ABC собирает инструкцию формата ABC.
func ABC(op OpCode, a, b, c int) Instr {
	return Instr(op) | Instr(a)<<8 | Instr(b)<<16 | Instr(c)<<24
}

// ABx собирает инструкцию формата ABx.
func ABx(op OpCode, a, bx int) Instr {
	return Instr(op) | Instr(a)<<8 | Instr(bx)<<16
}

// AsBx собирает инструкцию формата AsBx.
func AsBx(op OpCode, a, sbx int) Instr {
	return Instr(op) | Instr(a)<<8 | Instr(uint16(int16(sbx)))<<16
}

// SrcPos — позиция в исходнике (Sprint 7, §9).
type SrcPos struct {
	Line, Col int32
}

// Chunk — код, константы и паттерны одной функции (§2, §8).
//
// Размер regs для кадра — NumRegs (high-water mark аллокатора).
// Constants адресуются Bx (16 бит, до 65 536). Patterns — Bx.
type Chunk struct {
	Code      []Instr
	Constants []runtime.Value
	Patterns  []*CompiledPattern
	Pos       []SrcPos
	NumRegs   int
	NumParams int
	Variadic  bool
}

// NewChunk создаёт пустой чанк.
func NewChunk() *Chunk { return &Chunk{} }

// Emit добавляет инструкцию с позицией; возвращает её индекс.
func (c *Chunk) Emit(i Instr, pos SrcPos) int {
	c.Code = append(c.Code, i)
	c.Pos = append(c.Pos, pos)
	return len(c.Code) - 1
}

// PatchJump пишет sBx = target-(at+1) в инструкцию at, сохраняя A/op.
// Форма знаковая; target = at + 1 + sBx.
func (c *Chunk) PatchJump(at, target int) error {
	if at < 0 || at >= len(c.Code) {
		return fmt.Errorf("patch: at %d out of range", at)
	}
	sbx := target - (at + 1)
	if sbx < -32768 || sbx > 32767 {
		return fmt.Errorf("patch: sBx %d out of int16 range", sbx)
	}
	old := c.Code[at]
	c.Code[at] = AsBx(old.Op(), old.A(), sbx)
	return nil
}

// AddConstant добавляет константу; возвращает её индекс.
func (c *Chunk) AddConstant(v runtime.Value) int {
	c.Constants = append(c.Constants, v)
	return len(c.Constants) - 1
}

// AddPattern добавляет паттерн; возвращает его индекс.
func (c *Chunk) AddPattern(p *CompiledPattern) int {
	c.Patterns = append(c.Patterns, p)
	return len(c.Patterns) - 1
}

// LineAt — строка источника для инструкции ip (совместимость).
func (c *Chunk) LineAt(ip int) int {
	if ip < 0 || ip >= len(c.Pos) {
		return 0
	}
	return int(c.Pos[ip].Line)
}

// PosAt — позиция инструкции ip.
func (c *Chunk) PosAt(ip int) SrcPos {
	if ip < 0 || ip >= len(c.Pos) {
		return SrcPos{}
	}
	return c.Pos[ip]
}

// Disassemble печатает чанк в человекочитаемом виде (§9).
func (c *Chunk) Disassemble(name string) string {
	var sb strings.Builder
	extra := ""
	if c.Variadic {
		extra = " variadic"
	}
	fmt.Fprintf(&sb,
		"== %s params=%d%s regs=%d consts=%d patterns=%d ==\n",
		name, c.NumParams, extra, c.NumRegs, len(c.Constants), len(c.Patterns))
	for ip := range c.Code {
		c.disInstr(&sb, ip)
	}
	return sb.String()
}

func (c *Chunk) disInstr(sb *strings.Builder, ip int) {
	in := c.Code[ip]
	pos := c.PosAt(ip)
	fmt.Fprintf(sb, "%04d %3d:%-3d %-11s", ip, pos.Line, pos.Col, in.Op())

	switch in.Op() {
	case LOADK, GETGLOBAL, SETGLOBAL:
		k := in.Bx()
		fmt.Fprintf(sb, "r%d k%d ; %s", in.A(), k, c.Constants[k].Inspect())
	case MOVE, GETUPVAL, NEG, NOT, MAKEOK, MAKEERROR, WATCH, UNWATCH, MAILBOXSIZE:
		fmt.Fprintf(sb, "r%d r%d", in.A(), in.B())
	case ADD, SUB, MUL, DIV, INTDIV, REM, POW,
		EQ, NEQ, LT, GT, LE, GE, RANGE, INDEX:
		fmt.Fprintf(sb, "r%d r%d r%d", in.A(), in.B(), in.C())
	case JMP:
		fmt.Fprintf(sb, "-> %04d", ip+1+in.SBx())
	case JMPIFNOT, JMPIF:
		fmt.Fprintf(sb, "r%d -> %04d", in.A(), ip+1+in.SBx())
	case CALL:
		fmt.Fprintf(sb, "r%d %d -> r%d", in.A(), in.B(), in.C())
	case TAILCALL:
		fmt.Fprintf(sb, "r%d %d", in.A(), in.B())
	case RETURN, RAISE, SELF, MAKEREF, RECVTIMER:
		fmt.Fprintf(sb, "r%d", in.A())
	case TUPLE, LIST, VECTOR:
		fmt.Fprintf(sb, "r%d <- r%d..r%d", in.A(), in.B(), in.B()+in.C()-1)
	case MAP:
		fmt.Fprintf(sb, "r%d <- r%d..r%d", in.A(), in.B(), in.B()+2*in.C()-1)
	case MAKECLOSURE:
		fmt.Fprintf(sb, "r%d <- r%d +%d", in.A(), in.B(), in.C())
	case TRAPBEGIN:
		fmt.Fprintf(sb, "r%d handler -> %04d", in.A(), ip+1+in.SBx())
	case TRAPEND, YIELD:
		// без операндов
	case SPAWN:
		if in.C() == 1 {
			fmt.Fprintf(sb, "r%d <- spawn(r%d) linked", in.A(), in.B())
		} else {
			fmt.Fprintf(sb, "r%d <- spawn(r%d)", in.A(), in.B())
		}
	case SEND:
		fmt.Fprintf(sb, "r%d <- send(r%d, r%d)", in.A(), in.B(), in.C())
	case RECVTAKE:
		if in.SBx() != 0 {
			fmt.Fprintf(sb, "r%d after -> %04d", in.A(), ip+1+in.SBx())
		} else {
			fmt.Fprintf(sb, "r%d", in.A())
		}
	case MATCHLOCAL:
		fmt.Fprintf(sb, "r%d p%d ; %s",
			in.A(), in.Bx(), FormatCompiledPattern(c.Patterns[in.Bx()]))
	default:
		fmt.Fprintf(sb, "?%d", in.Op())
	}
	sb.WriteByte('\n')
}

// Function — скомпилированная функция (§2).
type Function struct {
	Name  string
	Arity int
	Chunk *Chunk
}

// IsBrigCode реализует runtime.Code.
func (c *Chunk) IsBrigCode() {}
