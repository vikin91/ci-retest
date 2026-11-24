package ciretest

import (
	"context"
	"encoding/json"
	"fmt"
)

type (
	commitCheckRun struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		URL        string `json:"html_url"`
		HeadSHA    string `json:"head_sha"`
		App        struct {
			ID   int64  `json:"id"`
			Slug string `json:"slug"`
		} `json:"app"`
		Suite struct {
			ID int64 `json:"id"`
		} `json:"check_suite"`
	}
	commitStatus struct {
		Context string `json:"context"`
		State   string `json:"state"`
		URL     string `json:"target_url"`
		Creator struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"creator"`
	}
	workflowRun struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		WorkflowID int64  `json:"workflow_id"`
		SuiteID    int64  `json:"check_suite_id"`
		SHA        string `json:"head_sha"`
		Event      string `json:"event"`
		Ref        string `json:"head_branch"`
	}
)

func (run commitCheckRun) passed() bool {
	switch run.Conclusion {
	case "success", "neutral", "skipped":
		return true
	default:
		return false
	}
}

func (run commitCheckRun) failed() bool {
	switch run.Conclusion {
	case "failure", "error", "cancelled", "timed_out", "action_required", "startup_failure", "stale":
		return true
	default:
		return false
	}
}

func (c *Client) readCommitCheckRuns(ctx context.Context, repo, sha string) ([]commitCheckRun, error) {
	var runs []commitCheckRun
	for page := 1; page <= maxPages; page++ {
		var response struct {
			Total int              `json:"total_count"`
			Runs  []commitCheckRun `json:"check_runs"`
		}
		endpoint := fmt.Sprintf("%s/commits/%s/check-runs?per_page=100&page=%d", repoPath(repo), segment(sha), page)
		if err := c.get(ctx, endpoint, &response); err != nil {
			return runs, err
		}
		runs = append(runs, response.Runs...)
		if page*100 >= response.Total {
			return runs, nil
		}
	}
	return runs, fmt.Errorf("GitHub check run pagination limit reached")
}

func (c *Client) readCommitStatuses(ctx context.Context, repo, sha string) ([]commitStatus, error) {
	var statuses []commitStatus
	err := c.pages(ctx, repoPath(repo)+"/commits/"+segment(sha)+"/statuses", func(raw json.RawMessage) error {
		var status commitStatus
		if err := json.Unmarshal(raw, &status); err != nil {
			return fmt.Errorf("malformed commit status")
		}
		statuses = append(statuses, status)
		return nil
	})
	return statuses, err
}
