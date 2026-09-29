// Package rpc implementa o servidor JSON-RPC 2.0 do OmaStore sobre um socket
// Unix. Cada mensagem é um objeto JSON numa linha (NDJSON). Operações longas
// (indexação, instalação) retornam um id de job e emitem notificações de
// progresso pelo mesmo socket. O protocolo está documentado em docs/ipc.md.
package rpc

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

// Versão do protocolo, devolvida por daemon.hello.
const ProtocolVersion = 1

// request é uma requisição ou notificação JSON-RPC.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response é uma resposta JSON-RPC.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// notification é uma mensagem do servidor sem id.
type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// Error é um erro JSON-RPC.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

// Códigos de erro. Os negativos de -32700 a -32600 são da especificação;
// os de -32001 em diante são do OmaStore.
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
)

// ErrBusy indica que já existe um job equivalente em andamento.
var ErrBusy = errors.New("já existe uma operação em andamento")

func errInvalidParams(format string, a ...any) *Error {
	return &Error{Code: CodeInvalidParams, Message: fmt.Sprintf(format, a...)}
}

// toError converte erros do backend em códigos estáveis para o frontend.
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
	case errors.Is(err, ErrBusy):
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
	}
	return &Error{Code: code, Message: err.Error()}
}
