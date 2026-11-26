package apply

// state is what Compute knows about one target (a file, a shared JSON entry or a list element)
// before deciding on it.
type state struct {
	// Blocked: a link, a folder or a non-folder parent is in the way; livery writes nothing there.
	Blocked bool
	// Present: the target exists on disk (never with Blocked).
	Present bool
	// Owned: the old lock records the target.
	Owned bool
	// MatchesWanted: present and the disk equals what the plans want. A list element has no
	// value, so for it this is Present.
	MatchesWanted bool
	// MatchesRecorded: present and the disk equals the sum in the old lock. A list element has
	// no value, so for it this is Present.
	MatchesRecorded bool
}

// act is what to do with a target; callers map it to their Op.
type act int

const (
	actNothing    act = iota // no action, no line
	actWrite                 // Write or SetEntry
	actUnchanged             // Unchanged
	actAdopt                 // Adopt
	actKept                  // KeptYours
	actEdited                // Edited
	actRemove                // Remove or UnsetEntry
	actLeftEdited            // LeftEdited
)

// recordSum is which sum the new lock records for a target.
type recordSum int

const (
	recordNone   recordSum = iota // not in the new lock
	recordWanted                  // the sum of what the plans want
	recordOld                     // the sum the old lock has
)

// decision is the outcome of decide.
type decision struct {
	Act    act
	Record recordSum
}

// decide maps what is known about one target to what to do with it and what to record. wanted
// is whether the plans still want the target. It reads nothing; callers do the I/O, the messages,
// Missing and the executable bit.
func decide(wanted bool, st state) decision {
	if !wanted {
		switch {
		case !st.Owned:
			return decision{actNothing, recordNone}
		case st.Blocked:
			// Something else took the place of the target: it is the user's now.
			return decision{actLeftEdited, recordNone}
		case !st.Present:
			// Already gone: nothing to do, and no longer owned.
			return decision{actNothing, recordNone}
		case st.MatchesRecorded:
			return decision{actRemove, recordNone}
		default:
			return decision{actLeftEdited, recordNone}
		}
	}
	switch {
	case st.Blocked:
		return decision{actKept, recordNone}
	case !st.Present:
		return decision{actWrite, recordWanted}
	case st.MatchesWanted && st.Owned:
		return decision{actUnchanged, recordWanted}
	case st.MatchesWanted:
		return decision{actAdopt, recordWanted}
	case !st.Owned:
		return decision{actKept, recordNone}
	case st.MatchesRecorded:
		return decision{actWrite, recordWanted}
	default:
		return decision{actEdited, recordOld}
	}
}

// op maps a decision's act to an Op; write and remove are the Ops for this kind of target
// (Write and Remove for files, SetEntry and UnsetEntry for entries). ok is false for actNothing.
func (d decision) op(write, remove Op) (op Op, ok bool) {
	switch d.Act {
	case actWrite:
		return write, true
	case actUnchanged:
		return Unchanged, true
	case actAdopt:
		return Adopt, true
	case actKept:
		return KeptYours, true
	case actEdited:
		return Edited, true
	case actRemove:
		return remove, true
	case actLeftEdited:
		return LeftEdited, true
	}
	return "", false
}

// sum returns the sum the decision records, given the wanted and the old sum; ok is false when
// nothing is recorded.
func (d decision) sum(newSum, oldSum string) (sum string, ok bool) {
	switch d.Record {
	case recordWanted:
		return newSum, true
	case recordOld:
		return oldSum, true
	}
	return "", false
}
