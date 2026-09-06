package gate

// Clones answers: are there near-duplicate copies that did not get the fix?
//
// PHASE 0 STUB — signature and not-run contract are final; the body is Lane C's.
//
// Cheaper than the plan assumed: the graph already emits a SIMILAR_TO relation
// for near-duplicate bodies (MinHash >= 0.82, see internal/sem/provider.go), so
// collect fills Input.Clones from a graph query rather than from a clone detector
// written here. The finding to produce is "you fixed one copy, N others still
// have it": for each changed entity, its SIMILAR_TO peers whose own files are
// absent from Input.ChangedFiles.
func Clones(input Input) CheckResult {
	if reason, missing := input.Unavailable[CheckClones]; missing {
		return NotRun(CheckClones, reason, noCeiling)
	}
	return NotRun(CheckClones, "not implemented yet", noCeiling)
}
