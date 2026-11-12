package output

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCurlVerbose_h2Format pins curl's HTTP/2 verbose formatting: no minor
// version in the protocol token and lowercase header names.
func TestCurlVerbose_h2Format(t *testing.T) {
	u, err := url.Parse("https://example.com/api")
	require.NoError(t, err)

	req := &http.Request{
		Method:     http.MethodGet,
		URL:        u,
		Host:       "example.com",
		Proto:      "HTTP/2",
		ProtoMajor: 2,
		Header:     http.Header{"Accept": {"*/*"}},
	}

	var buf strings.Builder
	out := &Output{verbose: true, logFileWriter: &buf}
	out.CurlRequest(req, []string{"accept: */*"})

	got := buf.String()
	require.Contains(t, got, "> GET /api HTTP/2\n")
	require.Contains(t, got, "> host: example.com\n")
	require.Contains(t, got, "> accept: */*\n")
	require.NotContains(t, got, "HTTP/2.0")
}

// TestCurlVerbose_h1Format checks HTTP/1.1 keeps its proto and header case.
func TestCurlVerbose_h1Format(t *testing.T) {
	u, err := url.Parse("https://example.com/api")
	require.NoError(t, err)

	req := &http.Request{
		Method:     http.MethodGet,
		URL:        u,
		Host:       "example.com",
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{"Accept": {"*/*"}},
	}

	var buf strings.Builder
	out := &Output{verbose: true, logFileWriter: &buf}
	out.CurlRequest(req, nil)

	got := buf.String()
	require.Contains(t, got, "> GET /api HTTP/1.1\n")
	require.Contains(t, got, "> Host: example.com\n")
	require.Contains(t, got, "> Accept: */*\n")
}
