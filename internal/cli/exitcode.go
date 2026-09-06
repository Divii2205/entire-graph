package cli

import "errors"

// ExitCodeError carries a process exit status out of a command handler.
//
// Every other command in this CLI answers a question, so "worked" and "failed"
// were the only two outcomes a caller needed and main.go could map any error to
// exit 1. Gate answers a JUDGEMENT — keep, continue, revert, unusable — and a CI
// job has to be able to tell those apart without parsing the output. So a
// verdict is not an error in the usual sense: the command did exactly what it was
// asked to, and the status is the answer.
//
// Message is empty for a verdict that already printed its own report. main.go
// prints nothing extra in that case; it just exits with Code. A non-empty Message
// behaves like any other error and is written to stderr.
type ExitCodeError struct {
	Code    int
	Message string
}

func (e *ExitCodeError) Error() string {
	return e.Message
}

// Silent reports whether this error has already said everything it needs to say
// on stdout, so the entry point must not print it again.
func (e *ExitCodeError) Silent() bool {
	return e.Message == ""
}

// NewExitCode builds a silent exit-status carrier for a command that has already
// written its own report.
func NewExitCode(code int) error {
	return &ExitCodeError{Code: code}
}

// ExitCodeOf reports the process exit status an error should produce, and
// whether the entry point still needs to print it.
//
// It is the single place the mapping lives, so a future command that also
// carries a status does not have to touch main.go. Anything that is not an
// ExitCodeError keeps the historical behaviour: exit 1, message printed.
func ExitCodeOf(err error) (code int, printMessage bool) {
	if err == nil {
		return 0, false
	}
	var carrier *ExitCodeError
	if errors.As(err, &carrier) {
		return carrier.Code, !carrier.Silent()
	}
	return 1, true
}
