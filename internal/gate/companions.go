package gate

// CompanionThreshold and CompanionMinObservations are the hand-tuned constants
// behind the companion gap. They are printed in the report rather than hidden,
// because they are a judgement call and not a learned value: a file that moved
// with the subject in at least this share of at least this many observed commits
// is treated as a companion.
const (
	CompanionThreshold       = 0.70
	CompanionMinObservations = 5
)

// Companions answers: what normally changes with this, but did not this time?
//
// PHASE 0 STUB — signature and not-run contract are final; the body is Lane C's.
//
// This is the check that finds FORGOTTEN work, and it is the reason Gate is not
// just a nicer diff: the evidence is not in the change. It comes from git history
// older than the session, via gitutil.FileCochanges, which collect places in
// Input.Cochanges. A finding here sets Floor VerdictContinue on its own — a file
// that has accompanied this one in 14 of the last 15 commits and is absent now is
// worth a human look regardless of what the coverage check thinks.
func Companions(input Input) CheckResult {
	if reason, missing := input.Unavailable[CheckCompanions]; missing {
		return NotRun(CheckCompanions, reason, noCeiling)
	}
	return NotRun(CheckCompanions, "not implemented yet", noCeiling)
}

// noCeiling is the empty Cap: this check imposes no ceiling when it does not run.
//
// Only risk and coverage cap the verdict, and the reason is directional. Those
// two are the checks that can EXCUSE a change — a low dependent count or a
// passing test is what moves a verdict toward keep — so losing them means Gate no
// longer knows the change is fine, and it must stop short of saying so. The
// companion and clone checks only ever accuse. Losing one removes a reason to
// worry; it never invents a reason to relax, so capping on them would suppress
// findings the checks that DID run legitimately produced.
const noCeiling Verdict = ""
