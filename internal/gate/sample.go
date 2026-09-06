package gate

import "fmt"

// SampleInput is the hand-made evidence set the rest of Gate is built against.
//
// It exists so that no lane waits for the tree-sitter build. The real collector
// in internal/cli/gate.go fills exactly these fields from sem.AnalyzeGitRange,
// the committed graph, and gitutil.FileCochanges; until it does, `gate --sample`
// drives the whole pipeline end to end, and every test in this package builds its
// Input the same way.
//
// The scenario is deliberately the one from the plan: forty-seven entities of
// which four are worth reading, one of them a signature change with fourteen
// dependents and nothing verifying it. It populates the coverage, companion and
// clone inputs even though those checks are still stubs — the day a stub becomes
// real, its output appears here with no fixture work.
func SampleInput() Input {
	input := Input{
		Repo:         ".",
		Base:         "main",
		Head:         "HEAD",
		Callers:      map[string][]Evidence{},
		Tests:        map[string][]Evidence{},
		Cochanges:    map[string][]Cochange{},
		Clones:       map[string][]EntityRef{},
		ChangedFiles: map[string]bool{},
		Unavailable:  map[CheckID]string{},
	}

	verifyToken := EntityRef{Name: "VerifyToken", Kind: "function", Path: "internal/auth/token.go", Line: 88}
	parseFlags := EntityRef{Name: "parseFlags", Kind: "function", Path: "internal/cli/root.go", Line: 729}
	configLoad := EntityRef{Name: "Config.Load", Kind: "method", Path: "internal/config/load.go", Line: 31}
	handleList := EntityRef{Name: "handleList", Kind: "function", Path: "internal/api/list.go", Line: 44}

	input.Changes = []Change{
		{
			EntityRef:    verifyToken,
			Change:       ChangeSignatureChanged,
			Dependents:   14,
			OldSignature: "func VerifyToken(raw string) (Claims, error)",
			NewSignature: "func VerifyToken(ctx context.Context, raw string) (Claims, error)",
		},
		{EntityRef: parseFlags, Change: ChangeBodyChanged, Dependents: 9},
		{EntityRef: configLoad, Change: ChangeBodyChanged, Dependents: 6},
		{EntityRef: handleList, Change: ChangeBodyChanged, Dependents: 3},
	}

	// Callers: written by other people, before this change existed. This is the
	// evidence an agent cannot author, and therefore cannot argue with.
	input.Callers[verifyToken.Key()] = []Evidence{
		{Kind: EvidenceCaller, Path: "internal/api/middleware.go", Line: 31, Note: "VerifyToken(header)"},
		{Kind: EvidenceCaller, Path: "internal/api/middleware.go", Line: 64, Note: "VerifyToken(cookie)"},
		{Kind: EvidenceCaller, Path: "internal/worker/queue.go", Line: 202, Note: "VerifyToken(job.Token)"},
		{Kind: EvidenceTypeUser, Path: "internal/auth/claims.go", Line: 12, Note: "Claims"},
	}
	input.Callers[parseFlags.Key()] = []Evidence{
		{Kind: EvidenceCaller, Path: "internal/cli/root.go", Line: 927, Note: "parseFlags(args)"},
	}
	input.Callers[configLoad.Key()] = []Evidence{
		{Kind: EvidenceCaller, Path: "cmd/server/main.go", Line: 40, Note: "cfg.Load(path)"},
	}
	input.Callers[handleList.Key()] = []Evidence{
		{Kind: EvidenceCaller, Path: "internal/api/router.go", Line: 88, Note: "GET /items"},
	}

	// Coverage evidence for the entity that has a test. Unused while Coverage is a
	// stub; the moment it is implemented, parseFlags renders as verified.
	input.Tests[parseFlags.Key()] = []Evidence{
		{Kind: EvidenceTest, Path: "internal/cli/root_test.go", Line: 212, Note: "TestParseFlags"},
	}

	// Git history, older than the session: list.go has moved with its test in 14
	// of the last 15 commits, and did not this time.
	input.Cochanges["internal/api/list.go"] = []Cochange{
		{Path: "internal/api/list_test.go", Together: 14, Observed: 15},
		{Path: "internal/api/router.go", Together: 4, Observed: 15},
	}

	// Near-duplicate bodies that did not receive the same edit.
	input.Clones[verifyToken.Key()] = []EntityRef{
		{Name: "verifyTokenLegacy", Kind: "function", Path: "internal/auth/legacy.go", Line: 140},
		{Name: "VerifyToken", Kind: "function", Path: "internal/gateway/token.go", Line: 55},
	}

	for _, change := range input.Changes {
		input.ChangedFiles[change.Path] = true
	}

	// The forty-three entities nobody needs to read. They are carried rather than
	// dropped because "43 others had nothing" is itself a claim the report makes,
	// and a reviewer must be able to check it with --all.
	for index := 0; index < 43; index++ {
		input.Changes = append(input.Changes, Change{
			EntityRef: EntityRef{
				Name: fmt.Sprintf("helper%02d", index),
				Kind: "function",
				Path: fmt.Sprintf("internal/util/helper%02d.go", index),
				Line: 10,
			},
			Change: ChangeBodyChanged,
		})
	}

	return input
}
