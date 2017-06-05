// +build !windows

package daemon

import (
	"os"
	"os/signal"
	"syscall"

	psignal "github.com/docker/docker/pkg/signal"
)

// Reading: Dump the stack when receive SIGUSR1
func setupDumpStackTrap() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR1)
	// Reading: SIGUSR1 is used for simple interprocess communication
	go func() {
		for range c {
			psignal.DumpStacks()
		}
	}()
}
