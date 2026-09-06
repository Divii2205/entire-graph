package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The verdict IS the exit status, so the plumbing that carries it out of Run has
// to be tested at this level: a report that prints CONTINUE while the process
// exits 0 is a gate that does not gate.
func TestGateCarriesTheVerdictOutAsAnExitCode(t *testing.T) {
	var stdout bytes.Buffer
	err := Run(t.Context(), Options{Stdout: &stdout}, []string{"gate", "--sample"})

	if err == nil {
		t.Fatal("gate returned no exit status for a continue verdict")
	}
	code, printMessage := ExitCodeOf(err)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (continue)", code)
	}
	if printMessage {
		t.Error("the exit status was reported as a message; the report is already on stdout")
	}
	if !strings.Contains(stdout.String(), "Gate verdict: CONTINUE") {
		t.Errorf("report missing from stdout:\n%s", stdout.String())
	}
}

// A plain error keeps the historical behaviour: exit 1, message printed. The new
// carrier must not change what every other command does.
func TestExitCodeOfPlainErrorIsUnchanged(t *testing.T) {
	code, printMessage := ExitCodeOf(errors.New("something broke"))
	if code != 1 || !printMessage {
		t.Errorf("plain error -> (%d, %v), want (1, true)", code, printMessage)
	}
	if code, printMessage := ExitCodeOf(nil); code != 0 || printMessage {
		t.Errorf("nil error -> (%d, %v), want (0, false)", code, printMessage)
	}
}

func TestGateJSONIsMachineReadable(t *testing.T) {
	var stdout bytes.Buffer
	Run(t.Context(), Options{Stdout: &stdout}, []string{"gate", "--sample", "--json"})

	var report struct {
		Verdict  string `json:"verdict"`
		ExitCode int    `json:"exit_code"`
		Review   []struct {
			Change struct {
				Name string `json:"name"`
			} `json:"change"`
		} `json:"review"`
		Checks []struct {
			ID  string `json:"id"`
			Ran bool   `json:"ran"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("gate --json did not emit valid JSON: %v\n%s", err, stdout.String())
	}
	if report.Verdict != "continue" || report.ExitCode != 1 {
		t.Errorf("verdict/exit = %q/%d, want continue/1", report.Verdict, report.ExitCode)
	}
	if len(report.Review) == 0 || report.Review[0].Change.Name != "VerifyToken" {
		t.Errorf("review order did not survive the JSON round trip: %+v", report.Review)
	}
	// A consumer has to be able to see that a check did not run, not infer it
	// from an absent finding list.
	if len(report.Checks) != 4 {
		t.Errorf("expected 4 checks in the JSON report, got %d", len(report.Checks))
	}
}

func TestGateRejectsUnknownFlags(t *testing.T) {
	err := Run(t.Context(), Options{Stdout: &bytes.Buffer{}}, []string{"gate", "--sample", "--nope"})
	if err == nil {
		t.Fatal("an unknown flag was accepted")
	}
	if !strings.Contains(err.Error(), "--nope") {
		t.Errorf("error does not name the unknown flag: %v", err)
	}
}

func TestGateFlagDefaults(t *testing.T) {
	flags, unknown, err := parseGateFlags(nil)
	if err != nil || len(unknown) != 0 {
		t.Fatalf("parse: %v %v", err, unknown)
	}
	if flags.base != "HEAD~1" || flags.head != "HEAD" {
		t.Errorf("default range = %s..%s, want HEAD~1..HEAD", flags.base, flags.head)
	}

	// An explicit --limit 0 means "no limit", not "show nothing" — zero already
	// selects the renderer's default, so it has to be translated.
	flags, _, err = parseGateFlags([]string{"--limit", "0"})
	if err != nil {
		t.Fatalf("parse --limit 0: %v", err)
	}
	if flags.limit != -1 {
		t.Errorf("--limit 0 -> %d, want -1 (unbounded)", flags.limit)
	}

	if _, _, err := parseGateFlags([]string{"--limit", "-3"}); err == nil {
		t.Error("a negative --limit was accepted")
	}
	if _, _, err := parseGateFlags([]string{"--base"}); err == nil {
		t.Error("--base with no value was accepted")
	}
}
