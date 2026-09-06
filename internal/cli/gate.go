package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/entireio/entire-graph/internal/gate"
	"github.com/entireio/entire-graph/internal/gitutil"
	"github.com/entireio/entire-graph/internal/sem"
)

// This file is the ONLY part of Gate that talks to the outside world. It resolves
// revisions, runs the analysis, and hands the results to internal/gate as a plain
// Input. Everything downstream of collect is pure and needs no C compiler, which
// is what lets the decision logic be built and tested while this half is still
// unproven on a given machine.

type gateFlags struct {
	repo string
	base string
	head string
	json bool
	all  bool
	// sample drives the whole pipeline from the built-in fixture instead of from
	// git. It is how the report can be demonstrated, and how the render and
	// verdict layers are exercised, on a machine where the grammar build has not
	// finished.
	sample bool
	limit  int
}

func runGate(ctx context.Context, opts Options, args []string) error {
	flags, unknown, err := parseGateFlags(args)
	if err != nil {
		return err
	}
	if len(unknown) != 0 {
		return unexpectedArgumentsError("gate", opts.Version, unknown)
	}

	var input gate.Input
	if flags.sample {
		input = gate.SampleInput()
	} else {
		repo, err := resolveRepo(ctx, opts.Env, flags.repo)
		if err != nil {
			return err
		}
		input, err = collect(ctx, repo, flags)
		if err != nil {
			return err
		}
	}

	report := gate.Evaluate(input)

	if flags.json {
		if err := gate.RenderJSON(opts.Stdout, report); err != nil {
			return err
		}
	} else {
		options := gate.RenderOptions{All: flags.all, MaxRows: flags.limit}
		if err := gate.RenderText(opts.Stdout, report, options); err != nil {
			return err
		}
	}

	// The verdict IS the exit status. The report is already on stdout, so this
	// carries the number out without printing anything further.
	if code := report.ExitCode; code != 0 {
		return NewExitCode(code)
	}
	return nil
}

// collect gathers the evidence. It is the seam between the outside world and the
// pure package, and the only place that may fail for environmental reasons.
//
// PHASE 0: only the entity-level change list is real. The other three evidence
// sources are marked unavailable with the reason a reader will see printed, so
// the degradation rule engages honestly rather than the checks silently reporting
// "found nothing". Filling them in is Phase 1 and after:
//
//	Callers   <- the committed graph (sem.ProviderSnapshot relations)
//	Tests     <- file-mirror test resolution
//	Cochanges <- gitutil.FileCochanges
//	Clones    <- the graph's SIMILAR_TO relations
func collect(ctx context.Context, repo string, flags gateFlags) (gate.Input, error) {
	// Resolve the revisions FIRST and separately. A typo in a ref is the user's
	// mistake and deserves a plain error; a failure once the analysis is running
	// is missing evidence, which is a verdict of its own.
	base, err := gitutil.RevParse(ctx, repo, flags.base)
	if err != nil {
		return gate.Input{}, fmt.Errorf("gate --base %s: %w", flags.base, err)
	}
	head, err := gitutil.RevParse(ctx, repo, flags.head)
	if err != nil {
		return gate.Input{}, fmt.Errorf("gate --head %s: %w", flags.head, err)
	}

	input := gate.Input{
		Repo:         repo,
		Base:         flags.base,
		Head:         flags.head,
		Callers:      map[string][]gate.Evidence{},
		Tests:        map[string][]gate.Evidence{},
		Cochanges:    map[string][]gate.Cochange{},
		Clones:       map[string][]gate.EntityRef{},
		ChangedFiles: map[string]bool{},
		Unavailable: map[gate.CheckID]string{
			gate.CheckCoverage:   "test resolution is not collected yet",
			gate.CheckCompanions: "co-change history is not collected yet",
			gate.CheckClones:     "near-duplicate relations are not collected yet",
		},
	}

	result, err := sem.AnalyzeGitRange(ctx, repo, base, head, nil)
	if err != nil {
		// Not a fatal error: Gate has a verdict for exactly this situation. The
		// report still prints, saying which check could not run and why, and the
		// verdict degrades rather than guessing.
		input.Unavailable[gate.CheckRisk] = "the change analysis failed: " + err.Error()
		return input, nil
	}

	for _, file := range result.Files {
		path := file.Path
		if path == "" {
			path = file.OldPath
		}
		input.ChangedFiles[path] = true
		for _, change := range file.Changes {
			input.Changes = append(input.Changes, gate.Change{
				EntityRef: gate.EntityRef{
					Name: change.Name,
					Kind: change.Kind,
					Path: changePath(file, change),
					Line: changeLine(change),
				},
				Change:       gate.ChangeKind(change.Type),
				Dependents:   change.DependentsCount,
				OldSignature: change.OldSignature,
				NewSignature: change.NewSignature,
			})
		}
	}

	return input, nil
}

// changePath prefers where the entity ENDED UP, so a reviewer opens the file that
// exists now. A removal has no after-path and falls back to where it was.
func changePath(file sem.FileChange, change sem.EntityChange) string {
	if change.NewPath != "" {
		return change.NewPath
	}
	if file.Path != "" {
		return file.Path
	}
	if change.OldPath != "" {
		return change.OldPath
	}
	return file.OldPath
}

// changeLine prefers the after-line for the same reason, falling back to the
// before-line for something that is no longer there.
func changeLine(change sem.EntityChange) int {
	if change.AfterStartLine > 0 {
		return change.AfterStartLine
	}
	return change.BeforeStartLine
}

func parseGateFlags(args []string) (gateFlags, []string, error) {
	flags := gateFlags{base: "HEAD~1", head: "HEAD"}
	var unknown []string

	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--repo":
			value, next, err := gateFlagValue(args, index, "--repo")
			if err != nil {
				return flags, nil, err
			}
			flags.repo, index = value, next
		case "--base":
			value, next, err := gateFlagValue(args, index, "--base")
			if err != nil {
				return flags, nil, err
			}
			flags.base, index = value, next
		case "--head":
			value, next, err := gateFlagValue(args, index, "--head")
			if err != nil {
				return flags, nil, err
			}
			flags.head, index = value, next
		case "--limit":
			value, next, err := gateFlagValue(args, index, "--limit")
			if err != nil {
				return flags, nil, err
			}
			parsed, convErr := strconv.Atoi(value)
			if convErr != nil || parsed < 0 {
				return flags, nil, errors.New("gate --limit needs a non-negative whole number")
			}
			// Zero would select the renderer's default rather than "show none", so
			// an explicit 0 becomes unbounded, which is what a caller asking for no
			// limit means.
			if parsed == 0 {
				parsed = -1
			}
			flags.limit, index = parsed, next
		case "--json":
			flags.json = true
		case "--all":
			flags.all = true
		case "--sample":
			flags.sample = true
		default:
			unknown = append(unknown, args[index])
		}
	}
	return flags, unknown, nil
}

func gateFlagValue(args []string, index int, name string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("gate %s needs a value", name)
	}
	return args[index+1], index + 1, nil
}
