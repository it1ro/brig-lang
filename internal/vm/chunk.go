// Package vm — стековая байткод-машина Брига.
//
// Этот файл описывает контейнер байткода: последовательность инструкций
// одной функции (Chunk), таблицу констант и структуру скомпилированной
// функции (Function). Дизассемблер здесь — задел под обязательный
// --dump-bytecode (§15.1).
package vm

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// maxOperand — максимальное значение 16-битного операнда.
const maxOperand = 1<<16 - 1

// Chunk — последовательность инструкций одной функции.
//
// Формат инструкции:
//
//	[1 байт опкод] ([2 байта операнд, big-endian] — если есть операнд)
type Chunk struct {
	Code      []byte          // поток инструкций
	Constants []runtime.Value // пул констант (индекс = операнд у OpConstant)
	Lines     []int           // номер строки на каждый байт кода (для операндов — 0)
}

// NewChunk создаёт пустой чанк.
func NewChunk() *Chunk { return &Chunk{} }

// Emit пишет одну инструкцию с операндом и номером строки источника.
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

// AddConstant кладёт значение в пул констант и возвращает его индекс.
func (c *Chunk) AddConstant(v runtime.Value) int {
	c.Constants = append(c.Constants, v)
	return len(c.Constants) - 1
}

// OperandPos возвращает позицию операнда последней записанной инструкции.
func (c *Chunk) OperandPos() int {
	if len(c.Code) < 2 {
		panic("vm: OperandPos on empty chunk")
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

// ChunkCode возвращает слайс байтов кода чанка для прямого доступа
// (патчинг переходов в компиляторе).
func ChunkCode(c *Chunk) []byte { return c.Code }

// LineAt возвращает номер строки источника для позиции инструкции.
func (c *Chunk) LineAt(ip int) int {
	if ip < 0 || ip >= len(c.Lines) {
		return 0
	}
	return c.Lines[ip]
}

// hasOperand сообщает, несёт ли опкод 16-битный операнд.
func hasOperand(op OpCode) bool {
	switch op {
	case OpConstant, OpGetLocal, OpSetLocal, OpGetGlobal, OpSetGlobal,
		OpCall, OpJump, OpJumpFalse, OpJumpTrue,
		OpTuple, OpList, OpVector, OpMap,
		OpMakeClosure, OpGetUpvalue, OpSetUpvalue, OpDefineLocalFn:
		return true
	}
	return false
}

// Function — скомпилированная функция: имя, арность и её чанк.
//
// Арность: >= 0 фиксированная; -1 — вариадическая (..args).
type Function struct {
	Name  string
	Arity int
	Chunk *Chunk
}

// Disassemble печатает чанк в человекочитаемом виде.
func (c *Chunk) Disassemble(name string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "== %s ==\n", name)
	for ip := 0; ip < len(c.Code); {
		ip = c.disInstr(&sb, ip)
	}
	return sb.String()
}

func (c *Chunk) disInstr(sb *strings.Builder, ip int) int {
	op := OpCode(c.Code[ip])
	line := c.LineAt(ip)
	fmt.Fprintf(sb, "%04d %4d %-10s", ip, line, op)
	if hasOperand(op) {
		operand := int(c.Code[ip+1])<<8 | int(c.Code[ip+2])
		switch op {
		case OpConstant, OpGetGlobal, OpSetGlobal:
			fmt.Fprintf(sb, "%4d (%s)", operand, c.Constants[operand].Inspect())
		case OpCall, OpTuple, OpList, OpVector:
			fmt.Fprintf(sb, "%4d args/elems", operand)
		case OpMap:
			fmt.Fprintf(sb, "%4d pairs", operand)
		case OpJump, OpJumpFalse, OpJumpTrue:
			fmt.Fprintf(sb, "-> %04d", operand)
		default:
			fmt.Fprintf(sb, "%4d", operand)
		}
		sb.WriteByte('\n')
		return ip + 3
	}
	sb.WriteByte('\n')
	return ip + 1
}
