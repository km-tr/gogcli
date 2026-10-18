package googleapi

import (
	"context"
	"net/http"
	"testing"
)

type recordingRoundTripper struct {
	req *http.Request
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.req = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func TestAPIBaseURLTransportRewritesOnlyGoogleAPIs(t *testing.T) {
	ctx, err := WithAPIBaseURL(context.Background(), "http://127.0.0.1:18080/")
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		in, want, forwardedHost string
	}{
		{"https://gmail.googleapis.com/gmail/v1/users/me/messages?q=a%20b", "http://127.0.0.1:18080/gmail/v1/users/me/messages?q=a%20b", "gmail.googleapis.com"},
		{"https://GoogleAPIs.com/v1/x", "http://127.0.0.1:18080/v1/x", "GoogleAPIs.com"},
		{"https://googleapis.com.evil.test/v1/x", "https://googleapis.com.evil.test/v1/x", ""},
		{"https://lh3.googleusercontent.com/x", "https://lh3.googleusercontent.com/x", ""},
		{"http://www.googleapis.com/v1/x", "http://www.googleapis.com/v1/x", ""},
	} {
		rec := &recordingRoundTripper{}
		transport := apiBaseURLTransportFromContext(ctx, rec)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, tt.in, nil)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()

		if got := rec.req.URL.String(); got != tt.want {
			t.Errorf("%s: URL = %q, want %q", tt.in, got, tt.want)
		}

		if got := rec.req.Header.Get("X-Forwarded-Host"); got != tt.forwardedHost {
			t.Errorf("%s: X-Forwarded-Host = %q, want %q", tt.in, got, tt.forwardedHost)
		}

		if req.URL.String() != tt.in {
			t.Errorf("%s: original request mutated to %q", tt.in, req.URL)
		}
	}
}

func TestWithAPIBaseURLRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{
		"127.0.0.1:18080",
		"localhost",
		"ftp://127.0.0.1",
		"http://",
		"http://127.0.0.1/prefix",
		"http://127.0.0.1/?x=1",
		"http://user:pass@127.0.0.1",
	} {
		if _, err := WithAPIBaseURL(context.Background(), raw); err == nil {
			t.Errorf("WithAPIBaseURL(%q) succeeded, want error", raw)
		}
	}
}
