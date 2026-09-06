package gate

import "fmt"

// Risk answers: what breaks if this changed?
//
// The evidence is entirely code the change did not write — the dependent count
// the graph computed from the committed tree, and the call sites behind it. That
// is the property the whole product rests on: an agent cannot talk its way past
// this check, because the check never reads anything the agent produced.
func Risk(input Input) CheckResult {
	if reason, missing := input.Unavailable[CheckRisk]; missing {
		// Cap at continue, never revert: with no dependent counts every change
		// looks harmless, and a check that did not run must not be allowed to
		// produce a keep either.
		return NotRun(CheckRisk, reason, VerdictContinue)
	}

	result := CheckResult{ID: CheckRisk, Ran: true}
	for _, change := range input.Changes {
		if change.Dependents == 0 {
			// Nothing points at it. Not evidence of safety, but nothing for a
			// reviewer to read either, so it stays out of the review order.
			continue
		}
		finding := Finding{
			Check:   CheckRisk,
			Subject: change.EntityRef,
			Summary: riskSummary(change),
			// Informational on its own. Whether a dependent count means keep,
			// continue or revert depends on coverage, and no single check may
			// answer a question that needs two — see Decide.
			Floor: VerdictKeep,
		}
		evidence := append([]Evidence(nil), input.Callers[change.Key()]...)
		SortEvidence(evidence)
		finding.Evidence = evidence
		result.Findings = append(result.Findings, finding)
	}
	sortFindings(result.Findings)
	return result
}

// riskSummary states the fact, not the advice: what changed and how many things
// the graph can see that depend on it.
func riskSummary(change Change) string {
	return fmt.Sprintf("%s · %d dependent%s", change.Change, change.Dependents, plural(change.Dependents))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
