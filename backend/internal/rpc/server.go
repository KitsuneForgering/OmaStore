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
	"sync/atomic"
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
	// catalogInterval limits how often a running index sends catalog.changed.
	catalogInterval time.Duration
	restart         chan struct{} // closed when a client asked the daemon to restart
	restartOnce     sync.Once
}

// NewServer creates a server on top of the backend.
func NewServer(b Backend, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{backend: b, log: log, conns: map[*conn]struct{}{}, ctx: ctx, cancel: cancel, lastActive: time.Now(),
		catalogInterval: 2 * time.Second, restart: make(chan struct{})}
	s.jobs = newJobs(s.broadcast)
	return s
}

// Outgoing messages: how many can wait for a client that is not reading, and
// how long a single write may take.
const (
	sendQueue    = 1024
	writeTimeout = 5 * time.Second
)

var errSlowClient = errors.New("client is not reading its messages")

// conn is a connected client. Its messages go through a queue written by its
// own goroutine, so sending never blocks: job progress is reported from
// inside the indexer, which must not wait for a client.
type conn struct {
	c       net.Conn
	out     chan []byte
	done    chan struct{}
	once    sync.Once
	pending atomic.Int64 // queued messages not written yet
}

func newConn(nc net.Conn) *conn {
	c := &conn{c: nc, out: make(chan []byte, sendQueue), done: make(chan struct{})}
	go c.writeLoop()
	return c
}

// send queues a message, written on one line. A client whose queue is full
// stopped reading: it is disconnected instead of holding back the sender.
func (c *conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	select {
	case <-c.done:
		return net.ErrClosed
	default:
	}
	c.pending.Add(1)
	select {
	case c.out <- b:
		return nil
	default:
		c.pending.Add(-1)
		c.close()
		return errSlowClient
	}
}

func (c *conn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.out:
			c.c.SetWriteDeadline(time.Now().Add(writeTimeout))
			_, err := c.c.Write(b)
			c.pending.Add(-1)
			if err != nil {
				c.close()
				return
			}
		}
	}
}

// close drops the queued messages and closes the connection.
func (c *conn) close() {
	c.once.Do(func() {
		close(c.done)
		c.c.Close()
	})
}

// finish closes the connection after the queued messages are written (for
// at most writeTimeout).
func (c *conn) finish() {
	deadline := time.Now().Add(writeTimeout)
	for c.pending.Load() > 0 && time.Now().Before(deadline) {
		select {
		case <-c.done:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	c.close()
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
		return nil, fmt.Errorf("listen on %s: %w", path, err)
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
		c := newConn(nc)
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
			c.finish()
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

// Stamper is implemented by backends that can tell when the database changed,
// including changes made by another process.
type Stamper interface {
	ChangeStamp(ctx context.Context) (string, error)
}

// WatchChanges polls the backend's change stamp every interval until Shutdown
// and sends catalog.changed when something else (e.g. `omastore index` run by
// the timer) changed the database. While a job runs, the job's own
// notification covers it. Does nothing if the backend is not a Stamper.
func (s *Server) WatchChanges(interval time.Duration) {
	st, ok := s.backend.(Stamper)
	if !ok {
		return
	}
	last, _ := st.ChangeStamp(s.ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		stamp, err := st.ChangeStamp(s.ctx)
		if err != nil || stamp == last {
			continue
		}
		last = stamp
		s.mu.Lock()
		clients := len(s.conns)
		s.mu.Unlock()
		if clients > 0 && s.jobs.running() == 0 {
			s.broadcast("catalog.changed", struct{}{})
		}
	}
}

// restartDelay lets the reply to self.restart reach the client first.
var restartDelay = 200 * time.Millisecond

func (s *Server) requestRestart() {
	s.restartOnce.Do(func() { time.AfterFunc(restartDelay, func() { close(s.restart) }) })
}

// Restart is closed when a client asks the daemon to exit so that it is
// started again from the updated executable (self.restart).
func (s *Server) Restart() <-chan struct{} { return s.restart }

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
		// Stop reading, so handleConn returns and finish writes the last
		// notifications (the jobs' final state) before closing.
		if cr, ok := c.c.(interface{ CloseRead() error }); ok && cr.CloseRead() == nil {
			continue
		}
		c.close()
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
					c.close()
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
