package config

import "sort"

// JobsReferencing returns the names of every job whose repositories list
// includes repoName, sorted for deterministic output. Used to detect when a
// repository is shared by more than one job (e.g. for exec's tag-safety
// gate) and to name those jobs in messages.
func (cfg *Config) JobsReferencing(repoName string) []string {
	var jobs []string
	for name, job := range cfg.Backups {
		for _, ref := range job.Repositories {
			if ref.Name == repoName {
				jobs = append(jobs, name)
				break
			}
		}
	}
	sort.Strings(jobs)
	return jobs
}
