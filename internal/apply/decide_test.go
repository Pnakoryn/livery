package apply

import (
	"fmt"
	"testing"
)

// possible reports whether st can occur: a blocked target is not present, the disk can only
// match when present, and it can only match the recorded sum when owned.
func possible(st state) bool {
	if st.Blocked && st.Present {
		return false
	}
	if !st.Present && (st.MatchesWanted || st.MatchesRecorded) {
		return false
	}
	return !st.MatchesRecorded || st.Owned
}

func TestDecide(t *testing.T) {
	type row struct {
		wanted bool
		st     state
		want   decision
	}
	rows := []row{
		// Wanted.
		{true, state{Blocked: true}, decision{actKept, recordNone}},
		{true, state{Blocked: true, Owned: true}, decision{actKept, recordNone}},
		{true, state{}, decision{actWrite, recordWanted}},
		{true, state{Owned: true}, decision{actWrite, recordWanted}},
		{true, state{Present: true}, decision{actKept, recordNone}},
		{true, state{Present: true, MatchesWanted: true}, decision{actAdopt, recordWanted}},
		{true, state{Present: true, Owned: true}, decision{actEdited, recordOld}},
		{true, state{Present: true, Owned: true, MatchesRecorded: true}, decision{actWrite, recordWanted}},
		{true, state{Present: true, Owned: true, MatchesWanted: true}, decision{actUnchanged, recordWanted}},
		{true, state{Present: true, Owned: true, MatchesWanted: true, MatchesRecorded: true}, decision{actUnchanged, recordWanted}},
		// Not wanted.
		{false, state{Blocked: true}, decision{actNothing, recordNone}},
		{false, state{Blocked: true, Owned: true}, decision{actLeftEdited, recordNone}},
		{false, state{}, decision{actNothing, recordNone}},
		{false, state{Owned: true}, decision{actNothing, recordNone}},
		{false, state{Present: true}, decision{actNothing, recordNone}},
		{false, state{Present: true, MatchesWanted: true}, decision{actNothing, recordNone}},
		{false, state{Present: true, Owned: true}, decision{actLeftEdited, recordNone}},
		{false, state{Present: true, Owned: true, MatchesRecorded: true}, decision{actRemove, recordNone}},
		{false, state{Present: true, Owned: true, MatchesWanted: true}, decision{actLeftEdited, recordNone}},
		{false, state{Present: true, Owned: true, MatchesWanted: true, MatchesRecorded: true}, decision{actRemove, recordNone}},
	}

	seen := map[string]bool{}
	for _, r := range rows {
		name := fmt.Sprintf("wanted=%v/%+v", r.wanted, r.st)
		if !possible(r.st) {
			t.Errorf("%s: row for a state that cannot occur", name)
		}
		if seen[name] {
			t.Errorf("%s: row twice", name)
		}
		seen[name] = true
		if got := decide(r.wanted, r.st); got != r.want {
			t.Errorf("%s: decide = %+v, want %+v", name, got, r.want)
		}
	}

	// Every state that can occur has a row.
	for i := 0; i < 1<<6; i++ {
		bit := func(n int) bool { return i&(1<<n) != 0 }
		wanted := bit(0)
		st := state{Blocked: bit(1), Present: bit(2), Owned: bit(3), MatchesWanted: bit(4), MatchesRecorded: bit(5)}
		if !possible(st) {
			continue
		}
		if name := fmt.Sprintf("wanted=%v/%+v", wanted, st); !seen[name] {
			t.Errorf("%s: no row", name)
		}
	}
}
