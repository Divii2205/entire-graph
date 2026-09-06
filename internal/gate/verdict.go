package gate

import "fmt"

// VerdictRules is the rule set, in the words the tool judges by, printed in
// every run so Gate is never a black box. If the code below and these lines ever
// disagree, the lines are the bug.
func VerdictRules() []string {
	return []string{
		"keep     (exit 0)  nothing with dependents changed, and no check found anything",
		"continue (exit 1)  something has dependents, or a check found something, or new code has no test",
		"revert   (exit 2)  something that breaks callers has dependents AND no covering test",
		"unusable (exit 5)  Gate ran but could not get evidence - do not build on this report",
		"",
		"A check that did not run is reported as not-run, never as a failure, and can",
		"never push the verdict upward: missing risk or coverage evidence caps the",
		"verdict at continue, and missing both makes the run unusable.",
	}
}

// Decide sets the verdict, exit code and reasons on a built report.
//
// It is the only place that reads two checks at once. The revert rule needs risk
// AND coverage together — "has dependents" is one check and "nothing verifies it"
// is another — so it cannot live inside either of them without one check reaching
// into the other, which the one-way data flow forbids.
func Decide(report *Report) {
	verdict, reasons := judge(*report)
	verdict, reasons = degrade(*report, verdict, reasons)

	report.Verdict = verdict
	report.ExitCode = verdict.ExitCode()
	report.Reasons = reasons
}

// judge applies the rules as if every check had run. The degradation rule is
// applied afterwards, separately, so that the two concerns stay legible: this
// function answers "what does the evidence say", degrade answers "how much of the
// evidence do we actually have".
func judge(report Report) (Verdict, []string) {
	verdict := VerdictKeep
	var reasons []string
	seen := make(map[string]bool)

	addReason := func(text string) {
		if seen[text] {
			return
		}
		seen[text] = true
		reasons = append(reasons, text)
	}

	if report.TotalEntities == 0 {
		return VerdictKeep, []string{"no entity-level changes in this range"}
	}

	// report.Review is already sorted, so the reasons come out in a fixed order.
	for _, item := range report.Review {
		change := item.Change
		covered := item.Coverage == CoverageCovered

		// The revert rule. CoverageUnknown is treated as uncovered here and
		// corrected by degrade, rather than being special-cased twice.
		if change.Change.BreaksCallers() && change.Dependents > 0 && !covered {
			verdict = worseOf(verdict, VerdictRevert)
			// The clause is worded from the coverage STATE, not from the rule.
			// Saying "no covering test" when the coverage check never ran would be
			// an accusation Gate has no evidence for, and it is the exact confusion
			// the unknown state exists to prevent.
			addReason(fmt.Sprintf("%s at %s is %s with %d dependent%s and %s",
				change.Name, change.Location(), change.Change, change.Dependents,
				plural(change.Dependents), uncoveredClause(item.Coverage)))
			continue
		}

		if change.Dependents > 0 {
			verdict = worseOf(verdict, VerdictContinue)
			addReason(fmt.Sprintf("%s at %s has %d dependent%s",
				change.Name, change.Location(), change.Dependents, plural(change.Dependents)))
		}

		// New code that nothing verifies. Deliberately not a revert: there are no
		// dependents to break yet, but there is also nothing holding it up.
		if change.Change == ChangeAdded && item.Coverage == CoverageUnchecked {
			verdict = worseOf(verdict, VerdictContinue)
			addReason(fmt.Sprintf("%s at %s is new and has no test", change.Name, change.Location()))
		}

		// A finding may raise the floor on its own — a companion gap does, because
		// forgotten work is worth a look whatever the tests say.
		for _, finding := range item.Findings {
			if finding.Floor.severity() <= verdict.severity() {
				continue
			}
			verdict = worseOf(verdict, finding.Floor)
			addReason(fmt.Sprintf("%s: %s (%s)", finding.Subject.Name, finding.Summary, finding.Check))
		}
	}

	return verdict, reasons
}

// degrade applies the degradation rule: a check that did not run can never push
// the verdict upward.
//
// Without it, a coverage check that failed to run would mean nothing has a
// covering test, so every risky change would read as revert. That is not
// strictness — it is a false accusation caused by a missing input.
func degrade(report Report, verdict Verdict, reasons []string) (Verdict, []string) {
	riskRan := report.Ran(CheckRisk)
	coverageRan := report.Ran(CheckCoverage)

	// Both of the checks that can excuse a change are gone. Gate has an opinion
	// about nothing, and says so rather than guessing.
	if !riskRan && !coverageRan {
		return VerdictUnusable, append(reasons,
			"the risk and coverage checks both did not run - there is not enough evidence to judge this range")
	}

	for _, check := range report.Checks {
		if check.Ran || check.Cap == noCeiling {
			continue
		}
		if verdict.severity() <= check.Cap.severity() {
			continue
		}
		verdict = check.Cap
		reasons = append(reasons, fmt.Sprintf(
			"the %s check did not run (%s), so the verdict is capped at %s",
			check.ID, notRunReason(check), check.Cap))
	}
	return verdict, reasons
}

// uncoveredClause distinguishes the two ways an entity can fail to be covered.
func uncoveredClause(state CoverageState) string {
	if state == CoverageUnknown {
		return "coverage was not checked"
	}
	return "no covering test"
}

func notRunReason(check CheckResult) string {
	if check.Reason == "" {
		return "no reason given"
	}
	return check.Reason
}
