// Package gate decides whether a range of changes is safe to keep.
//
// The package is deliberately PURE: it takes evidence in, and returns a verdict
// and a review order out. It never runs git, never parses source, and never
// touches the tree-sitter build. Everything that talks to the outside world
// lives in internal/cli/gate.go and hands its results here as an Input.
//
// That split is what lets the decision logic be written and tested on a machine
// where the CGO grammar build has not finished — or has never worked — and it is
// why every test in this package builds its Input by hand.
//
// Data flows ONE WAY:
//
//	collect -> Input -> checks -> []CheckResult -> Build -> Report -> Decide -> Render
//
// Nothing calls backwards. A check may not read another check's findings; when a
// rule genuinely needs two checks at once (the revert rule needs risk AND
// coverage) that rule lives in Decide, which sees the merged Report.
package gate

import (
	"fmt"
	"sort"
)

// Verdict is the answer Gate exists to give.
type Verdict string

const (
	// VerdictKeep means nothing changed here is risky, or what is risky is covered.
	VerdictKeep Verdict = "keep"
	// VerdictContinue means a human should read something before this merges.
	VerdictContinue Verdict = "continue"
	// VerdictRevert means something with dependents was removed or had its
	// signature changed, and nothing verifies it.
	VerdictRevert Verdict = "revert"
	// VerdictUnusable means Gate ran but could not get enough evidence to judge.
	// It is NOT a failing verdict about the code: it is Gate declining to answer,
	// and the report that accompanies it must not be built on.
	VerdictUnusable Verdict = "unusable"
)

// ExitCode is the process exit status for a verdict, so CI can enforce a gate
// with nobody watching. These numbers are part of the command's contract and
// must not be renumbered.
func (v Verdict) ExitCode() int {
	switch v {
	case VerdictKeep:
		return 0
	case VerdictContinue:
		return 1
	case VerdictRevert:
		return 2
	case VerdictUnusable:
		return 5
	default:
		// An unset verdict is a programming error in Gate itself, not a judgement
		// about the repository. Report it as unusable rather than as keep: the one
		// thing a gate must never do is wave something through by accident.
		return 5
	}
}

// severity orders the three judging verdicts so a report can take the worst one.
// VerdictUnusable is deliberately absent — it is not "worse than revert", it is a
// different axis (no evidence vs. bad evidence) and Decide handles it separately.
func (v Verdict) severity() int {
	switch v {
	case VerdictKeep:
		return 0
	case VerdictContinue:
		return 1
	case VerdictRevert:
		return 2
	default:
		return -1
	}
}

// worseOf returns whichever of two verdicts a reviewer should act on.
func worseOf(a, b Verdict) Verdict {
	if b.severity() > a.severity() {
		return b
	}
	return a
}

// ChangeKind mirrors the vocabulary internal/sem already emits for an entity
// change ("added", "removed", "renamed", "moved", "signature_changed",
// "body_changed") so that collect is a straight field copy rather than a
// translation table that can drift.
type ChangeKind string

const (
	ChangeAdded            ChangeKind = "added"
	ChangeRemoved          ChangeKind = "removed"
	ChangeRenamed          ChangeKind = "renamed"
	ChangeMoved            ChangeKind = "moved"
	ChangeSignatureChanged ChangeKind = "signature_changed"
	ChangeBodyChanged      ChangeKind = "body_changed"
)

// BreaksCallers reports whether this kind of change can break code that was
// written before the change existed. It is the distinction the revert rule turns
// on: a body change can be wrong, but a removal or a signature change is wrong
// for everyone who called it.
func (c ChangeKind) BreaksCallers() bool {
	switch c {
	case ChangeRemoved, ChangeRenamed, ChangeMoved, ChangeSignatureChanged:
		return true
	default:
		return false
	}
}

