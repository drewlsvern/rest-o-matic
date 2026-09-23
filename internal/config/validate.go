package config

import (
	"fmt"
	"sort"
)

// ValidationError identifies a config problem, optionally scoped to a job or
// a repository.
type ValidationError struct {
	Job        string
	Repository string
	Message    string
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
	Repository string
	Message    string
}

func (w Warning) String() string {
	if w.Repository != "" {
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
		if job.Policy == "" {
			res.Errors = append(res.Errors, ValidationError{Job: name, Message: "must reference a policy"})
		} else if _, ok := cfg.Policies[job.Policy]; !ok {
			res.Errors = append(res.Errors, ValidationError{Job: name, Message: fmt.Sprintf("references undefined policy %q", job.Policy)})
		}

		if len(job.Source.Paths) == 0 {
			res.Errors = append(res.Errors, ValidationError{Job: name, Message: "source.paths must be non-empty"})
		}

		if len(job.Repositories) == 0 {
			res.Errors = append(res.Errors, ValidationError{Job: name, Message: "must reference at least one repository"})
		}
		for _, ref := range job.Repositories {
			if _, ok := cfg.Repositories[ref.Name]; !ok {
				res.Errors = append(res.Errors, ValidationError{Job: name, Message: fmt.Sprintf("references undefined repository %q", ref.Name)})
			}
		}
	}

	return res
}
