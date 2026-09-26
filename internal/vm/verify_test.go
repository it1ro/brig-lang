package vm

import (
	"strings"
	"testing"
)

func mkChunk(n int, code ...Instr) *Chunk {
	c := NewChunk()
	c.NumRegs = n
	for _, in := range code {
		c.Emit(in, SrcPos{})
	}
	return c
}

// I-F2: TAILCALL между TRAPBEGIN и TRAPEND — Verify обязан отвергнуть.
func TestVerifyRejectsTailCallUnderTrap(t *testing.T) {
	c := mkChunk(3,
		ABx(LOADK, 1, 0),
		AsBx(TRAPBEGIN, 0, 2),
		ABC(TAILCALL, 1, 0, 0),
		ABC(TRAPEND, 0, 0, 0),
		ABC(RETURN, 0, 0, 0),
	)
	if err := Verify(c); err == nil {
		t.Fatal("Verify: want error for TAILCALL under trap, got nil")
	} else if !strings.Contains(err.Error(), "TAILCALL") {
		t.Fatalf("Verify: want TAILCALL in error, got %v", err)
	}
}

// I-F2: RunMain без Verify на чанке с TRAPBEGIN; TAILCALL → runtime guard.
func TestVMTailCallUnderTrapGuard(t *testing.T) {
	c := mkChunk(2,
		AsBx(TRAPBEGIN, 0, 1),
		ABC(TAILCALL, 0, 0, 0),
		ABC(RETURN, 0, 0, 0),
	)
	fn := &Function{Name: "main", Arity: 0, Chunk: c}
	m := New()
	_, err := m.RunMain(FuncValue(fn))
	if err == nil {
		t.Fatal("RunMain: want internal: TAILCALL under active trap, got nil")
	}
	if !strings.Contains(err.Error(), "internal: TAILCALL under active trap") {
		t.Fatalf("RunMain: got %v, want internal: TAILCALL under active trap", err)
	}
}

// I-F1: чтение неопределённого r2 в теле ветки после MATCHLOCAL+JMP (T-36).
func TestVerifyMatchLocalBranchUndefinedReg(t *testing.T) {
	c := mkChunk(3,
		AsBx(RECVTAKE, 0, 0),  // 0: r0 = msg
		ABx(MATCHLOCAL, 0, 0), // 1
		AsBx(JMP, 0, 1),       // 2: fail -> 4
		ABC(RETURN, 2, 0, 0),  // 3: reads r2 — never defined
		ABC(RAISE, 0, 0, 0),   // 4
	)
	c.AddPattern(&CompiledPattern{Kind: PatWildcard})
	if err := Verify(c); err == nil {
		t.Errorf("Verify accepted read of undefined r2 on MATCHLOCAL success edge")
	}
}

// I-F1: чтение неопределённого r2 в after-ветке RECVTAKE (T-36).
func TestVerifyRecvAfterUndefinedReg(t *testing.T) {
	c := mkChunk(3,
		AsBx(RECVTAKE, 0, 1), // 0: after -> 2
		ABC(RETURN, 0, 0, 0), // 1
		ABC(RETURN, 2, 0, 0), // 2: after-body reads undefined r2
	)
	if err := Verify(c); err == nil {
		t.Errorf("Verify accepted read of undefined r2 in after-body")
	}
}

func TestCompiledPatternSlots(t *testing.T) {
	tests := []struct {
		name string
		pat  *CompiledPattern
		want []int
	}{
		{
			name: "wildcard",
			pat:  &CompiledPattern{Kind: PatWildcard},
			want: nil,
		},
		{
			name: "ident",
			pat:  &CompiledPattern{Kind: PatIdent, Slot: 3},
			want: []int{3},
		},
		{
			name: "ident negative slot",
			pat:  &CompiledPattern{Kind: PatIdent, Slot: -1},
			want: nil,
		},
		{
			name: "nested tuple",
			pat: &CompiledPattern{
				Kind: PatTuple,
				Subs: []*CompiledPattern{
					{Kind: PatIdent, Slot: 1},
					{Kind: PatTuple, Subs: []*CompiledPattern{
						{Kind: PatIdent, Slot: 2},
						{Kind: PatWildcard},
					}},
				},
			},
			want: []int{1, 2},
		},
		{
			name: "list with rest",
			pat: &CompiledPattern{
				Kind:     PatList,
				HasRest:  true,
				RestSlot: 5,
				Subs: []*CompiledPattern{
					{Kind: PatIdent, Slot: 4},
				},
			},
			want: []int{5, 4},
		},
		{
			name: "as-pattern",
			pat: &CompiledPattern{
				Kind:   PatAs,
				AsSlot: 7,
				Inner:  &CompiledPattern{Kind: PatIdent, Slot: 8},
			},
			want: []int{7, 8},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pat.Slots()
			if len(got) != len(tt.want) {
				t.Fatalf("Slots() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Slots() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
