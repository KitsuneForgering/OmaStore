// Command omastored: OmaStore's daemon. Serves the frontend over JSON-RPC 2.0
// at $XDG_RUNTIME_DIR/omastore.sock (see docs/ipc.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/app"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/logging"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/rpc"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "omastored:", err)
		os.Exit(1)
	}
}

func run() error {
	socket := flag.String("socket", "", "socket path (default $XDG_RUNTIME_DIR/omastore.sock)")
	verbose := flag.Bool("v", false, "verbose logging")
	idleTimeout := flag.Duration("idle-timeout", -1,
		"exit after this long without clients or jobs (0 = never; default: 10m if started by systemd, otherwise never)")
	flag.Parse()
	level := ""
	if *verbose {
		level = "debug"
	}
	log := logging.Setup(os.Stderr, level)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.Open(ctx, log)
	if err != nil {
		return err
	}
	defer a.Close()

	if n, err := a.Images.Prune(imageMaxAge, imageMaxBytes); err != nil {
		log.Warn("image cache not pruned", "err", err)
	} else if n > 0 {
		log.Info("image cache pruned", "removed", n)
	}

	l, err := rpc.ActivationListener()
	if err != nil {
		return err
	}
	activated := l != nil
	path := *socket
	if path == "" {
		path = a.Paths.Socket
	}
	if l == nil {
		if l, err = rpc.Listen(path); err != nil {
			return err
		}
		defer os.Remove(path)
	}

	// With socket activation systemd restarts the daemon on the next connection,
	// so exiting when idle costs the user nothing.
	idle := *idleTimeout
	if idle < 0 {
		idle = 0
		if activated {
			idle = defaultIdleTimeout
		}
	}

	srv := rpc.NewServer(a, log)
	go srv.WatchChanges(changePollInterval)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	log.Info("omastored ready", "socket", addr(l), "idle-timeout", idle)

	var idleC <-chan time.Time
	if idle > 0 {
		t := time.NewTicker(max(idle/4, 50*time.Millisecond))
		defer t.Stop()
		idleC = t.C
	}
loop:
	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			break loop
		case err = <-errc:
			break loop
		case <-srv.Restart():
			log.Info("restart requested; shutting down")
			break loop
		case <-idleC:
			if d := srv.IdleFor(); d >= idle {
				log.Info("idle; shutting down", "idle-for", d.Round(time.Second))
				break loop
			}
		}
	}
	srv.Shutdown()
	return err
}

// changePollInterval is how often the daemon checks for changes made by
// other processes (the CLI or the index timer) to tell the frontend.
const changePollInterval = 15 * time.Second

// Image cache limits, applied when the daemon starts.
const (
	imageMaxAge         = 30 * 24 * time.Hour
	imageMaxBytes int64 = 200 << 20
)

// defaultIdleTimeout is the idle time before exiting when started by
// socket activation.
const defaultIdleTimeout = 10 * time.Minute

func addr(l net.Listener) string { return l.Addr().String() }