// EntityRef locates one entity. Path is repo-relative and slash-separated on
// every platform, because it is compared, sorted and printed, and a backslash on
// Windows would make the same repository sort two different ways.
type EntityRef struct {
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// Key is a stable identity for map lookups and for the tie-break in sorting. It
// is not printed and is not a compound-v1 symbol ID; it only has to be unique
// within one Gate run.
func (r EntityRef) Key() string {
	return r.Path + "\x00" + r.Name + "\x00" + r.Kind
}

// Location is the clickable "file:line" a reviewer opens.
func (r EntityRef) Location() string {
	if r.Line <= 0 {
		return r.Path
	}
	return fmt.Sprintf("%s:%d", r.Path, r.Line)
}

// Change is one entity the range touched, with the dependent count the graph
// already computed for it.
type Change struct {
	EntityRef
	Change ChangeKind `json:"change"`
	// Dependents is the graph's heuristic count of things that would notice this
	// entity changing. It is evidence, not a proof: reflection, dynamic dispatch
	// and generated code are invisible to it.
	Dependents   int    `json:"dependents"`
	OldSignature string `json:"old_signature,omitempty"`
	NewSignature string `json:"new_signature,omitempty"`
}

// EvidenceKind says what KIND of thing is being cited, so the renderer can label
// it and a reader can tell a caller from a test from a git observation.
type EvidenceKind string

const (
	// EvidenceCaller is a call site that exists in code the change did not write.
	EvidenceCaller EvidenceKind = "caller"
	// EvidenceTypeUser is a site that names the changed type.
	EvidenceTypeUser EvidenceKind = "type_user"
	// EvidenceTest is a test the coverage check believes verifies the entity.
	EvidenceTest EvidenceKind = "test"
	// EvidenceCochange is a git observation: files that normally change together.
	EvidenceCochange EvidenceKind = "cochange"
	// EvidenceClone is a near-duplicate body that did not receive the same edit.
	EvidenceClone EvidenceKind = "clone"
)

// Evidence is one citation a reviewer can open and check for themselves. Every
// finding Gate prints must carry at least one, because a finding a human cannot
// verify is indistinguishable from a finding Gate made up.
type Evidence struct {
	Kind EvidenceKind `json:"kind"`
	Path string       `json:"path"`
	Line int          `json:"line,omitempty"`
	Note string       `json:"note,omitempty"`
}

// Location is the clickable "file:line" for this citation.
func (e Evidence) Location() string {
	if e.Line <= 0 {
		return e.Path
	}
	return fmt.Sprintf("%s:%d", e.Path, e.Line)
}

// SortEvidence puts a slice of citations into a total order. Called before any
// rendering: Go randomises map iteration, and a renderer that walks a map
// unsorted makes two runs of the same commit print different bytes, which breaks
// the one property the whole product rests on.
func SortEvidence(items []Evidence) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Note < right.Note
	})
}

// CheckID names one of the four checks.
type CheckID string

const (
	// CheckRisk asks: what breaks if this changed?
	CheckRisk CheckID = "risk"
	// CheckCoverage asks: did anything verify this?
	CheckCoverage CheckID = "coverage"
	// CheckCompanions asks: what normally changes with this, but did not?
	CheckCompanions CheckID = "companions"
	// CheckClones asks: are there copies that did not get the fix?
	CheckClones CheckID = "clones"
)

// CheckOrder is the order checks are reported in. Fixed, not derived from a map.
var CheckOrder = []CheckID{CheckRisk, CheckCoverage, CheckCompanions, CheckClones}

// Finding is one thing a check noticed, about one entity, with its citations.
type Finding struct {
	Check   CheckID   `json:"check"`
	Subject EntityRef `json:"subject"`
	// Summary is the single line the renderer prints. It must read as a fact
	// about the repository, not as advice.
	Summary  string     `json:"summary"`
	Evidence []Evidence `json:"evidence,omitempty"`
	// Floor is the least-severe verdict this finding on its own permits. A check
	// that only wants to inform a reviewer leaves it at VerdictKeep; a companion
	// gap sets VerdictContinue. The revert rule is NOT expressible here because it
	// needs two checks at once, so it lives in Decide.
	Floor Verdict `json:"floor,omitempty"`
}

