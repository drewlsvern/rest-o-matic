package config

import "fmt"

// MergeRetention deep-merges override onto base, key by key: keys present in
// override replace base's value, keys absent from override keep base's
// value. Neither argument is mutated.
func MergeRetention(base, override Retention) Retention {
	merged := make(Retention, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

// ResolveRetention applies the three-level cascade: policy retention, then
// a job-level override merged on top, then a per-repository override merged
// on top of that. Each level only needs to specify the keys it changes.
func ResolveRetention(policy, jobOverride, repoOverride Retention) Retention {
	return MergeRetention(MergeRetention(policy, jobOverride), repoOverride)
}

// EffectiveRetention resolves the retention that applies to a specific
// (job, repository) pair, per the config spec's override cascade.
func (cfg *Config) EffectiveRetention(jobName, repoName string) (Retention, error) {
	job, ok := cfg.Backups[jobName]
	if !ok {
		return nil, fmt.Errorf("no such job %q", jobName)
	}
	policy, ok := cfg.Policies[job.Policy]
	if !ok {
		return nil, fmt.Errorf("job %q references undefined policy %q", jobName, job.Policy)
	}

	var repoOverride Retention
	found := false
	for _, ref := range job.Repositories {
		if ref.Name == repoName {
			repoOverride = ref.Retention
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("job %q does not reference repository %q", jobName, repoName)
	}

	return ResolveRetention(policy.Retention, job.Retention, repoOverride), nil
}

// EffectiveSchedule returns the schedule a job runs on, resolved from its
// policy. There is no job-level schedule override in this version.
func (cfg *Config) EffectiveSchedule(jobName string) (string, error) {
	job, ok := cfg.Backups[jobName]
	if !ok {
		return "", fmt.Errorf("no such job %q", jobName)
	}
	policy, ok := cfg.Policies[job.Policy]
	if !ok {
		return "", fmt.Errorf("job %q references undefined policy %q", jobName, job.Policy)
	}
	return policy.Schedule, nil
}
