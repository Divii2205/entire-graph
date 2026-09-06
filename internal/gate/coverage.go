package gate

// Coverage answers: did anything verify this?
//
// PHASE 0 STUB — the signature and the not-run contract are final; the body is
// Lane B's. Until it is written the check reports itself as not run, which under
// the degradation rule caps the verdict at continue. That is the correct
// behaviour for a missing input, and it means the rest of the pipeline can be
// built and demonstrated before this exists.
//
// When implementing: the repo's own test resolver (internal/sem/search_verify.go)
// is NOT reachable from here — every derivation in it is unexported and keyed off
// search results rather than a diff. Resolve tests from Input.Tests, which collect
// fills using file-mirror conventions, and record the evidence so a reviewer can
// open the test that supposedly covers the change.
func Coverage(input Input) CheckResult {
	if reason, missing := input.Unavailable[CheckCoverage]; missing {
		return NotRun(CheckCoverage, reason, VerdictContinue)
	}
	return NotRun(CheckCoverage, "not implemented yet", VerdictContinue)
}

// coverageOf reports what the coverage check concluded about one entity. It is
// the single place the merged report reads coverage from, so the distinction
// between "looked and found nothing" and "could not look" is made exactly once.
func coverageOf(input Input, check CheckResult, ref EntityRef) (CoverageState, []Evidence) {
	if !check.Ran {
		return CoverageUnknown, nil
	}
	tests := append([]Evidence(nil), input.Tests[ref.Key()]...)
	if len(tests) == 0 {
		return CoverageUnchecked, nil
	}
	SortEvidence(tests)
	return CoverageCovered, tests
}
