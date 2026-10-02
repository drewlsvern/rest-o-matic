//go:build windows

package execution

import "os/exec"

// configureWrappedResticCancel is unreachable on Windows, where config
// validation rejects every wrapped read mode; it keeps the direct
// behaviour so the package builds.
func configureWrappedResticCancel(cmd *exec.Cmd) { configureResticCancel(cmd) }
