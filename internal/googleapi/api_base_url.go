package googleapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const apiBaseURLEnv = "GOG_GOOGLE_API_BASE_URL"

type apiBaseURLContextKey struct{}

var errInvalidAPIBaseURL = errors.New("want http(s)://host[:port] without path, query, or credentials")

// WithAPIBaseURL validates a GOG_GOOGLE_API_BASE_URL value and stores it in
// ctx. A blank value leaves ctx unchanged. Path prefixes are rejected so the
// original request path is forwarded unchanged.
func WithAPIBaseURL(ctx context.Context, raw string) (context.Context, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ctx, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ctx, fmt.Errorf("invalid %s %q: %w", apiBaseURLEnv, raw, err)
	}

	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return ctx, fmt.Errorf("invalid %s %q: %w", apiBaseURLEnv, raw, errInvalidAPIBaseURL)
	}

	return context.WithValue(ctx, apiBaseURLContextKey{}, &url.URL{Scheme: u.Scheme, Host: u.Host}), nil
}

// apiBaseURLTransport sends https requests for googleapis.com and its
// subdomains to the configured base URL, keeping path and query. The
// original host goes in X-Forwarded-Host because many APIs share paths
// such as /v1/... across hosts.
type apiBaseURLTransport struct {
	base    http.RoundTripper
	baseURL *url.URL
}

func apiBaseURLTransportFromContext(ctx context.Context, base http.RoundTripper) http.RoundTripper {
	if ctx == nil {
		return base
	}

	baseURL, _ := ctx.Value(apiBaseURLContextKey{}).(*url.URL)
	if baseURL == nil {
		return base
	}

	return &apiBaseURLTransport{base: base, baseURL: baseURL}
}

func (t *apiBaseURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Hostname())
	if req.URL.Scheme != "https" || (host != "googleapis.com" && !strings.HasSuffix(host, ".googleapis.com")) {
		return t.base.RoundTrip(req) //nolint:wrapcheck // pass-through transport
	}

	out := req.Clone(req.Context())
	out.Header.Set("X-Forwarded-Host", req.URL.Host)
	out.URL.Scheme = t.baseURL.Scheme
	out.URL.Host = t.baseURL.Host
	out.Host = t.baseURL.Host

	return t.base.RoundTrip(out) //nolint:wrapcheck // pass-through transport
}
