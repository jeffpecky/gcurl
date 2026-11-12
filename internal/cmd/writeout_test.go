package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestRenderWriteOut pins the curl --write-out variable expansion.
func TestRenderWriteOut(t *testing.T) {
	u, err := url.Parse("http://example.com:8080/path")
	if err != nil {
		t.Fatal(err)
	}

	resp := &http.Response{
		StatusCode:    404,
		ProtoMajor:    1,
		ContentLength: 42,
		Header:        http.Header{"Content-Type": {"text/plain"}},
		Request:       &http.Request{URL: u},
	}

	got := renderWriteOut(
		"%{http_code}|%{response_code}|%{url_effective}|%{size_download}|%{scheme}|"+
			"%{content_type}|%{remote_ip}|%{remote_port}|%{http_version}|%{time_connect}",
		resp, time.Now(), 250*time.Millisecond,
	)

	want := strings.Join([]string{
		"404", "404", "http://example.com:8080/path", "42", "http",
		"text/plain", "0.0.0.0", "0", "1", "0.250000",
	}, "|")

	// time_total is wall-clock and intentionally not asserted.
	if got != want {
		t.Fatalf("renderWriteOut = %q, want %q", got, want)
	}
}

// TestExitCodeFor pins the curl exit code mapping.
func TestExitCodeFor(t *testing.T) {
	cases := map[string]int{
		"the requested URL returned error 404": 22,
		"dial tcp: no such host":               6,
		"context deadline exceeded":            28,
		"dial tcp: connection refused":         7,
		"too many redirects (limit 50)":        47,
		"something else":                       1,
	}

	for msg, want := range cases {
		err := fmt.Errorf("%w %s", errFail, msg)
		if !strings.Contains(msg, "requested URL returned error") {
			err = errors.New(msg)
		}
		if got := exitCodeFor(err); got != want {
			t.Errorf("exitCodeFor(%q) = %d, want %d", msg, got, want)
		}
	}
}
