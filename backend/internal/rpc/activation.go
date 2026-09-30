package rpc

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// listenFDsStart is the first descriptor passed by systemd.
const listenFDsStart = 3

// ActivationListener returns the socket received through systemd socket
// activation (LISTEN_PID/LISTEN_FDS), or nil if the process was not activated that way.
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
		return nil, fmt.Errorf("expected 1 socket from systemd, got %d", n)
	}
	// Do not pass them on to child processes.
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
	f := os.NewFile(uintptr(listenFDsStart), "systemd-socket")
	defer f.Close()
	l, err := net.FileListener(f)
	if err != nil {
		return nil, fmt.Errorf("systemd socket: %w", err)
	}
	return l, nil
}
