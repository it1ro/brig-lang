package vm

import "testing"

func TestInstrABC(t *testing.T) {
	cases := []struct {
		op      OpCode
		a, b, c int
	}{
		{ADD, 0, 0, 0},
		{ADD, 1, 2, 3},
		{CALL, 5, 255, 7},
		{LOADK, 200, 100, 50},
		{SPAWN, 3, 4, 1},
	}
	for _, tc := range cases {
		i := ABC(tc.op, tc.a, tc.b, tc.c)
		if i.Op() != tc.op {
			t.Errorf("ABC(%v,%d,%d,%d).Op = %v", tc.op, tc.a, tc.b, tc.c, i.Op())
		}
		if i.A() != tc.a || i.B() != tc.b || i.C() != tc.c {
			t.Errorf("ABC(%v,%d,%d,%d) = A=%d B=%d C=%d",
				tc.op, tc.a, tc.b, tc.c, i.A(), i.B(), i.C())
		}
	}
}

func TestInstrABx(t *testing.T) {
	cases := []struct {
		op    OpCode
		a, bx int
	}{
		{LOADK, 0, 0},
		{LOADK, 5, 40000},
		{GETGLOBAL, 255, 65535},
	}
	for _, tc := range cases {
		i := ABx(tc.op, tc.a, tc.bx)
		if i.Op() != tc.op {
			t.Errorf("ABx(%v,%d,%d).Op = %v", tc.op, tc.a, tc.bx, i.Op())
		}
		if i.A() != tc.a {
			t.Errorf("ABx(%v,%d,%d).A = %d", tc.op, tc.a, tc.bx, i.A())
		}
		if i.Bx() != tc.bx {
			t.Errorf("ABx(%v,%d,%d).Bx = %d", tc.op, tc.a, tc.bx, i.Bx())
		}
	}
}

func TestInstrAsBx(t *testing.T) {
	cases := []struct {
		a, sbx int
	}{
		{0, 0},
		{0, 1},
		{0, -1},
		{3, 1234},
		{3, -1234},
		{0, 32767},
		{0, -32768},
	}
	for _, tc := range cases {
		i := AsBx(JMP, tc.a, tc.sbx)
		if i.Op() != JMP {
			t.Errorf("AsBx(JMP,%d,%d).Op = %v", tc.a, tc.sbx, i.Op())
		}
		if i.A() != tc.a {
			t.Errorf("AsBx(JMP,%d,%d).A = %d", tc.a, tc.sbx, i.A())
		}
		if i.SBx() != tc.sbx {
			t.Errorf("AsBx(JMP,%d,%d).SBx = %d", tc.a, tc.sbx, i.SBx())
		}
	}
}

func TestPatchJump(t *testing.T) {
	c := NewChunk()
	c.Emit(AsBx(JMP, 3, 0), SrcPos{Line: 1, Col: 1})      // at 0
	c.Emit(ABC(RETURN, 0, 0, 0), SrcPos{Line: 1, Col: 2}) // at 1
	c.Emit(ABC(RETURN, 1, 0, 0), SrcPos{Line: 1, Col: 3}) // at 2

	if err := c.PatchJump(0, 2); err != nil {
		t.Fatalf("PatchJump: %v", err)
	}
	// target - (at+1) = 2 - 1 = 1
	if got := c.Code[0].SBx(); got != 1 {
		t.Errorf("sBx = %d, want 1", got)
	}
	if c.Code[0].A() != 3 {
		t.Errorf("A lost: got %d, want 3", c.Code[0].A())
	}
	if c.Code[0].Op() != JMP {
		t.Errorf("op lost: got %v, want JMP", c.Code[0].Op())
	}
}

func TestPatchJumpBackward(t *testing.T) {
	c := NewChunk()
	c.Emit(AsBx(JMP, 0, 0), SrcPos{})
	c.Emit(ABC(RETURN, 0, 0, 0), SrcPos{})
	c.Emit(ABC(RETURN, 1, 0, 0), SrcPos{})

	if err := c.PatchJump(0, -0); err == nil {
		// target = -0 = 0; sBx = 0 - 1 = -1 (допустимо)
	}
	// Реальный backward: target = 0 из ip=2.
	c.Emit(AsBx(JMP, 0, 0), SrcPos{}) // at 3
	if err := c.PatchJump(3, 0); err != nil {
		t.Fatalf("backward PatchJump: %v", err)
	}
	// target - (at+1) = 0 - 4 = -4
	if got := c.Code[3].SBx(); got != -4 {
		t.Errorf("backward sBx = %d, want -4", got)
	}
}

func TestPatchJumpOutOfRange(t *testing.T) {
	c := NewChunk()
	c.Emit(AsBx(JMP, 0, 0), SrcPos{})
	if err := c.PatchJump(0, 40000); err == nil {
		t.Error("want sBx range error for target 40000")
	}
	if err := c.PatchJump(-1, 0); err == nil {
		t.Error("want error for negative at")
	}
	if err := c.PatchJump(99, 0); err == nil {
		t.Error("want error for out-of-range at")
	}
}
