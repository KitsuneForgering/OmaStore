// Package rpc implements OmaStore's JSON-RPC 2.0 server over a Unix
// socket. Each message is a JSON object on one line (NDJSON). Long operations
// (indexing, installation) return a job id and emit progress notifications
// over the same socket. The protocol is documented in docs/ipc.md.
package rpc

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/sysdeps"
)

// ProtocolVersion is returned by daemon.hello. Bump it whenever a method or
// a DTO changes: the interface compares it with the one it was built for and
// tells the user when the running daemon is older (docs/ipc.md).
const ProtocolVersion = 2

// Methods are the methods this daemon answers, also returned by
// daemon.hello so an interface can check for the ones it needs.
var Methods = []string{
	"daemon.hello",
	"catalog.list", "catalog.get", "catalog.similar", "catalog.categories",
	"installs.list", "index.start", "install.start", "update.start", "install.uninstall", "install.rollback",
	"jobs.list", "jobs.cancel", "author.check", "star.get", "star.set", "deps.check", "deps.install",
	"self.status", "self.update", "self.restart", "image.get",
}

// request is a JSON-RPC request or notification.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is a JSON-RPC response.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// notification is a server message without an id.
type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// Error is a JSON-RPC error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

// Error codes. The negative ones from -32700 to -32600 come from the
// specification; the ones from -32001 on are OmaStore's.
const (
	CodeParse          = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603

	CodeNotFound       = -32001
	CodeBusy           = -32002
	CodeConflict       = -32003
	CodeNotInstallable = -32004
	CodeRateLimited    = -32005
	CodeNotInstalled   = -32006
	CodeUpToDate       = -32007
	CodeChecksum       = -32008
	CodeCanceled       = -32009
	CodeIncomplete     = -32010
	CodeAuthRequired   = -32011
	CodeDenied         = -32012
	CodeUnsupported    = -32013
	CodeUnavailable    = -32014
	CodeInUse          = -32015
	CodeNoPrevious     = -32016
	CodeUnverified     = -32017
	CodeStaleDatabase  = -32018
)

// ErrBusy means an equivalent job is already running.
var ErrBusy = errors.New("an operation is already running")

func errInvalidParams(format string, a ...any) *Error {
	return &Error{Code: CodeInvalidParams, Message: fmt.Sprintf(format, a...)}
}

// toError converts backend errors into stable codes for the frontend.
func toError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	code := CodeInternal
	var rl *github.RateLimitError
	switch {
	case errors.Is(err, store.ErrNotFound):
		code = CodeNotFound
	case errors.Is(err, ErrBusy), errors.Is(err, index.ErrBusy), errors.Is(err, install.ErrBusy):
		code = CodeBusy
	case errors.Is(err, install.ErrConflict):
		code = CodeConflict
	case errors.Is(err, install.ErrNotInstallable):
		code = CodeNotInstallable
	case errors.As(err, &rl):
		code = CodeRateLimited
	case errors.Is(err, install.ErrNotInstalled):
		code = CodeNotInstalled
	case errors.Is(err, install.ErrUpToDate):
		code = CodeUpToDate
	case errors.Is(err, install.ErrChecksum):
		code = CodeChecksum
	case errors.Is(err, errCanceled):
		code = CodeCanceled
	case errors.Is(err, install.ErrIncomplete):
		code = CodeIncomplete
	case errors.Is(err, install.ErrInUse):
		code = CodeInUse
	case errors.Is(err, install.ErrNoPrevious):
		code = CodeNoPrevious
	case errors.Is(err, install.ErrUnverified):
		code = CodeUnverified
	case errors.Is(err, github.ErrNoToken), errors.Is(err, github.ErrStarForbidden):
		code = CodeAuthRequired
	case errors.Is(err, sysdeps.ErrDenied):
		code = CodeDenied
	case errors.Is(err, sysdeps.ErrNoPacman):
		code = CodeUnsupported
	case errors.Is(err, sysdeps.ErrStaleDatabase):
		code = CodeStaleDatabase
	case errors.Is(err, sysdeps.ErrUnavailable):
		code = CodeUnavailable
	case errors.Is(err, github.ErrNotFound):
		code = CodeNotFound
	}
	return &Error{Code: code, Message: err.Error()}
}
