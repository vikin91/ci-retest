// Package ciretest discovers manual GitHub Actions and Prow retries.
package ciretest

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ManualRetryWarning reminds engineers to inspect the failure before retrying.
const ManualRetryWarning = "This tool cannot tell flakes from true failures. Inspect each failure before retrying."

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type (
	// Options controls which configured jobs the planner considers.
	Options struct {
		IncludeOptional bool
	}

	// Target identifies one PR or tag in a GitHub repository.
	Target struct {
		Repo string
		PR   int
		Tag  string
	}

	// Result contains offers backed by SHA. A read error can accompany partial results.
	Result struct {
		Target      Target
		SHA         string
		URL         string
		Jobs        []ActionsJob
		Prow        []ProwRetry
		Warnings    []string
		OtherChecks []string
	}

	// ActionsJob identifies a verified job that an engineer can rerun.
	ActionsJob struct {
		ID                      int64
		Name, URL, WorkflowName string
	}

	// ProwRetry supplies the configured presubmit command for a failed status context.
	ProwRetry struct{ Context, Command, URL string }
)

// CheckPR offers retries for a pull request's current revision.
func (c *Client) CheckPR(ctx context.Context, repo string, pr int, opts Options) (Result, error) {
	return c.check(ctx, Target{Repo: repo, PR: pr}, opts.IncludeOptional)
}

// CheckTag checks the tagged commit. Tags have no merge requirements.
func (c *Client) CheckTag(ctx context.Context, repo, tag string, opts Options) (Result, error) {
	return c.check(ctx, Target{Repo: repo, Tag: tag}, opts.IncludeOptional)
}

// ValidRepo accepts GitHub repository names in owner/name form.
func ValidRepo(repo string) bool {
	return repoPattern.MatchString(repo) && !strings.Contains(repo, "..")
}

// valid requires exactly one target kind.
func (t Target) valid() bool {
	if !ValidRepo(t.Repo) {
		return false
	}
	if t.PR > 0 {
		return t.Tag == ""
	}
	return t.PR == 0 && ValidRetryTag(t.Tag)
}

// String renders a target for display.
func (t Target) String() string {
	if t.PR > 0 {
		return fmt.Sprintf("%s#%d", t.Repo, t.PR)
	}
	return t.Repo + "@" + t.Tag
}

// RerunCommand formats a command for an engineer to rerun the verified job.
func (j ActionsJob) RerunCommand(repo string) string {
	return fmt.Sprintf("gh api --method POST repos/%s/actions/jobs/%d/rerun", repo, j.ID)
}

// ProwComment combines the verified presubmit commands into a copyable comment.
func (r Result) ProwComment() string {
	lines := make([]string, len(r.Prow))
	for i, job := range r.Prow {
		lines[i] = job.Command
	}
	return strings.Join(lines, "\n")
}
