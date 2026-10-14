// Package exits defines the CLI's exit codes and the error that carries one.
//
// These codes are part of the CLI's contract and match the Python CLI
// (kemory_cli/exits.py). Add to the list; never renumber it, and never reuse a
// retired number for a different meaning.
package exits

import "errors"

const (
	// OK is success.
	OK = 0
	// Failure is anything that does not fit a category below.
	Failure = 1
	// Usage is a bad invocation.
	Usage = 2
	// Auth means no credentials, expired credentials, or the server rejected
	// them. Actionable: `kemory login`.
	Auth = 3
	// Network means the API could not be reached at all. Nothing ran
	// server-side.
	Network = 4
	// Forbidden is a 403: authenticated, but not allowed.
	Forbidden = 5
	// NotFound is a 404, usually a stale id.
	NotFound = 6
	// Conflict is a 409.
	Conflict = 7
	// RateLimited is a 429.
	RateLimited = 8
	// Unavailable is a 503: kemory answered "not right now".
	Unavailable = 9
)

var byStatus = map[int]int{
	401: Auth,
	403: Forbidden,
	404: NotFound,
	409: Conflict,
	429: RateLimited,
	503: Unavailable,
}

// ForStatus maps an HTTP status to an exit code. Unlisted statuses are Failure.
func ForStatus(status int) int {
	if code, ok := byStatus[status]; ok {
		return code
	}
	return Failure
}

// Error is a user-facing failure with the exit code it implies.
type Error struct {
	Message string
	Code    int
}

func (e *Error) Error() string { return e.Message }

// New returns an Error with the given exit code.
func New(message string, code int) *Error { return &Error{Message: message, Code: code} }

// Code returns the exit code for err: its own code when it is an *Error,
// Failure otherwise.
func Code(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Failure
}
