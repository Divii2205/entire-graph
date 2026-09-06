package gate

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/entireio/entire-graph/internal/termsafe"
)

// RenderOptions control how much of a report is printed. They never change what
// the verdict is — only how much of the evidence behind it is shown.
type RenderOptions struct {
	// All prints every entity, including the quiet ones that are summarised as a
	// count by default.
	All bool
	// MaxEvidence caps the citations printed per row. Zero means the default; a
	// negative value means unbounded.
	MaxEvidence int
	// MaxRows caps the review order. Zero means the default; negative unbounded.
	MaxRows int
}

const (
	defaultMaxEvidence = 3
	defaultMaxRows     = 10
)

// RenderText writes the human-facing report.
//
// Every string that came from the repository — a path, a symbol name, a note —
// goes through termsafe.Line before it is printed. These values carry bytes the
// repository chose: a Git pathname may hold any byte but NUL and '/', so an ESC
// or a C1 CSI in a scanned repository would otherwise be obeyed by the reader's
// terminal rather than displayed. Gate prints a verdict a person acts on, which
// makes forging its output worth someone's time.
func RenderText(out io.Writer, report Report, options RenderOptions) error {
	buffer := &strings.Builder{}

	fmt.Fprintf(buffer, "Gate verdict: %s          (%d entit%s changed)\n",
		strings.ToUpper(string(report.Verdict)), report.TotalEntities, pluralY(report.TotalEntities))
	fmt.Fprintf(buffer, "range: %s..%s\n", termsafe.Line(report.Base), termsafe.Line(report.Head))
	buffer.WriteString("\n")

	renderReview(buffer, report, options)
	renderReasons(buffer, report)
	renderChecks(buffer, report)
	renderRules(buffer)

	fmt.Fprintf(buffer, "\nexit code %d\n", report.ExitCode)

	_, err := io.WriteString(out, buffer.String())
	return err
}

func renderReview(buffer *strings.Builder, report Report, options RenderOptions) {
	rows := report.Review
	if !options.All {
		if limit := rowLimit(options); limit >= 0 && len(rows) > limit {
			rows = rows[:limit]
		}
	}

	if len(rows) == 0 {
		buffer.WriteString("REVIEW ORDER - nothing to read first\n")
		fmt.Fprintf(buffer, " %d entit%s changed, none of them with a dependent or a finding.\n\n",
			report.TotalEntities, pluralY(report.TotalEntities))
		return
	}

	fmt.Fprintf(buffer, "REVIEW ORDER - read these %d first\n", len(rows))
	for index, item := range rows {
		renderRow(buffer, index+1, item, options)
	}

	// Say what was left out, and how to see it. A reviewer who is told 43 entities
	// were quiet can decide to trust that; one who is shown 4 rows and no count
	// cannot tell whether the other 43 exist.
	hidden := len(report.Review) - len(rows)
	switch {
	case hidden > 0 && report.QuietCount > 0:
		fmt.Fprintf(buffer, "\n %d more with findings and %d with 0 dependents and no findings.  Full list: --all\n",
			hidden, report.QuietCount)
	case hidden > 0:
		fmt.Fprintf(buffer, "\n %d more with findings.  Full list: --all\n", hidden)
	case report.QuietCount > 0:
		fmt.Fprintf(buffer, "\n The other %d entit%s have 0 dependents and no findings.  Full list: --all\n",
			report.QuietCount, pluralY(report.QuietCount))
	}
	buffer.WriteString("\n")
}

func renderRow(buffer *strings.Builder, position int, item ReviewItem, options RenderOptions) {
	change := item.Change
	fmt.Fprintf(buffer, "%2d. %s  %s  %s\n",
		position, termsafe.Line(change.Name), termsafe.Line(change.Kind), termsafe.Line(change.Location()))

	facts := []string{string(change.Change)}
	if change.Dependents > 0 {
		facts = append(facts, fmt.Sprintf("%d dependent%s", change.Dependents, plural(change.Dependents)))
	}
	facts = append(facts, coverageLabel(item))
	fmt.Fprintf(buffer, "    %s\n", strings.Join(facts, " · "))

	for _, finding := range item.Findings {
		if finding.Check == CheckRisk {
			// The risk line is already the facts line above it; printing it again
			// would double every row for no new information.
			continue
		}
		fmt.Fprintf(buffer, "    %s\n", termsafe.Line(finding.Summary))
	}

	renderEvidence(buffer, item, options)
}

