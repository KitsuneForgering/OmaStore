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

// maxSocketPath is the size of sockaddr_un.sun_path on Linux.
const maxSocketPath = 108

// maxMessage limits the size of a received message.
const maxMessage = 1 << 20

// Server serves JSON-RPC clients.
type Server struct {
	backend Backend
	log     *slog.Logger
	jobs    *jobs

	mu        sync.Mutex
	conns     map[*conn]struct{}
	listeners []net.Listener
	// lastActive is when the server stopped having connections or jobs.
	lastActive time.Time
	ctx        context.Context // canceled on Shutdown
	cancel     context.CancelFunc
	connsWG    sync.WaitGroup
}

// NewServer creates a server on top of the backend.
func NewServer(b Backend, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{backend: b, log: log, conns: map[*conn]struct{}{}, ctx: ctx, cancel: cancel, lastActive: time.Now()}
	s.jobs = newJobs(s.broadcast)
	return s
}

// conn is a connected client.
type conn struct {
	c   net.Conn
	wmu sync.Mutex
}

// send writes a message on one line. A slow client does not block the
// others: the write has a deadline.
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

// broadcast sends a notification to every client.
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
			s.log.Debug("failed to notify client", "err", err)
			c.c.Close()
		}
	}
}

// Listen opens the Unix socket at path with 0600 permissions. An orphan
// socket (from a daemon that died) is removed; if another daemon is serving,
// it returns an error.
func Listen(path string) (net.Listener, error) {
	// sockaddr_un.sun_path has 108 bytes (including the terminator); the bind
	// error ("invalid argument") does not explain that.
	if len(path) >= maxSocketPath {
		return nil, fmt.Errorf("socket path too long (%d bytes, maximum %d): %s", len(path), maxSocketPath-1, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, fmt.Errorf("another omastored is already running at %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove orphan socket: %w", err)
		}
	}
	// Restrictive umask during bind so there is no window with 0777.
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

// Serve serves connections until the listener is closed or Shutdown is called.
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

// IdleFor reports how long the server has been idle: no connections and no
// running jobs. Returns 0 if there is activity.
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

// Shutdown stops accepting connections, cancels the jobs, waits for them to
// finish and closes the connections.
func (s *Server) Shutdown() {
	// Close the listeners before returning: after Shutdown no new connection
	// is accepted (and the Unix socket is removed by Close).
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
		// Each request runs in parallel: a slow call (image.get) does not block
		// the others from the same client.
		go func() {
			if resp := s.handleMessage(line); resp != nil {
				if err := c.send(resp); err != nil {
					c.c.Close()
				}
			}
		}()
	}
}

var errTooLong = errors.New("message larger than the limit")

// readLine reads up to '\n' with a size limit.
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

// handleMessage processes a line and returns the response (nil for client
// notifications). Batches (arrays) are not supported.
func (s *Server) handleMessage(line []byte) *response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return &response{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &Error{Code: CodeParse, Message: "invalid JSON: " + err.Error()}}
	}
	id := req.ID
	isNotification := len(id) == 0
	if isNotification {
		id = json.RawMessage("null")
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return &response{JSONRPC: "2.0", ID: id,
			Error: &Error{Code: CodeInvalidRequest, Message: `invalid request (jsonrpc "2.0" and method are required)`}}
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
