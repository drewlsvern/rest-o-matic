package config

import "fmt"

// ValidationError identifies a config problem, optionally scoped to a job.
type ValidationError struct {
	Job     string
	Message string
}

func (e ValidationError) Error() string {
	if e.Job != "" {
		return fmt.Sprintf("job %q: %s", e.Job, e.Message)
	}
	return e.Message
}

// Validate checks cross-references and required fields across the whole
// config, returning every problem found rather than stopping at the first.
func Validate(cfg *Config) []error {
	var errs []error

	for name, job := range cfg.Backups {
		if job.Policy == "" {
			errs = append(errs, ValidationError{Job: name, Message: "must reference a policy"})
		} else if _, ok := cfg.Policies[job.Policy]; !ok {
			errs = append(errs, ValidationError{Job: name, Message: fmt.Sprintf("references undefined policy %q", job.Policy)})
		}

		if len(job.Source.Paths) == 0 {
			errs = append(errs, ValidationError{Job: name, Message: "source.paths must be non-empty"})
		}

		if len(job.Repositories) == 0 {
			errs = append(errs, ValidationError{Job: name, Message: "must reference at least one repository"})
		}
		for _, ref := range job.Repositories {
			if _, ok := cfg.Repositories[ref.Name]; !ok {
				errs = append(errs, ValidationError{Job: name, Message: fmt.Sprintf("references undefined repository %q", ref.Name)})
			}
		}
	}

	return errs
}
