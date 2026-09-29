package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// maxSocketPath é o tamanho de sockaddr_un.sun_path no Linux.
const maxSocketPath = 108

// maxMessage limita o tamanho de uma mensagem recebida.
const maxMessage = 1 << 20

// Server atende clientes JSON-RPC.
type Server struct {
	backend Backend
	log     *slog.Logger
	jobs    *jobs

	mu        sync.Mutex
	conns     map[*conn]struct{}
	listeners []net.Listener
	// lastActive é quando o servidor deixou de ter conexões ou jobs.
	lastActive time.Time
	ctx        context.Context // cancelado no Shutdown
	cancel     context.CancelFunc
	connsWG    sync.WaitGroup
}

// NewServer cria um servidor sobre o backend.
func NewServer(b Backend, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{backend: b, log: log, conns: map[*conn]struct{}{}, ctx: ctx, cancel: cancel, lastActive: time.Now()}
	s.jobs = newJobs(s.broadcast)
	return s
}

// conn é um cliente conectado.
type conn struct {
	c   net.Conn
	wmu sync.Mutex
}

// send escreve uma mensagem numa linha. Um cliente lento não trava os
// demais: a escrita tem prazo.
func (c *conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = c.c.Write(b)
	return err
}

// broadcast envia uma notificação a todos os clientes.
func (s *Server) broadcast(method string, params any) {
	s.mu.Lock()
	conns := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	msg := notification{JSONRPC: "2.0", Method: method, Params: params}
	for _, c := range conns {
		if err := c.send(msg); err != nil {
			s.log.Debug("falha ao notificar cliente", "err", err)
			c.c.Close()
		}
	}
}

// Listen abre o socket Unix em path com permissão 0600. Um socket órfão
// (de um daemon que morreu) é removido; se outro daemon estiver atendendo,
// retorna erro.
func Listen(path string) (net.Listener, error) {
	// sockaddr_un.sun_path tem 108 bytes (com o terminador); o erro do bind
	// ("invalid argument") não explica isso.
	if len(path) >= maxSocketPath {
		return nil, fmt.Errorf("caminho do socket longo demais (%d bytes, máximo %d): %s", len(path), maxSocketPath-1, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, fmt.Errorf("outro omastored já está rodando em %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remover socket órfão: %w", err)
		}
	}
	// umask restritivo durante o bind para não haver janela com 0777.
	old := umask(0o177)
	l, err := net.Listen("unix", path)
	umask(old)
	if err != nil {
		return nil, fmt.Errorf("escutar em %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// Serve atende conexões até o listener ser fechado ou Shutdown ser chamado.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		l.Close()
		return nil
	}
	s.listeners = append(s.listeners, l)
	s.mu.Unlock()
	for {
		nc, err := l.Accept()
		if err != nil {
			if s.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		c := &conn{c: nc}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		s.connsWG.Add(1)
		go func() {
			defer s.connsWG.Done()
			s.handleConn(c)
			s.mu.Lock()
			delete(s.conns, c)
			s.lastActive = time.Now()
			s.mu.Unlock()
			nc.Close()
		}()
	}
}

// IdleFor diz há quanto tempo o servidor está ocioso: sem conexões e sem
// jobs em andamento. Retorna 0 se houver atividade.
func (s *Server) IdleFor() time.Duration {
	running, lastJob := s.jobs.activity()
	s.mu.Lock()
	defer s.mu.Unlock()
	if running > 0 || len(s.conns) > 0 {
		return 0
	}
	since := s.lastActive
	if lastJob.After(since) {
		since = lastJob
	}
	return time.Since(since)
}

// Shutdown para de aceitar conexões, cancela os jobs, espera terminarem e
// fecha as conexões.
func (s *Server) Shutdown() {
	// Fecha os listeners antes de retornar: depois do Shutdown nenhuma
	// conexão nova é aceita (e o socket Unix é removido pelo Close).
	s.mu.Lock()
	s.cancel()
	for _, l := range s.listeners {
		l.Close()
	}
	s.listeners = nil
	s.mu.Unlock()
	s.jobs.shutdown()
	s.mu.Lock()
	for c := range s.conns {
		c.c.Close()
	}
	s.mu.Unlock()
	s.connsWG.Wait()
}

func (s *Server) handleConn(c *conn) {
	r := bufio.NewReaderSize(c.c, 64<<10)
	for {
		line, err := readLine(r)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				c.send(response{JSONRPC: "2.0", ID: json.RawMessage("null"),
					Error: &Error{Code: CodeInvalidRequest, Message: err.Error()}})
			}
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		// Cada requisição roda em paralelo: uma chamada lenta (image.get)
		// não bloqueia as outras do mesmo cliente.
		go func() {
			if resp := s.handleMessage(line); resp != nil {
				if err := c.send(resp); err != nil {
					c.c.Close()
				}
			}
		}()
	}
}

var errTooLong = errors.New("mensagem maior que o limite")

// readLine lê até '\n' com limite de tamanho.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > maxMessage {
			return nil, errTooLong
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

// handleMessage processa uma linha e retorna a resposta (nil para
// notificações do cliente). Lotes (arrays) não são suportados.
func (s *Server) handleMessage(line []byte) *response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return &response{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &Error{Code: CodeParse, Message: "JSON inválido: " + err.Error()}}
	}
	id := req.ID
	isNotification := len(id) == 0
	if isNotification {
		id = json.RawMessage("null")
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return &response{JSONRPC: "2.0", ID: id,
			Error: &Error{Code: CodeInvalidRequest, Message: `requisição inválida (jsonrpc "2.0" e method são obrigatórios)`}}
	}
	result, err := s.call(req.Method, req.Params)
	if isNotification {
		return nil
	}
	if err != nil {
		return &response{JSONRPC: "2.0", ID: id, Error: toError(err)}
	}
	if result == nil {
		result = struct{}{}
	}
	return &response{JSONRPC: "2.0", ID: id, Result: result}
}
