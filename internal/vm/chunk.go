package vm

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

const maxOperand = 1<<16 - 1

// Chunk — последовательность инструкций одной функции.
type Chunk struct {
	Code      []byte
	Constants []runtime.Value
	Patterns  []*CompiledPattern // пул скомпилированных паттернов (v0.4.8)
	Lines     []int
}

// NewChunk создаёт пустой чанк.
func NewChunk() *Chunk { return &Chunk{} }

// Emit пишет одну инструкцию с одним 16-битным операндом.
func (c *Chunk) Emit(op OpCode, operand, line int) {
	if hasOperand(op) && (operand < 0 || operand > maxOperand) {
		panic(fmt.Sprintf("vm: operand %d out of 16-bit range for %s", operand, op))
	}
	c.Code = append(c.Code, byte(op))
	c.Lines = append(c.Lines, line)
	if hasOperand(op) {
		c.Code = append(c.Code, byte(operand>>8), byte(operand))
		c.Lines = append(c.Lines, 0, 0)
	}
}

// EmitTwo пишет инструкцию с двумя 16-битными операндами
// (OpRecvTake, OpMatchLocal). 5 байт: [op][a_hi][a_lo][b_hi][b_lo].
func (c *Chunk) EmitTwo(op OpCode, a, b, line int) {
	if a < 0 || a > maxOperand || b < 0 || b > maxOperand {
		panic(fmt.Sprintf("vm: operand out of 16-bit range for %s", op))
	}
	c.Code = append(c.Code, byte(op))
	c.Lines = append(c.Lines, line)
	c.Code = append(c.Code, byte(a>>8), byte(a))
	c.Lines = append(c.Lines, 0, 0)
	c.Code = append(c.Code, byte(b>>8), byte(b))
	c.Lines = append(c.Lines, 0, 0)
}

// AddConstant кладёт значение в пул констант.
func (c *Chunk) AddConstant(v runtime.Value) int {
	c.Constants = append(c.Constants, v)
	return len(c.Constants) - 1
}

// AddPattern кладёт паттерн в пул паттернов.
func (c *Chunk) AddPattern(p *CompiledPattern) int {
	c.Patterns = append(c.Patterns, p)
	return len(c.Patterns) - 1
}

// OperandPos — позиция операнда последней записанной инструкции.
func (c *Chunk) OperandPos() int {
	if len(c.Code) < 2 {
		panic("vm: OperandPos on empty chunk")
	}
	return len(c.Code) - 2
}

// Operand2Pos — позиция второго операнда последней EmitTwo-инструкции.
func (c *Chunk) Operand2Pos() int {
	if len(c.Code) < 2 {
		panic("vm: Operand2Pos on empty chunk")
	}
	return len(c.Code) - 2
}

// PatchOperand записывает 16-битный операнд по байтовой позиции.
func (c *Chunk) PatchOperand(pos, value int) {
	if value < 0 || value > maxOperand {
		panic(fmt.Sprintf("vm: patch target %d out of 16-bit range", value))
	}
	if pos < 0 || pos+1 >= len(c.Code) {
		panic(fmt.Sprintf("vm: patch position %d out of range", pos))
	}
	c.Code[pos] = byte(value >> 8)
	c.Code[pos+1] = byte(value)
}

// ChunkCode возвращает слайс байтов кода.
func ChunkCode(c *Chunk) []byte { return c.Code }

// LineAt — номер строки источника для позиции инструкции.
func (c *Chunk) LineAt(ip int) int {
	if ip < 0 || ip >= len(c.Lines) {
		return 0
	}
	return c.Lines[ip]
}

// hasOperand — опкод несёт один 16-битный операнд.
func hasOperand(op OpCode) bool {
	switch op {
	case OpConstant, OpGetLocal, OpSetLocal, OpGetGlobal, OpSetGlobal,
		OpCall, OpJump, OpJumpFalse, OpJumpTrue,
		OpTuple, OpList, OpVector, OpMap,
		OpMakeClosure, OpGetUpvalue, OpSetUpvalue, OpDefineLocalFn,
		OpTrapBegin, OpSpawn:
		return true
	}
	return false
}

// opSize — полная длина инструкции в байтах.
func opSize(op OpCode) int {
	switch op {
	case OpRecvTake, OpMatchLocal:
		return 5
	}
	if hasOperand(op) {
		return 3
	}
	return 1
}

// Function — скомпилированная функция.
type Function struct {
	Name  string
	Arity int
	Chunk *Chunk
}

// Disassemble печатает чанк.
func (c *Chunk) Disassemble(name string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "== %s ==\n", name)
	for ip := 0; ip < len(c.Code); {
		ip = c.disInstr(&sb, ip)
	}
	if len(c.Patterns) > 0 {
		sb.WriteString("  patterns:\n")
		for i, p := range c.Patterns {
			fmt.Fprintf(&sb, "    [%d] %s (fail=%d)\n", i, FormatCompiledPattern(p), p.FailAddr)
		}
	}
	return sb.String()
}

func (c *Chunk) disInstr(sb *strings.Builder, ip int) int {
	op := OpCode(c.Code[ip])
	line := c.LineAt(ip)
	fmt.Fprintf(sb, "%04d %4d %-12s", ip, line, op)

	switch op {
	case OpRecvTake:
		slot := int(c.Code[ip+1])<<8 | int(c.Code[ip+2])
		after := int(c.Code[ip+3])<<8 | int(c.Code[ip+4])
		fmt.Fprintf(sb, "slot=%d after=%04d\n", slot, after)
		return ip + 5
	case OpMatchLocal:
		slot := int(c.Code[ip+1])<<8 | int(c.Code[ip+2])
		pi := int(c.Code[ip+3])<<8 | int(c.Code[ip+4])
		fmt.Fprintf(sb, "slot=%d pattern=%d\n", slot, pi)
		return ip + 5
	}

	if hasOperand(op) {
		operand := int(c.Code[ip+1])<<8 | int(c.Code[ip+2])
		switch op {
		case OpConstant, OpGetGlobal, OpSetGlobal:
			fmt.Fprintf(sb, "%4d (%s)", operand, c.Constants[operand].Inspect())
		case OpCall, OpTuple, OpList, OpVector:
			fmt.Fprintf(sb, "%4d args/elems", operand)
		case OpMap:
			fmt.Fprintf(sb, "%4d pairs", operand)
		case OpJump, OpJumpFalse, OpJumpTrue, OpTrapBegin:
			fmt.Fprintf(sb, "-> %04d", operand)
		case OpSpawn:
			if operand == 1 {
				fmt.Fprintf(sb, "linked")
			} else {
				fmt.Fprintf(sb, "unlinked")
			}
		default:
			fmt.Fprintf(sb, "%4d", operand)
		}
		sb.WriteByte('\n')
		return ip + 3
	}
	sb.WriteByte('\n')
	return ip + 1
}

// IsBrigCode реализует runtime.Code.
func (c *Chunk) IsBrigCode() {}