// renderEvidence prints the citations a reviewer opens to check the row. This is
// the part that makes a finding falsifiable rather than an assertion.
func renderEvidence(buffer *strings.Builder, item ReviewItem, options RenderOptions) {
	citations := append([]Evidence(nil), item.CoverageEvidence...)
	for _, finding := range item.Findings {
		citations = append(citations, finding.Evidence...)
	}
	SortEvidence(citations)

	limit := evidenceLimit(options)
	shown := citations
	if !options.All && limit >= 0 && len(shown) > limit {
		shown = shown[:limit]
	}
	for _, citation := range shown {
		note := ""
		if citation.Note != "" {
			note = "  " + termsafe.Line(citation.Note)
		}
		fmt.Fprintf(buffer, "      %-9s %s%s\n", citation.Kind, termsafe.Line(citation.Location()), note)
	}
	if hidden := len(citations) - len(shown); hidden > 0 {
		fmt.Fprintf(buffer, "      %-9s +%d more\n", "", hidden)
	}

	// A row with a dependent count but no citations is a claim the reader cannot
	// yet open. Hand them the command that expands it rather than asking them to
	// take the number on trust — a finding nobody can check is indistinguishable
	// from one Gate made up.
	if len(citations) == 0 && item.Change.Dependents > 0 {
		fmt.Fprintf(buffer, "      %-9s entire graph impact --symbol %s --file %s\n",
			"verify", termsafe.Line(item.Change.Name), termsafe.Line(item.Change.Path))
	}
}

func renderReasons(buffer *strings.Builder, report Report) {
	if len(report.Reasons) == 0 {
		return
	}
	buffer.WriteString("WHY\n")
	for _, reason := range report.Reasons {
		fmt.Fprintf(buffer, " - %s\n", termsafe.Line(reason))
	}
	buffer.WriteString("\n")
}

// renderChecks prints every check and whether it ran. A check that did not run is
// reported as not-run, never as a failure and never by omission: a reader has to
// be able to see that a verdict was reached with an input missing.
func renderChecks(buffer *strings.Builder, report Report) {
	buffer.WriteString("CHECKS\n")
	for _, check := range report.Checks {
		if check.Ran {
			fmt.Fprintf(buffer, " %-11s ran (%d finding%s)\n",
				check.ID, len(check.Findings), plural(len(check.Findings)))
			continue
		}
		fmt.Fprintf(buffer, " %-11s NOT RUN: %s\n", check.ID, termsafe.Line(notRunReason(check)))
	}
	buffer.WriteString("\n")
}

func renderRules(buffer *strings.Builder) {
	buffer.WriteString("RULES\n")
	for _, rule := range VerdictRules() {
		if rule == "" {
			buffer.WriteString("\n")
			continue
		}
		fmt.Fprintf(buffer, " %s\n", rule)
	}
}

// RenderJSON writes the report for a machine — a reviewing agent, or CI. The
// encoder is wrapped so a repository-chosen string cannot escape the JSON sink
// any more than it can escape the text one.
func RenderJSON(out io.Writer, report Report) error {
	encoder := json.NewEncoder(termsafe.NewJSONWriter(out))
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func coverageLabel(item ReviewItem) string {
	switch item.Coverage {
	case CoverageCovered:
		if len(item.CoverageEvidence) > 0 {
			return "verified (" + termsafe.Line(item.CoverageEvidence[0].Location()) + ")"
		}
		return "verified"
	case CoverageUnchecked:
		return "UNCHECKED"
	default:
		// Not "unchecked". Gate did not look, and saying otherwise would be an
		// accusation it has no evidence for.
		return "coverage unknown"
	}
}

func rowLimit(options RenderOptions) int {
	if options.MaxRows == 0 {
		return defaultMaxRows
	}
	return options.MaxRows
}

func evidenceLimit(options RenderOptions) int {
	if options.MaxEvidence == 0 {
		return defaultMaxEvidence
	}
	return options.MaxEvidence
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
