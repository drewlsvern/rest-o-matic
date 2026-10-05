package config

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
)

// goos and lookPath are runtime.GOOS and exec.LookPath; variables so tests
// can check other platforms and a missing podman.
var (
	goos     = runtime.GOOS
	lookPath = exec.LookPath
)

// ValidationError identifies a config problem, optionally scoped to a job or
// a repository.
type ValidationError struct {
	Job        string
	Repository string
	// Path is the dotted path of the field concerned, such as
	// backups.docs.policy, used to find it in the file.
	Path    string
	Message string
}

func (e ValidationError) Error() string {
	switch {
	case e.Job != "":
		return fmt.Sprintf("job %q: %s", e.Job, e.Message)
	case e.Repository != "":
		return fmt.Sprintf("repository %q: %s", e.Repository, e.Message)
	}
	return e.Message
}

// Warning identifies a config that is likely, but not certainly, wrong.
// Warnings never cause validation to fail.
type Warning struct {
	Job        string
	Repository string
	// Path is the dotted path of the field concerned.
	Path    string
	Message string
}

func (w Warning) String() string {
	switch {
	case w.Job != "":
		return fmt.Sprintf("job %q: %s", w.Job, w.Message)
	case w.Repository != "":
		return fmt.Sprintf("repository %q: %s", w.Repository, w.Message)
	}
	return w.Message
}

// Result holds everything Validate found. The config is valid when Errors is
// empty, regardless of Warnings.
type Result struct {
	Errors   []error
	Warnings []Warning
}

// Validate checks cross-references, required fields, and each repository's
// backend/url consistency across the whole config, returning every problem
// found rather than stopping at the first.
func Validate(cfg *Config) Result {
	var res Result

	repoNames := make([]string, 0, len(cfg.Repositories))
	for name := range cfg.Repositories {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)
	for _, name := range repoNames {
		errs, warns := CheckRepository(name, cfg.Repositories[name])
		res.Errors = append(res.Errors, errs...)
		res.Warnings = append(res.Warnings, warns...)
	}

	for name, job := range cfg.Backups {
		at := "backups." + name + "."
		if job.Policy == "" {
			res.Errors = append(res.Errors, ValidationError{Job: name, Path: at + "policy", Message: "must reference a policy"})
		} else if _, ok := cfg.Policies[job.Policy]; !ok {
			res.Errors = append(res.Errors, ValidationError{Job: name, Path: at + "policy", Message: fmt.Sprintf("references undefined policy %q", job.Policy)})
		}

		if len(job.Source.Paths) == 0 {
			res.Errors = append(res.Errors, ValidationError{Job: name, Path: at + "source.paths", Message: "source.paths must be non-empty"})
		}

		if len(job.Repositories) == 0 {
			res.Errors = append(res.Errors, ValidationError{Job: name, Path: at + "repositories", Message: "must reference at least one repository"})
		}
		for i, ref := range job.Repositories {
			if _, ok := cfg.Repositories[ref.Name]; !ok {
				res.Errors = append(res.Errors, ValidationError{Job: name, Path: fmt.Sprintf("%srepositories.%d", at, i), Message: fmt.Sprintf("references undefined repository %q", ref.Name)})
			}
		}

		if warn, err := checkReadMode(name, job); err != nil {
			res.Errors = append(res.Errors, err)
		} else if warn != nil {
			res.Warnings = append(res.Warnings, *warn)
		}
	}

	return res
}

// checkReadMode validates a job's read_as. podman-unshare is Linux-only:
// elsewhere Podman runs containers in a VM, and bind-mounted files show up
// on the host with ordinary ownership. A missing podman is only a warning,
// since the config may be validated on another machine than the one that
// runs it.
func checkReadMode(name string, job Job) (*Warning, error) {
	switch job.ReadMode() {
	case ReadDirect:
		return nil, nil
	case ReadPodmanUnshare:
		if goos != "linux" {
			return nil, ValidationError{Job: name, Path: "backups." + name + ".read_as", Message: fmt.Sprintf("read_as: %s is not needed on %s: Podman runs containers in a VM there and bind-mounted files are readable directly; remove read_as", ReadPodmanUnshare, goos)}
		}
		if _, err := lookPath("podman"); err != nil {
			return &Warning{Job: name, Path: "backups." + name + ".read_as", Message: fmt.Sprintf("read_as: %s needs podman, which was not found on PATH", ReadPodmanUnshare)}, nil
		}
		return nil, nil
	default:
		return nil, ValidationError{Job: name, Path: "backups." + name + ".read_as", Message: fmt.Sprintf("read_as: unknown value %q (allowed: %s, %s)", job.ReadAs, ReadDirect, ReadPodmanUnshare)}
	}
}
