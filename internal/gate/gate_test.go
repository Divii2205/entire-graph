package gate

import (
	"strings"
	"testing"
)

// riskyInput is one signature change with dependents: the shape every rule in
// section 6 turns on.
func riskyInput() Input {
	ref := EntityRef{Name: "VerifyToken", Kind: "function", Path: "internal/auth/token.go", Line: 88}
	return Input{
		Base: "main",
		Head: "HEAD",
		Changes: []Change{
			{EntityRef: ref, Change: ChangeSignatureChanged, Dependents: 14},
		},
		Callers: map[string][]Evidence{
			ref.Key(): {{Kind: EvidenceCaller, Path: "internal/api/middleware.go", Line: 31}},
		},
		Tests:        map[string][]Evidence{},
		Cochanges:    map[string][]Cochange{},
		Clones:       map[string][]EntityRef{},
		ChangedFiles: map[string]bool{"internal/auth/token.go": true},
		Unavailable:  map[CheckID]string{},
	}
}

func ranCheck(id CheckID, findings ...Finding) CheckResult {
	return CheckResult{ID: id, Ran: true, Findings: findings}
}

func decide(t *testing.T, input Input, checks []CheckResult) Report {
	t.Helper()
	report := Build(input, checks)
	Decide(&report)
	return report
}

func TestExitCodesAreTheContract(t *testing.T) {
	cases := map[Verdict]int{
		VerdictKeep:     0,
		VerdictContinue: 1,
		VerdictRevert:   2,
		VerdictUnusable: 5,
	}
	for verdict, want := range cases {
		if got := verdict.ExitCode(); got != want {
			t.Errorf("%s exit code = %d, want %d", verdict, got, want)
		}
	}
	// An unset verdict must never be mistaken for a pass.
	if got := Verdict("").ExitCode(); got != 5 {
		t.Errorf("empty verdict exit code = %d, want 5", got)
	}
}

// The revert rule: breaks callers, has dependents, and the coverage check RAN and
// found nothing.
func TestRevertWhenCoverageRanAndFoundNothing(t *testing.T) {
	input := riskyInput()
	report := decide(t, input, []CheckResult{Risk(input), ranCheck(CheckCoverage)})

	if report.Verdict != VerdictRevert {
		t.Fatalf("verdict = %s, want revert (reasons: %v)", report.Verdict, report.Reasons)
	}
	if report.ExitCode != 2 {
		t.Errorf("exit code = %d, want 2", report.ExitCode)
	}
}

// The degradation rule, and the single most important test in this package: the
// SAME risky change must not read as revert when the coverage check could not
// run. Nothing has a covering test in that situation, so an uncapped verdict
// would accuse every risky change of being unverified because of a missing input.
func TestCoverageNotRunCapsRevertAtContinue(t *testing.T) {
	input := riskyInput()
	input.Unavailable[CheckCoverage] = "resolver unavailable"

	report := decide(t, input, RunChecks(input))

	if report.Verdict != VerdictContinue {
		t.Fatalf("verdict = %s, want continue (reasons: %v)", report.Verdict, report.Reasons)
	}
	if !strings.Contains(strings.Join(report.Reasons, "\n"), "capped at continue") {
		t.Errorf("report does not say the verdict was capped: %v", report.Reasons)
	}
	// The row must still be present and still first: a missing coverage check
	// changes the verdict, never the reading order.
	if len(report.Review) == 0 || report.Review[0].Change.Name != "VerifyToken" {
		t.Fatalf("review order lost the risky entity: %+v", report.Review)
	}
	if report.Review[0].Coverage != CoverageUnknown {
		t.Errorf("coverage = %s, want unknown - 'no test exists' and 'we could not look' are different claims",
			report.Review[0].Coverage)
	}
}

