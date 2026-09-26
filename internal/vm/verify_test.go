package vm

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
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
	wantVerifyError(t, c, "RETURN at 3 reads undefined register r2")
}

// I-F1: чтение неопределённого r2 в after-ветке RECVTAKE (T-36).
func TestVerifyRecvAfterUndefinedReg(t *testing.T) {
	c := mkChunk(3,
		AsBx(RECVTAKE, 0, 1), // 0: after -> 2
		ABC(RETURN, 0, 0, 0), // 1
		ABC(RETURN, 2, 0, 0), // 2: after-body reads undefined r2
	)
	wantVerifyError(t, c, "RETURN at 2 reads undefined register r2")
}

func wantVerifyError(t *testing.T, c *Chunk, want string) {
	t.Helper()
	err := Verify(c)
	if err == nil {
		t.Fatalf("Verify: want error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Verify: want error containing %q, got %v", want, err)
	}
}

// T-55: timeout-ребро RECVTAKE не пишет R[A] — чтение R[A] в after-теле
// должно отвергаться, а на ребре сообщения (ip+1) — приниматься.
func TestVerifyRecvAfterReadsMsgReg(t *testing.T) {
	c := mkChunk(2,
		AsBx(RECVTAKE, 0, 1), // 0: r0 = msg; after -> 2
		ABC(RETURN, 0, 0, 0), // 1: message edge, r0 defined
		ABC(RETURN, 0, 0, 0), // 2: after edge, r0 undefined
	)
	wantVerifyError(t, c, "RETURN at 2 reads undefined register r0")

	ok := mkChunk(2,
		AsBx(RECVTAKE, 0, 1), // 0: after -> 2
		ABC(RETURN, 0, 0, 0), // 1: reads r0 on message edge
		ABx(LOADK, 1, 0),     // 2: after-body
		ABC(RETURN, 1, 0, 0), // 3
	)
	if err := Verify(ok); err != nil {
		t.Fatalf("Verify rejected read of R[A] on RECVTAKE message edge: %v", err)
	}
}

// T-55: слоты паттерна определены на success-ребре MATCHLOCAL (ip+2).
func TestVerifyMatchLocalSuccessSlotDefined(t *testing.T) {
	c := mkChunk(2,
		AsBx(RECVTAKE, 0, 0),  // 0: r0 = msg
		ABx(MATCHLOCAL, 0, 0), // 1: r1 <- pattern slot
		AsBx(JMP, 0, 1),       // 2: fail -> 4
		ABC(RETURN, 1, 0, 0),  // 3: success body reads r1
		ABC(RAISE, 0, 0, 0),   // 4
	)
	c.AddPattern(&CompiledPattern{Kind: PatIdent, Slot: 1})
	if err := Verify(c); err != nil {
		t.Fatalf("Verify rejected read of pattern slot on success edge: %v", err)
	}
}

// T-55: на fail-ребре MATCHLOCAL слоты паттерна не определены.
func TestVerifyMatchLocalFailEdgeSlotUndefined(t *testing.T) {
	c := mkChunk(2,
		AsBx(RECVTAKE, 0, 0),  // 0
		ABx(MATCHLOCAL, 0, 0), // 1
		AsBx(JMP, 0, 1),       // 2: fail -> 4
		ABC(RETURN, 1, 0, 0),  // 3
		ABC(RETURN, 1, 0, 0),  // 4: fail path reads r1
	)
	c.AddPattern(&CompiledPattern{Kind: PatIdent, Slot: 1})
	wantVerifyError(t, c, "RETURN at 4 reads undefined register r1")
}

// T-55: success-ребро MATCHLOCAL (ip+2) не может выходить за конец кода.
func TestVerifyMatchLocalSuccessOutOfRange(t *testing.T) {
	c := mkChunk(1,
		AsBx(RECVTAKE, 0, 0),  // 0
		ABx(MATCHLOCAL, 0, 0), // 1: success -> 3 == len(Code)
		AsBx(JMP, 0, -3),      // 2: fail -> 0
	)
	c.AddPattern(&CompiledPattern{Kind: PatWildcard})
	wantVerifyError(t, c, "MATCHLOCAL at 1: success target 3 out of range")
}

// T-55: индекс паттерна проверяется и в недостижимом коде.
func TestVerifyMatchLocalBadPatternIndex(t *testing.T) {
	c := mkChunk(1,
		ABx(LOADK, 0, 0),      // 0
		ABC(RETURN, 0, 0, 0),  // 1
		ABx(MATCHLOCAL, 0, 5), // 2: unreachable, no pattern 5
		AsBx(JMP, 0, -4),      // 3: -> 0
		ABC(RETURN, 0, 0, 0),  // 4
	)
	wantVerifyError(t, c, "MATCHLOCAL at 2: pattern 5 out of range")
}