// CheckResult is a check's whole contribution, INCLUDING whether it ran.
//
// Ran is the field that makes the degradation rule possible. A check that could
// not run must never count against the change: if the coverage check fails, then
// nothing has a covering test, and every risky change would read as a revert.
// That is not strictness, it is a false accusation caused by a missing input.
type CheckResult struct {
	ID CheckID `json:"id"`
	// Ran is false when the check could not produce evidence. Its findings are
	// then ignored and Cap applies instead.
	Ran bool `json:"ran"`
	// Reason explains, in one line, why a check did not run. Printed verbatim, so
	// the report says "not run: <reason>" rather than silently omitting the check.
	Reason   string    `json:"reason,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
	// Cap is the WORST verdict Gate may reach while this check is missing. A
	// check that did not run can only ever pull the verdict toward keep, never
	// push it toward revert.
	Cap Verdict `json:"cap,omitempty"`
}

// NotRun builds the result of a check that could not run, with the verdict
// ceiling it imposes.
func NotRun(id CheckID, reason string, ceiling Verdict) CheckResult {
	return CheckResult{ID: id, Ran: false, Reason: reason, Cap: ceiling}
}

// CoverageState is what the coverage check concluded about one entity.
type CoverageState string

const (
	// CoverageCovered means a test was resolved for this entity.
	CoverageCovered CoverageState = "covered"
	// CoverageUnchecked means the coverage check ran and found nothing.
	CoverageUnchecked CoverageState = "unchecked"
	// CoverageUnknown means the coverage check did not run. It is a distinct
	// state from unchecked ON PURPOSE: "no test exists" and "we could not look"
	// must never render, or judge, the same way.
	CoverageUnknown CoverageState = "unknown"
)

// ReviewItem is one entity as the reviewer sees it: the change, what verifies
// it, and everything the checks said about it, merged.
type ReviewItem struct {
	Change           Change        `json:"change"`
	Coverage         CoverageState `json:"coverage"`
	CoverageEvidence []Evidence    `json:"coverage_evidence,omitempty"`
	Findings         []Finding     `json:"findings,omitempty"`
}

// Quiet reports whether this entity is one a reviewer can skip: nothing depends
// on it and no check said anything about it.
func (item ReviewItem) Quiet() bool {
	return item.Change.Dependents == 0 && len(item.Findings) == 0
}

// Input is everything collect gathered from the outside world. Checks read it;
// nothing else does.
//
// The lookup maps are keyed by EntityRef.Key(), except Cochanges which is keyed
// by file path. Reading a map is fine — it is ITERATING one that destroys
// determinism — so any check that ranges over these must sort before returning.
type Input struct {
	Repo string `json:"repo"`
	Base string `json:"base"`
	Head string `json:"head"`

	// Changes is the entity-level change list for the range, already carrying the
	// graph's dependent counts.
	Changes []Change `json:"changes"`

	// Callers maps an entity to the call sites and type uses that reach it, drawn
	// from the committed graph — code written before this change existed.
	Callers map[string][]Evidence `json:"-"`

	// Tests maps an entity to the tests the resolver believes verify it.
	Tests map[string][]Evidence `json:"-"`

	// Cochanges maps a changed file path to the files git says normally change
	// with it, with how often.
	Cochanges map[string][]Cochange `json:"-"`

	// Clones maps an entity to its near-duplicate bodies elsewhere in the repo.
	Clones map[string][]EntityRef `json:"-"`

	// ChangedFiles is the set of files the range touched, so the companion check
	// can ask what is MISSING from it.
	ChangedFiles map[string]bool `json:"-"`

	// Unavailable records checks collect could not gather evidence for, and why.
	// Populated by collect, consumed by the check functions, which turn each entry
	// into a NotRun CheckResult rather than an empty finding list. The difference
	// matters: an empty finding list means "looked, found nothing".
	Unavailable map[CheckID]string `json:"-"`
}

// Cochange is one git observation: how often another file changed in the same
// commit as the subject file, out of how many commits observed.
type Cochange struct {
	Path     string `json:"path"`
	Together int    `json:"together"`
	Observed int    `json:"observed"`
}

// Rate is how often the two files moved together, in [0,1].
func (c Cochange) Rate() float64 {
	if c.Observed <= 0 {
		return 0
	}
	return float64(c.Together) / float64(c.Observed)
}

// Report is the finished decision: what was judged, what a human should read,
// and why the verdict is what it is.
type Report struct {
	Repo string `json:"repo,omitempty"`
	Base string `json:"base"`
	Head string `json:"head"`

	// Verdict and its exit code are the enforceable half of the product.
	Verdict  Verdict `json:"verdict"`
	ExitCode int     `json:"exit_code"`

	// Reasons state, in the report itself, WHY the verdict is what it is, so the
	// tool is never a black box.
	Reasons []string `json:"reasons,omitempty"`

	// TotalEntities is every entity the range touched, including the quiet ones
	// left out of Review.
	TotalEntities int `json:"total_entities"`
	// Review is the ranked read-this-first list. Sorted; never map-ordered.
	Review []ReviewItem `json:"review"`
	// QuietCount is how many entities had nothing to say, summarised as a count
	// rather than printed. The full list is available behind --all.
	QuietCount int `json:"quiet_count"`

	// Checks reports every check and whether it ran, in CheckOrder.
	Checks []CheckResult `json:"checks"`
}

// Ran reports whether a named check contributed evidence to this report.
func (r Report) Ran(id CheckID) bool {
	for _, check := range r.Checks {
		if check.ID == id {
			return check.Ran
		}
	}
	return false
}
