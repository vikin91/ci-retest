# CI Retest

CI Retest reads GitHub Actions and OpenShift CI presubmit results and prints
manual rerun instructions for configured StackRox repositories.

## Run the CLI

Use Go 1.26 or later. Set `GH_TOKEN` or `GITHUB_TOKEN`, or sign in with
`gh auth login --hostname github.com`. The token needs read access to GitHub
Actions, checks, commit statuses, contents, pull requests, and branch rules.

```sh
go run ./cmd/ci-retest --repo stackrox --prs 24136 24135
go run ./cmd/ci-retest --optional --repo stackrox --tags 4.11.8-rc.0,5.0.0-rc.0
```

PR checks cover merge requirements and their prerequisites. Pass `--optional`
to include configured optional jobs. Tags have no PR merge requirements, so
use `--optional` to check them for retry offers. Pass `-v` before the target
list to log GitHub requests to stderr.

For Actions, open the job link and use the rerun control. For Prow, post the
printed `/test` commands on the linked PR after inspecting the failures. The
CLI performs only reads.

## Use the Go package

```go
checker := ciretest.New(ciretest.Config{
    GitHubToken: token,
    HTTPClient:  client,
})
result, err := checker.CheckPR(ctx, "stackrox/stackrox", 24136, ciretest.Options{})
```

Create one checker per session to reuse Prow configuration cached by blob
SHA. Call `CheckTag` for a tag. A result with a read error can still contain
verified offers. Compare the reported SHA with the revision your application
is displaying before offering those retries.

## Update retry rules

The allowlists and denylists in [ci_retest_config.go](ci_retest_config.go)
identify checks by repository, provider, workflow, and job. Keep Konflux
build checks in the Konflux planner's scope.

The [workflow dependency rules](retry_dependency_rules.go) record dependencies
between workflows and audited dependencies for known workflow revisions. For
other revisions, the planner reads the workflow YAML from GitHub.

Run `go test ./...` and `go vet ./...` before submitting changes.
