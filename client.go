package ciretest

import (
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vikin91/ci-retest/internal/githubread"
)

const maxPages = 20

type (
	// Config supplies credentials and the HTTP client for read-only GitHub requests.
	Config struct {
		GitHubToken string
		BaseURL     string
		HTTPClient  *http.Client
	}

	// Client checks GitHub Actions and Prow without executing retry offers.
	Client struct {
		baseURL, token string
		http           *http.Client
		backoff        readBackoff
		prowFiles      sync.Map
	}
)

// requestError separates safe UI causes from endpoint diagnostics.
type requestError struct {
	diagnostic, problem string
	cause               error
	statusCode          int
}

// Error preserves endpoint diagnostics for the activity log.
func (e *requestError) Error() string { return e.diagnostic }

// Unwrap preserves cancellation and deadline classification.
func (e *requestError) Unwrap() error { return e.cause }

func requestProblem(err error) string {
	if failure, ok := errors.AsType[*requestError](err); ok {
		return failure.problem
	}
	return err.Error()
}

func transportProblem(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Request timed out."
	case errors.Is(err, context.Canceled):
		return "Request canceled."
	default:
		return safeTransportCause(err).Error() + "."
	}
}

// New constructs a read-only client. Reuse it to cache Prow files by blob SHA.
func New(config Config) *Client {
	client := http.Client{Timeout: 30 * time.Second}
	if config.HTTPClient != nil {
		client = *config.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		baseURL: strings.TrimRight(cmp.Or(config.BaseURL, "https://api.github.com"), "/"),
		token:   config.GitHubToken,
		http:    &client,
	}
}

func (c *Client) get(ctx context.Context, endpoint string, dst any) error {
	if err := c.backoff.problem(); err != nil {
		return err
	}
	const method = http.MethodGet
	requestURL := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return fmt.Errorf("build GitHub %s %s request failed", method, safeEndpoint(requestURL, c.token))
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		cause := safeTransportCause(err)
		return &requestError{diagnostic: fmt.Sprintf("GitHub %s %s unavailable: %s", method, safeEndpoint(requestURL, c.token), cause), problem: transportProblem(err), cause: cause}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		failure := &requestError{
			diagnostic: fmt.Sprintf("GitHub %s %s rejected (HTTP %d): %s", method, safeEndpoint(requestURL, c.token), resp.StatusCode, httpStatusHint(resp)),
			problem:    fmt.Sprintf("%s (HTTP %d).", httpStatusHint(resp), resp.StatusCode),
			statusCode: resp.StatusCode,
		}
		if rateLimited(resp) {
			c.backoff.record(resp, failure)
		}
		return failure
	}
	return c.decodeResponse(ctx, method, endpoint, json.NewDecoder(io.LimitReader(resp.Body, 8<<20)), dst)
}

func (c *Client) decodeResponse(ctx context.Context, method, endpoint string, decoder *json.Decoder, dst any) error {
	if err := decoder.Decode(dst); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		_, networkFailure := errors.AsType[net.Error](err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || networkFailure {
			return &requestError{diagnostic: fmt.Sprintf("GitHub %s %s response unavailable: %s", method, safeEndpoint(c.baseURL+endpoint, c.token), safeTransportCause(err)), problem: transportProblem(err), cause: safeTransportCause(err)}
		}
		return &requestError{diagnostic: fmt.Sprintf("GitHub %s %s response malformed", method, safeEndpoint(c.baseURL+endpoint, c.token)), problem: "GitHub returned an invalid JSON response."}
	}
	return nil
}

func safeEndpoint(rawURL, token string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "/"
	}
	return githubread.Resource(parsed, token)
}

func httpStatusHint(resp *http.Response) string {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return "Authentication failed; GitHub rejected the token"
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
			return "GitHub rate limit reached"
		}
		return "Access denied by token permissions or repository policy"
	case http.StatusNotFound:
		return "Not found or inaccessible with the current GitHub token"
	case http.StatusTooManyRequests:
		return "GitHub rate limit reached"
	default:
		if resp.StatusCode >= 500 {
			return "GitHub server error"
		}
		return "GitHub rejected the request"
	}
}

func safeTransportCause(err error) error {
	if deferred, ok := errors.AsType[interface {
		error
		GitHubReadDeferral() string
	}](err); ok {
		return errors.New(deferred.GitHubReadDeferral())
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return errors.New("DNS lookup failed")
	}
	if _, ok := errors.AsType[*tls.RecordHeaderError](err); ok {
		return errors.New("TLS handshake failed")
	}
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return errors.New("TLS handshake failed")
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return errors.New("TLS certificate verification failed")
	}
	if _, ok := errors.AsType[x509.CertificateInvalidError](err); ok {
		return errors.New("TLS certificate verification failed")
	}
	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return errors.New("TLS certificate verification failed")
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return errors.New("network timeout")
	}
	return errors.New("network failure")
}

func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func repoPath(repo string) string { return "/repos/" + repo }
func segment(s string) string     { return url.PathEscape(s) }

func (c *Client) pages(ctx context.Context, endpoint string, consume func(json.RawMessage) error) error {
	for page := 1; page <= maxPages; page++ {
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		var data []json.RawMessage
		if err := c.get(ctx, fmt.Sprintf("%s%sper_page=100&page=%d", endpoint, sep, page), &data); err != nil {
			return err
		}
		if data == nil {
			return fmt.Errorf("GitHub returned an invalid page")
		}
		for _, item := range data {
			if err := consume(item); err != nil {
				return err
			}
		}
		if len(data) < 100 {
			return nil
		}
	}
	return fmt.Errorf("GitHub result exceeded pagination limit")
}
