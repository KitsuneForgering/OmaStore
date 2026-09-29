// Comando omastored: daemon do OmaStore. Atende o frontend por JSON-RPC 2.0
// em $XDG_RUNTIME_DIR/omastore.sock (ver docs/ipc.md).
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
	socket := flag.String("socket", "", "caminho do socket (default $XDG_RUNTIME_DIR/omastore.sock)")
	verbose := flag.Bool("v", false, "log detalhado")
	idleTimeout := flag.Duration("idle-timeout", -1,
		"encerra após esse tempo sem clientes nem jobs (0 = nunca; padrão: 10m se iniciado pelo systemd, senão nunca)")
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

	// Com socket activation o systemd reinicia o daemon na próxima conexão,
	// então encerrar quando ocioso não tem custo para o usuário.
	idle := *idleTimeout
	if idle < 0 {
		idle = 0
		if activated {
			idle = defaultIdleTimeout
		}
	}

	srv := rpc.NewServer(a, log)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	log.Info("omastored pronto", "socket", addr(l), "idle-timeout", idle)

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
			log.Info("encerrando")
			break loop
		case err = <-errc:
			break loop
		case <-idleC:
			if d := srv.IdleFor(); d >= idle {
				log.Info("ocioso; encerrando", "ocioso-ha", d.Round(time.Second))
				break loop
			}
		}
	}
	srv.Shutdown()
	return err
}

// defaultIdleTimeout é o tempo ocioso antes de encerrar quando iniciado
// por socket activation.
const defaultIdleTimeout = 10 * time.Minute

func addr(l net.Listener) string { return l.Addr().String() }
