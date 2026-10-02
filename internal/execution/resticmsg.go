package execution

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
)

// maxResticMessages is how many of restic's messages a reported error
// carries before the rest are only counted. A backup that can't read a
// directory tree can produce one message per file.
const maxResticMessages = 3

// resticFailure builds the error for a restic command that failed: what
// was being done, restic's exit status, and what restic said, as plain
// text (see resticMessages).
func resticFailure(what string, runErr error, stderr string) error {
	if messages := resticMessages(stderr); messages != "" {
		return fmt.Errorf("%s: %w: %s", what, runErr, messages)
	}
	return fmt.Errorf("%s: %w", what, runErr)
}

// resticMessages turns what restic wrote to standard error into its
// messages as plain text, joined with "; ". Under --json restic reports
// errors as JSON objects, one per line; elsewhere, and for some warnings
// even under --json, it prints plain lines. Both become plain messages
// here, so an error reads the same in a terminal, in `status`, and in a
// notification.
func resticMessages(stderr string) string {
	var messages []string
	seen := map[string]bool{}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		message := messageIn(line)
		if message != "" && !seen[message] {
			seen[message] = true
			messages = append(messages, message)
		}
		// Without --json a fatal error is the last thing restic says;
		// the plain lines after it are advice about the same failure.
		if strings.HasPrefix(line, "Fatal: ") {
			break
		}
	}
	if extra := len(messages) - maxResticMessages; extra > 0 {
		messages = append(messages[:maxResticMessages], fmt.Sprintf("and %d more", extra))
	}
	return strings.Join(messages, "; ")
}

// messageIn is the message in one line of restic's standard error, or
// "" for a line that carries none.
func messageIn(line string) string {
	if !strings.HasPrefix(line, "{") {
		return tidy(line)
	}
	var obj struct {
		MessageType string `json:"message_type"`
		Message     string `json:"message"`
		Error       struct {
			Message string `json:"message"`
			// restic up to 0.16 reported the operating system's error as
			// its parts, with a number in place of the text.
			Op   string `json:"Op"`
			Path string `json:"Path"`
			Err  any    `json:"Err"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return tidy(line) // not JSON after all
	}
	switch obj.MessageType {
	case "exit_error":
		return tidy(obj.Message)
	case "error":
		if obj.Error.Message != "" {
			return tidy(obj.Error.Message)
		}
		if obj.Error.Op != "" || obj.Error.Path != "" {
			return tidy(strings.TrimSpace(obj.Error.Op+" "+obj.Error.Path) + ": " + errnoText(obj.Error.Err))
		}
	}
	// Progress and summary objects are not messages.
	return ""
}

// errnoText describes the error number restic 0.16 printed.
func errnoText(v any) string {
	switch n := v.(type) {
	case float64:
		return syscall.Errno(int(n)).Error()
	case string:
		return n
	}
	return "error"
}

// tidy keeps the first line of a message and drops a leading "Fatal: ":
// the error it becomes part of already says the command failed, and what
// follows the first line is advice that repeats the repository's location.
func tidy(message string) string {
	message, _, _ = strings.Cut(strings.TrimSpace(message), "\n")
	return strings.TrimSpace(strings.TrimPrefix(message, "Fatal: "))
}
