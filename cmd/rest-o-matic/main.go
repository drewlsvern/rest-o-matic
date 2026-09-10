// Command rest-o-matic is a simple Restic wrapper: it never runs as a
// daemon and never manages the OS scheduler. `tick` and `run` are one-shot
// invocations meant to be driven by whatever periodic trigger the host
// already provides (cron, launchd, ...).
package main

import "os"

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