// T-55: слоты паттерна обязаны лежать в [0, NumRegs).
func TestVerifyPatternSlotOutOfRange(t *testing.T) {
	c := mkChunk(2,
		AsBx(RECVTAKE, 0, 0),  // 0
		ABx(MATCHLOCAL, 0, 0), // 1
		AsBx(JMP, 0, 1),       // 2: fail -> 4
		ABC(RETURN, 0, 0, 0),  // 3
		ABC(RAISE, 0, 0, 0),   // 4
	)
	c.AddPattern(&CompiledPattern{Kind: PatTuple, Subs: []*CompiledPattern{
		{Kind: PatIdent, Slot: 1},
		{Kind: PatIdent, Slot: 7},
	}})
	wantVerifyError(t, c, "MATCHLOCAL at 1: pattern 0 slot 7 out of range [0,2)")
}

// T-55: Slots() совпадает с регистрами, которые реально пишет MatchPattern
// при успешном сопоставлении — для каждого PatternKind.
func TestCompiledPatternSlotsMatchesMatchPattern(t *testing.T) {
	ident := func(slot int) *CompiledPattern {
		return &CompiledPattern{Kind: PatIdent, Slot: slot}
	}
	tests := []struct {
		kind PatternKind
		pat  *CompiledPattern
		val  runtime.Value
	}{
		{PatWildcard, &CompiledPattern{Kind: PatWildcard}, runtime.Int(1)},
		{PatIdent, ident(0), runtime.Int(1)},
		{PatLiteral, &CompiledPattern{Kind: PatLiteral, Lit: runtime.Int(1)}, runtime.Int(1)},
		{PatCtor, &CompiledPattern{Kind: PatCtor, Tag: "Some", Subs: []*CompiledPattern{ident(1)}},
			runtime.Variant("Some", runtime.Int(1))},
		{PatTuple, &CompiledPattern{Kind: PatTuple, Subs: []*CompiledPattern{
			ident(0), {Kind: PatWildcard}, ident(2)}},
			runtime.Tuple(runtime.Int(1), runtime.Int(2), runtime.Int(3))},
		{PatList, &CompiledPattern{Kind: PatList, HasRest: true, RestSlot: 3,
			Subs: []*CompiledPattern{ident(1)}},
			runtime.List(runtime.Int(1), runtime.Int(2))},
		{PatList, &CompiledPattern{Kind: PatList, HasRest: true, RestSlot: -1,
			Subs: []*CompiledPattern{ident(1)}},
			runtime.List(runtime.Int(1), runtime.Int(2))},
		{PatMap, &CompiledPattern{Kind: PatMap, Pairs: []MapPatPair{
			{Key: runtime.Atom("a"), Value: ident(2)},
			{Key: runtime.Atom("b"), Value: &CompiledPattern{Kind: PatWildcard}}}},
			runtime.Map([]runtime.MapEntry{
				{Key: runtime.Atom("a"), Val: runtime.Int(1)},
				{Key: runtime.Atom("b"), Val: runtime.Int(2)}})},
		{PatAs, &CompiledPattern{Kind: PatAs, AsSlot: 4, Inner: &CompiledPattern{
			Kind: PatTuple, Subs: []*CompiledPattern{ident(0)}}},
			runtime.Tuple(runtime.Int(1))},
	}

	covered := map[PatternKind]bool{}
	for _, tt := range tests {
		covered[tt.kind] = true
		unset := runtime.Atom("__unset__")
		locals := make([]runtime.Value, 8)
		for i := range locals {
			locals[i] = unset
		}
		if !MatchPattern(tt.val, tt.pat, locals) {
			t.Fatalf("%s: MatchPattern(%s) = false, want true",
				FormatCompiledPattern(tt.pat), tt.val.Inspect())
		}
		written := map[int]bool{}
		for i, v := range locals {
			if !runtime.Equal(v, unset) {
				written[i] = true
			}
		}
		slots := map[int]bool{}
		for _, s := range tt.pat.Slots() {
			slots[s] = true
		}
		if len(written) != len(slots) {
			t.Errorf("%s: MatchPattern wrote %v, Slots() = %v",
				FormatCompiledPattern(tt.pat), written, slots)
			continue
		}
		for s := range written {
			if !slots[s] {
				t.Errorf("%s: MatchPattern wrote %v, Slots() = %v",
					FormatCompiledPattern(tt.pat), written, slots)
				break
			}
		}
	}
	for k := PatWildcard; k <= PatAs; k++ {
		if !covered[k] {
			t.Errorf("PatternKind %d not covered", k)
		}
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
