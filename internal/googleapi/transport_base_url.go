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

var (
	errInvalidAPIBaseURL   = errors.New("want http(s)://host[:port] without path, query, or credentials")
	errCrossOriginRedirect = errors.New("refuse cross-origin redirect from " + apiBaseURLEnv + " request")
)

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

// apiBaseURLHTTPClient returns the client used for every Google API request
// while GOG_GOOGLE_API_BASE_URL is set. It has no credential provider: the
// relay or proxy behind the base URL adds credentials, so gog never sends
// OAuth tokens, service-account tokens, or API keys there.
func apiBaseURLHTTPClient(ctx context.Context) (*http.Client, bool) {
	if ctx == nil {
		return nil, false
	}

	baseURL, _ := ctx.Value(apiBaseURLContextKey{}).(*url.URL)
	if baseURL == nil {
		return nil, false
	}

	transport := &apiBaseURLTransport{base: quotaProjectTransportFromContext(ctx, newBaseTransport()), baseURL: baseURL}

	return &http.Client{Transport: readOnlyTransportFromContext(ctx, NewRetryTransport(transport))}, true
}

// apiBaseURLTransport sends https requests for googleapis.com and its
// subdomains to the configured base URL, keeping path and query. The
// original host goes in X-Forwarded-Host because many APIs share paths
// such as /v1/... across hosts. Redirects are refused unless they stay on
// the origin of the request that received them; callers may replace
// http.Client.CheckRedirect, so the check lives here.
type apiBaseURLTransport struct {
	base    http.RoundTripper
	baseURL *url.URL
}

func (t *apiBaseURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if prev := req.Response; prev != nil && prev.Request != nil &&
		(prev.Request.URL.Scheme != req.URL.Scheme || !strings.EqualFold(prev.Request.URL.Host, req.URL.Host)) {
		if req.Body != nil {
			_ = req.Body.Close()
		}

		return nil, fmt.Errorf("%w: %s", errCrossOriginRedirect, req.URL.Redacted())
	}

	host := strings.ToLower(req.URL.Hostname())
	if req.URL.Scheme != "https" || (host != "googleapis.com" && !strings.HasSuffix(host, ".googleapis.com")) {
		return t.base.RoundTrip(req) //nolint:wrapcheck // pass-through transport
	}

	out := req.Clone(req.Context())
	out.Header.Del("Authorization")
	out.Header.Set("X-Forwarded-Host", req.URL.Host)
	out.URL.Scheme = t.baseURL.Scheme
	out.URL.Host = t.baseURL.Host
	out.Host = t.baseURL.Host

	resp, err := t.base.RoundTrip(out)
	if resp != nil {
		// Redirect targets resolve against, and are compared with, the URL
		// the caller asked for rather than the base URL
		resp.Request = req
	}

	return resp, err //nolint:wrapcheck // pass-through transport
}
