package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/vikin91/ci-retest"
	"github.com/vikin91/ci-retest/internal/githubread"
)

// Run executes the read-only command with the given arguments and streams.
func Run(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	return run(ctx, args, out, diagnostics, newReader)
}

type (
	cliOptions struct {
		optional bool
		debugLog bool
	}

	retryReader func(context.Context, ciretest.Target, bool) (ciretest.Result, error)
)

func newReader(ctx context.Context, log *requestLog) (retryReader, error) {
	token := githubread.AuthToken(ctx)
	if token == "" {
		return nil, errors.New("set GH_TOKEN or GITHUB_TOKEN, or sign in with gh auth login --hostname github.com")
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: log,
	}
	return retryReaderFor(ciretest.New(ciretest.Config{GitHubToken: token, HTTPClient: client})), nil
}

func targets(args []string, diagnostics io.Writer, options *cliOptions) ([]ciretest.Target, error) {
	fs := flag.NewFlagSet("ci-retest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&options.optional, "optional", false, "include configured optional CI jobs")
	fs.BoolVar(&options.debugLog, "v", false, "show Debug messages on stderr")
	repo := fs.String("repo", "stackrox", "repository")
	prs := fs.String("prs", "", "PR numbers")
	tags := fs.String("tags", "", "tag names")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(diagnostics, `Usage: ci-retest [--optional] [--repo NAME|OWNER/REPO] --prs 24136 24135
       ci-retest [--optional] [--repo NAME|OWNER/REPO] --tags 4.11.8-rc.0,5.0.0-rc.0

Choose one list and put it last. Spaces and commas are accepted. Short
repository names use the stackrox owner. Default: stackrox/stackrox.
Prints supported GitHub Actions reruns and Prow retry comments.
By default CI retries cover PR merge requirements and their
prerequisites. Add --optional to include configured optional CI jobs on PRs or tags.
Pass -v before the target list to show Debug messages on stderr.
GitHub authentication: GH_TOKEN,
GITHUB_TOKEN, or a saved gh login. Read access to Actions, checks,
commit statuses, contents and pull requests is needed.`)
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	var hasPRs, hasTags bool
	fs.Visit(func(f *flag.Flag) {
		hasPRs = hasPRs || f.Name == "prs"
		hasTags = hasTags || f.Name == "tags"
	})
	if hasPRs == hasTags {
		return nil, errors.New("choose exactly one of --prs or --tags")
	}
	if !strings.Contains(*repo, "/") {
		*repo = "stackrox/" + *repo
	}
	if !ciretest.ValidRepo(*repo) {
		return nil, errors.New("invalid repository")
	}
	list := *tags
	if hasPRs {
		list = *prs
	}
	values := strings.FieldsFunc(strings.Join(append([]string{list}, fs.Args()...), " "), func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	var result []ciretest.Target
	seen := make(map[ciretest.Target]bool)
	for _, value := range values {
		t := ciretest.Target{Repo: *repo}
		if hasPRs {
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("invalid PR number %q", value)
			}
			t.PR = n
		} else {
			t.Tag = value
		}
		if hasTags && !ciretest.ValidRetryTag(t.Tag) {
			return nil, fmt.Errorf("invalid target %q", value)
		}
		if !seen[t] {
			result = append(result, t)
			seen[t] = true
		}
	}
	if len(result) == 0 {
		return nil, errors.New("empty target list")
	}
	return result, nil
}

func run(ctx context.Context, args []string, out, diagnostics io.Writer, reader func(context.Context, *requestLog) (retryReader, error)) int {
	var options cliOptions
	targets, err := targets(args, diagnostics, &options)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, err)
		return 2
	}
	readRetries, err := reader(ctx, &requestLog{output: diagnostics, enabled: options.debugLog})
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, err)
		return 1
	}
	code := 0
	for i, t := range targets {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		report, err := readRetries(ctx, t, options.optional)
		if err != nil {
			report.Warnings = append(report.Warnings, err.Error())
			code = 1
		}
		_, _ = fmt.Fprintf(out, "---\n%s\n", report.URL)
		if t.Tag != "" {
			_, _ = fmt.Fprintf(out, "Tag: %s\n", t.Tag)
		}
		switch {
		case len(report.Jobs)+len(report.Prow) > 0:
			_, _ = fmt.Fprintln(out, "\nAvailable retries")
		case len(report.OtherChecks)+len(report.Warnings) > 0:
			_, _ = fmt.Fprintln(out, "\nNext action: Investigate")
		default:
			_, _ = fmt.Fprintln(out, "\nNext action: None")
		}
		for j, job := range report.Jobs {
			if j > 0 {
				_, _ = fmt.Fprintln(out)
			}
			if job.WorkflowName != "" {
				_, _ = fmt.Fprintf(out, "%s / ", job.WorkflowName)
			}
			_, _ = fmt.Fprintf(out, "%s\n%s\n", job.Name, job.URL)
			_, _ = fmt.Fprintln(out, job.RerunCommand(t.Repo))
		}
		if len(report.Prow) > 0 {
			if len(report.Jobs) > 0 {
				_, _ = fmt.Fprintln(out)
			}
			_, _ = fmt.Fprintf(out, "Failed Prow checks: post this comment on %s\n%s\n", report.URL, report.ProwComment())
		}
		if len(report.OtherChecks) > 0 {
			_, _ = fmt.Fprintln(out, "\nUnsupported checks")
			for _, check := range report.OtherChecks {
				_, _ = fmt.Fprintln(out, check)
			}
		}
		if len(report.Jobs)+len(report.Prow) > 0 {
			report.Warnings = append(report.Warnings, ciretest.ManualRetryWarning)
		}
		if len(report.Warnings) > 0 {
			_, _ = fmt.Fprintln(out, "\nWarnings:")
			for _, w := range report.Warnings {
				_, _ = fmt.Fprintln(out, w)
			}
		}
		if ctx.Err() != nil {
			return 1
		}
	}
	return code
}

func retryReaderFor(client *ciretest.Client) retryReader {
	return func(ctx context.Context, target ciretest.Target, optional bool) (ciretest.Result, error) {
		opts := ciretest.Options{IncludeOptional: optional}
		if target.PR > 0 {
			return client.CheckPR(ctx, target.Repo, target.PR, opts)
		}
		return client.CheckTag(ctx, target.Repo, target.Tag, opts)
	}
}
