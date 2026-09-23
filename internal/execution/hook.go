package execution

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// hookWaitDelay is how long a cancelled hook gets to exit after SIGTERM
// before it is killed outright.
const hookWaitDelay = 5 * time.Second

// runHook runs one hook command through `sh -c` with the caller's
// environment plus env. Cancelling ctx stops the command and, on Unix,
// everything it started.
func runHook(ctx context.Context, command string, env []string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	configureHookProcess(cmd)
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
