package execution

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// hookWaitDelay bounds how long a cancelled hook's process tree may take to
// exit before it is killed outright and its output pipes are closed.
const hookWaitDelay = 5 * time.Second

// runHook runs one hook command through the platform's shell (see
// newHookCmd) with the caller's environment plus env. Cancelling ctx stops
// the command and everything it started.
func runHook(ctx context.Context, command string, env []string) error {
	cmd := newHookCmd(ctx, command)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		if output := strings.TrimSpace(out.String()); output != "" {
			return fmt.Errorf("hook %q failed: %w: %s", command, err, output)
		}
		return fmt.Errorf("hook %q failed: %w", command, err)
	}
	return nil
}

// runHooksStopOnError runs commands in order and stops at the first failure,
// for `before` hooks where a later command may depend on an earlier one.
func runHooksStopOnError(ctx context.Context, commands, env []string) error {
	for _, c := range commands {
		if err := runHook(ctx, c, env); err != nil {
			return err
		}
	}
	return nil
}

// runHooksAll runs every command even if earlier ones fail, returning each
// failure, for cleanup and outcome hooks where one failed command must not
// stop the rest (e.g. restarting several containers).
func runHooksAll(ctx context.Context, commands, env []string) []error {
	var errs []error
	for _, c := range commands {
		if err := runHook(ctx, c, env); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
