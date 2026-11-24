package ciretest

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	pathpkg "path"
	"strings"
)

type branchResponse struct {
	Protection struct {
		RequiredStatusChecks struct {
			EnforcementLevel string `json:"enforcement_level"`
		} `json:"required_status_checks"`
	} `json:"protection"`
}

// file reads a repository file at an immutable commit SHA.
func (c *Client) file(ctx context.Context, repo, sha, filePath string) ([]byte, error) {
	if !ValidRepo(repo) || sha == "" || filePath == "" || strings.HasPrefix(filePath, "/") || pathpkg.Clean(filePath) != filePath || filePath == ".." || strings.HasPrefix(filePath, "../") {
		return nil, fmt.Errorf("invalid GitHub file identity")
	}
	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	parts := strings.Split(filePath, "/")
	for i := range parts {
		parts[i] = segment(parts[i])
	}
	endpoint := repoPath(repo) + "/contents/" + strings.Join(parts, "/") + "?ref=" + url.QueryEscape(sha)
	if err := c.get(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	if result.Encoding != "base64" {
		return nil, fmt.Errorf("GitHub file encoding unsupported")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(result.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("GitHub file content malformed")
	}
	return decoded, nil
}
