package rpc

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// listenFDsStart é o primeiro descritor passado pelo systemd.
const listenFDsStart = 3

// ActivationListener retorna o socket recebido por socket activation do
// systemd (LISTEN_PID/LISTEN_FDS), ou nil se o processo não foi ativado assim.
func ActivationListener() (net.Listener, error) {
	pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if err != nil || pid != os.Getpid() {
		return nil, nil
	}
	n, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || n < 1 {
		return nil, nil
	}
	if n > 1 {
		return nil, fmt.Errorf("esperava 1 socket do systemd, recebi %d", n)
	}
	// Não repassar para processos filhos.
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
	f := os.NewFile(uintptr(listenFDsStart), "systemd-socket")
	defer f.Close()
	l, err := net.FileListener(f)
	if err != nil {
		return nil, fmt.Errorf("socket do systemd: %w", err)
	}
	return l, nil
}
