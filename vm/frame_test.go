package vm

import "testing"

// TestErrorUnwindTruncatesStrayStackValues is the regression test for a
// latent bug the frame/trampoline rewrite fixed as a side effect: before
// frame.stackBase existed, a value pushed but never consumed by a failed
// instruction (e.g. the 99 below, still sitting under the two operands
// OpDiv itself pops before erroring) was never removed from mc.stack -
// there was nothing that reset it between Run() calls on a reused
// Machine, so it would linger forever, growing unboundedly across
// repeated errors. See unwindToCoalesce's doc comment.
func TestErrorUnwindTruncatesStrayStackValues(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatal(err)
	}
	instructions := []Instruction{
		{Op: OpPush, Arg: int64(99)}, // stranded once OpDiv below fails
		{Op: OpPush, Arg: int64(1)},
		{Op: OpPush, Arg: int64(0)},
		{Op: OpDiv},
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatal("expected a divide-by-zero error")
	}
	if len(mc.stack) != 0 {
		t.Fatalf("mc.stack has %d stray value(s) left after the error, want 0", len(mc.stack))
	}

	// A completely unrelated later Run() on the same (reused) Machine
	// must see a clean stack, not whatever an earlier failed call left
	// behind.
	got, err := mc.Run([]Instruction{{Op: OpPush, Arg: int64(42)}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != int64(42) {
		t.Fatalf("got %v, want 42", got)
	}
	if len(mc.stack) != 0 {
		t.Fatalf("mc.stack has %d value(s) left after a successful Run(), want 0", len(mc.stack))
	}
}

// TestErrorUnwindCaughtByCoalesceTruncatesStrayValues is the same
// scenario, but with the failing instructions run as an isolated "??"
// left-hand frame (via pushFrame's coalesce marker) rather than the
// top-level program - confirming unwindToCoalesce cleans up mc.stack at
// every frame it discards on its way to the catch point, not just the
// outermost one.
func TestErrorUnwindCaughtByCoalesceTruncatesStrayValues(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatal(err)
	}
	left := []Instruction{
		{Op: OpPush, Arg: int64(99)}, // stranded once OpDiv below fails
		{Op: OpPush, Arg: int64(1)},
		{Op: OpPush, Arg: int64(0)},
		{Op: OpDiv},
	}
	instructions := []Instruction{
		{Op: OpCoalesce, Arg: &CoalesceArg{Left: left, JumpEnd: 2}},
		{Op: OpPush, Arg: int64(-1)}, // right-hand fallback
	}
	got, err := mc.Run(instructions, nil)
	if err != nil {
		t.Fatalf("unexpected error (should have been caught by \"??\"): %v", err)
	}
	if got != int64(-1) {
		t.Fatalf("got %v, want -1", got)
	}
	if len(mc.stack) != 0 {
		t.Fatalf("mc.stack has %d stray value(s) left, want 0", len(mc.stack))
	}
}
