package gate

import "sort"

// Evaluate is the whole pipeline: run every check over the collected evidence,
// merge it into a review order, and decide. It is the only function internal/cli
// needs to call, and it is pure — the same Input always produces the same Report,
// byte for byte, which is what lets Gate block a push.
func Evaluate(input Input) Report {
	checks := RunChecks(input)
	report := Build(input, checks)
	Decide(&report)
	return report
}

// RunChecks runs the four checks in CheckOrder. The order is fixed rather than
// derived from a map so the report reads the same way every run, and the checks
// are independent by design: adding a fifth evidence source is a new file plus a
// line here, not a change to anything that already works.
func RunChecks(input Input) []CheckResult {
	return []CheckResult{
		Risk(input),
		Coverage(input),
		Companions(input),
		Clones(input),
	}
}

// Build merges the checks into one entity-per-row review order.
//
// A check that did not run contributes nothing: its findings are dropped rather
// than trusted, because a check that reports itself as not-run may have produced
// a partial finding list before it gave up, and half an answer read as a whole
// one is how a gate starts lying.
func Build(input Input, checks []CheckResult) Report {
	report := Report{
		Repo:          input.Repo,
		Base:          input.Base,
		Head:          input.Head,
		TotalEntities: len(input.Changes),
		Checks:        orderChecks(checks),
	}

	coverageCheck := findCheck(report.Checks, CheckCoverage)

	// Findings are grouped by their subject so one entity is one row, no matter
	// how many checks had something to say about it.
	bySubject := make(map[string][]Finding)
	for _, check := range report.Checks {
		if !check.Ran {
			continue
		}
		for _, finding := range check.Findings {
			key := finding.Subject.Key()
			bySubject[key] = append(bySubject[key], finding)
		}
	}

	// Ranged over input.Changes, NOT over bySubject: a map range would order the
	// review differently on every run.
	for _, change := range input.Changes {
		state, evidence := coverageOf(input, coverageCheck, change.EntityRef)
		item := ReviewItem{
			Change:           change,
			Coverage:         state,
			CoverageEvidence: evidence,
			Findings:         bySubject[change.Key()],
		}
		sortFindings(item.Findings)
		if item.Quiet() {
			report.QuietCount++
			continue
		}
		report.Review = append(report.Review, item)
	}
	sortReview(report.Review)
	return report
}

// orderChecks returns the results in CheckOrder, so a caller that assembles them
// in some other order still produces an identically ordered report.
func orderChecks(checks []CheckResult) []CheckResult {
	ordered := make([]CheckResult, 0, len(checks))
	for _, id := range CheckOrder {
		for _, check := range checks {
			if check.ID == id {
				ordered = append(ordered, check)
			}
		}
	}
	// Anything not in CheckOrder is a check someone added without registering it.
	// Keep it rather than drop it, appended in the caller's order, so it shows up
	// in the report instead of vanishing.
	for _, check := range checks {
		if !knownCheck(check.ID) {
			ordered = append(ordered, check)
		}
	}
	return ordered
}

func knownCheck(id CheckID) bool {
	for _, known := range CheckOrder {
		if known == id {
			return true
		}
	}
	return false
}

func findCheck(checks []CheckResult, id CheckID) CheckResult {
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	return CheckResult{ID: id}
}

// sortReview puts the review order in the order a reviewer should read it, with
// a total ordering all the way down so two runs cannot differ.
//
// Blocking rows come first — the ones where something that breaks callers has
// dependents and nothing verifies it — because those are the rows that decide the
// verdict. After that it is dependent count, then how much was found, then the
// location, which is the tie-break that makes the sort total.
func sortReview(items []ReviewItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if blocking(left) != blocking(right) {
			return blocking(left)
		}
		if left.Change.Dependents != right.Change.Dependents {
			return left.Change.Dependents > right.Change.Dependents
		}
		if len(left.Findings) != len(right.Findings) {
			return len(left.Findings) > len(right.Findings)
		}
		if left.Change.Path != right.Change.Path {
			return left.Change.Path < right.Change.Path
		}
		if left.Change.Line != right.Change.Line {
			return left.Change.Line < right.Change.Line
		}
		return left.Change.Name < right.Change.Name
	})
}

// blocking reports the revert condition for one row: something that breaks code
// written before this change, with dependents, and nothing that verifies it.
//
// CoverageUnknown counts as unverified HERE on purpose. This function only ranks;
// it does not judge. Decide applies the degradation rule afterwards, so a missing
// coverage check still floats the riskiest rows to the top of the reading order
// while being forbidden from turning them into a revert.
func blocking(item ReviewItem) bool {
	return item.Change.Change.BreaksCallers() &&
		item.Change.Dependents > 0 &&
		item.Coverage != CoverageCovered
}

// sortFindings gives a finding list a total order for rendering.
func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if left.Check != right.Check {
			return checkRank(left.Check) < checkRank(right.Check)
		}
		if left.Subject.Path != right.Subject.Path {
			return left.Subject.Path < right.Subject.Path
		}
		if left.Subject.Line != right.Subject.Line {
			return left.Subject.Line < right.Subject.Line
		}
		if left.Subject.Name != right.Subject.Name {
			return left.Subject.Name < right.Subject.Name
		}
		return left.Summary < right.Summary
	})
}

// checkRank orders findings by CheckOrder rather than alphabetically, so a row
// reads risk first and the cheaper observations after.
func checkRank(id CheckID) int {
	for index, known := range CheckOrder {
		if known == id {
			return index
		}
	}
	return len(CheckOrder)
}