func TestRiskAndCoverageBothMissingIsUnusable(t *testing.T) {
	input := riskyInput()
	input.Unavailable[CheckRisk] = "analysis failed"
	input.Unavailable[CheckCoverage] = "resolver unavailable"

	report := decide(t, input, RunChecks(input))

	if report.Verdict != VerdictUnusable {
		t.Fatalf("verdict = %s, want unusable", report.Verdict)
	}
	if report.ExitCode != 5 {
		t.Errorf("exit code = %d, want 5", report.ExitCode)
	}
}

// A check that only ever accuses must not cap anything when it goes missing:
// losing it removes a reason to worry, it never invents a reason to relax.
func TestAccusingChecksDoNotCapTheVerdict(t *testing.T) {
	input := riskyInput()
	input.Unavailable[CheckCompanions] = "no git history"
	input.Unavailable[CheckClones] = "no similarity relations"

	report := decide(t, input, []CheckResult{
		Risk(input), ranCheck(CheckCoverage), Companions(input), Clones(input),
	})

	if report.Verdict != VerdictRevert {
		t.Fatalf("verdict = %s, want revert - a missing companion check must not soften a revert", report.Verdict)
	}
}

func TestCoveredRiskyChangeIsNotAReverted(t *testing.T) {
	input := riskyInput()
	ref := input.Changes[0].EntityRef
	input.Tests[ref.Key()] = []Evidence{
		{Kind: EvidenceTest, Path: "internal/auth/token_test.go", Line: 12, Note: "TestVerifyToken"},
	}

	report := decide(t, input, []CheckResult{Risk(input), ranCheck(CheckCoverage)})

	if report.Verdict != VerdictContinue {
		t.Fatalf("verdict = %s, want continue", report.Verdict)
	}
	if report.Review[0].Coverage != CoverageCovered {
		t.Errorf("coverage = %s, want covered", report.Review[0].Coverage)
	}
}

func TestNothingRiskyIsKeep(t *testing.T) {
	input := riskyInput()
	input.Changes = []Change{{
		EntityRef: EntityRef{Name: "helper", Kind: "function", Path: "internal/util/helper.go", Line: 3},
		Change:    ChangeBodyChanged,
	}}
	input.Callers = map[string][]Evidence{}

	report := decide(t, input, []CheckResult{Risk(input), ranCheck(CheckCoverage)})

	if report.Verdict != VerdictKeep {
		t.Fatalf("verdict = %s, want keep (reasons: %v)", report.Verdict, report.Reasons)
	}
	if report.QuietCount != 1 {
		t.Errorf("quiet count = %d, want 1", report.QuietCount)
	}
	if len(report.Review) != 0 {
		t.Errorf("review order should be empty, got %+v", report.Review)
	}
}

func TestEmptyRangeIsKeep(t *testing.T) {
	input := riskyInput()
	input.Changes = nil

	report := decide(t, input, []CheckResult{Risk(input), ranCheck(CheckCoverage)})

	if report.Verdict != VerdictKeep {
		t.Fatalf("verdict = %s, want keep", report.Verdict)
	}
}

// Findings from a check that reported itself as not-run must be dropped, not
// trusted: half an answer read as a whole one is how a gate starts lying.
func TestNotRunCheckFindingsAreIgnored(t *testing.T) {
	input := riskyInput()
	ref := input.Changes[0].EntityRef
	partial := CheckResult{
		ID:  CheckCompanions,
		Ran: false,
		Findings: []Finding{
			{Check: CheckCompanions, Subject: ref, Summary: "half an answer", Floor: VerdictRevert},
		},
	}

	report := decide(t, input, []CheckResult{Risk(input), ranCheck(CheckCoverage), partial})

	for _, item := range report.Review {
		for _, finding := range item.Findings {
			if finding.Check == CheckCompanions {
				t.Fatalf("a not-run check contributed a finding: %+v", finding)
			}
		}
	}
}

// The claim the whole product rests on: run it twice, get the same bytes. Go
// randomises map iteration, so a renderer that walks a map unsorted fails here.
func TestRenderIsByteIdenticalAcrossRuns(t *testing.T) {
	first := renderSample(t)
	for run := 0; run < 20; run++ {
		if got := renderSample(t); got != first {
			t.Fatalf("render differed on run %d\n--- first ---\n%s\n--- got ---\n%s", run, first, got)
		}
	}
}

