package githubread

import (
	"cmp"
	"net/url"
	"strings"
)

// Resource returns a single escaped path and redacts credentials from diagnostics.
func Resource(u *url.URL, credential string) string {
	path := cmp.Or(u.EscapedPath(), "/")
	if credential != "" {
		path = strings.ReplaceAll(path, url.PathEscape(credential), "[redacted]")
		path = strings.ReplaceAll(path, credential, "[redacted]")
	}
	lower := strings.ToLower(path)
	for _, marker := range []string{"token", "secret", "password", "credential", "authorization", "api_key", "apikey"} {
		if strings.Contains(lower, marker) {
			return "/[redacted]"
		}
	}
	return path
}
