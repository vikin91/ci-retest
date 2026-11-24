package cli

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vikin91/ci-retest/internal/githubread"
)

type requestLog struct {
	output  io.Writer
	enabled bool
}

// CallingAPI sends request metadata to diagnostics in verbose mode.
func (l *requestLog) CallingAPI(system, method, resource string) {
	if l.enabled {
		_, _ = fmt.Fprintf(l.output, "%s Debug: Calling API: %s %s %s\n", time.Now().Format(time.RFC3339), system, method, resource)
	}
}

// RoundTrip logs the outbound path without query parameters or credentials.
func (l *requestLog) RoundTrip(req *http.Request) (*http.Response, error) {
	_, token, _ := strings.Cut(req.Header.Get("Authorization"), " ")
	l.CallingAPI("GitHub", req.Method, githubread.Resource(req.URL, token))
	return http.DefaultTransport.RoundTrip(req)
}
