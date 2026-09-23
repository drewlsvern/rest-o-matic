package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// interruptContexts returns the two contexts a job runs under. The first
// SIGINT/SIGTERM cancels work (stopping before hooks, restic, and jobs not
// yet started) with the signal as its cause; the after hooks keep running
// under cleanup until a second signal cancels that too. stop releases the
// signal handler.
func interruptContexts() (work, cleanup context.Context, stop func()) {
	work, cancelWork := context.WithCancelCause(context.Background())
	cleanup, cancelCleanup := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		for received := 0; ; received++ {
			select {
			case sig := <-sigs:
				if received == 0 {
					cancelWork(fmt.Errorf("interrupted by %s", signalName(sig)))
					continue
				}
				cancelCleanup()
				return
			case <-done:
				return
			}
		}
	}()

	return work, cleanup, func() {
		signal.Stop(sigs)
		close(done)
		cancelWork(nil)
		cancelCleanup()
	}
}

func signalName(sig os.Signal) string {
	switch sig {
	case os.Interrupt:
		return "SIGINT"
	case syscall.SIGTERM:
		return "SIGTERM"
	}
	return sig.String()
}