func renderSample(t *testing.T) string {
	t.Helper()
	report := Evaluate(SampleInput())
	buffer := &strings.Builder{}
	if err := RenderText(buffer, report, RenderOptions{}); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buffer.String()
}

func TestJSONIsByteIdenticalAcrossRuns(t *testing.T) {
	render := func() string {
		report := Evaluate(SampleInput())
		buffer := &strings.Builder{}
		if err := RenderJSON(buffer, report); err != nil {
			t.Fatalf("render json: %v", err)
		}
		return buffer.String()
	}
	first := render()
	for run := 0; run < 20; run++ {
		if got := render(); got != first {
			t.Fatalf("json differed on run %d", run)
		}
	}
}

// The review order is the real product: the riskiest unverified row must lead.
func TestReviewOrderPutsTheBlockingRowFirst(t *testing.T) {
	report := Evaluate(SampleInput())

	if len(report.Review) < 4 {
		t.Fatalf("expected at least 4 rows, got %d", len(report.Review))
	}
	if report.Review[0].Change.Name != "VerifyToken" {
		t.Errorf("first row = %s, want VerifyToken", report.Review[0].Change.Name)
	}
	wantOrder := []string{"VerifyToken", "parseFlags", "Config.Load", "handleList"}
	for index, want := range wantOrder {
		if got := report.Review[index].Change.Name; got != want {
			t.Errorf("row %d = %s, want %s", index+1, got, want)
		}
	}
	if report.QuietCount != 43 {
		t.Errorf("quiet count = %d, want 43", report.QuietCount)
	}
}

// The report must never be a black box: the rules it judged by are in its output.
func TestReportPrintsItsOwnRulesAndCheckStatus(t *testing.T) {
	text := renderSample(t)

	for _, want := range []string{"RULES", "CHECKS", "NOT RUN", "exit code"} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered report is missing %q:\n%s", want, text)
		}
	}
	// A stubbed check must appear as not-run rather than vanish.
	for _, id := range []CheckID{CheckCoverage, CheckCompanions, CheckClones} {
		if !strings.Contains(text, string(id)) {
			t.Errorf("rendered report never mentions the %s check", id)
		}
	}
}

// A row whose dependent count has no citations yet must hand the reader the
// command that expands it, rather than asking for trust.
func TestUncitedRiskRowOffersAVerifyCommand(t *testing.T) {
	input := riskyInput()
	input.Callers = map[string][]Evidence{}

	report := decide(t, input, RunChecks(input))
	buffer := &strings.Builder{}
	if err := RenderText(buffer, report, RenderOptions{}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buffer.String(), "entire graph impact --symbol VerifyToken") {
		t.Errorf("no verify affordance on an uncited risk row:\n%s", buffer.String())
	}
}

func TestCochangeRate(t *testing.T) {
	if got := (Cochange{Together: 14, Observed: 15}).Rate(); got < 0.93 || got > 0.94 {
		t.Errorf("rate = %f, want ~0.933", got)
	}
	if got := (Cochange{}).Rate(); got != 0 {
		t.Errorf("rate with no observations = %f, want 0", got)
	}
}

// Unknown coverage must never be worded as "no covering test": the two states
// are different claims, and only one of them is something Gate observed.
func TestUnknownCoverageIsNotWordedAsAMissingTest(t *testing.T) {
	input := riskyInput()
	input.Unavailable[CheckCoverage] = "resolver unavailable"

	report := decide(t, input, RunChecks(input))
	reasons := strings.Join(report.Reasons, "\n")

	if strings.Contains(reasons, "no covering test") {
		t.Errorf("unknown coverage was reported as a missing test:\n%s", reasons)
	}
	if !strings.Contains(reasons, "coverage was not checked") {
		t.Errorf("unknown coverage was not stated as unchecked:\n%s", reasons)
	}
}
