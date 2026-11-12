package client

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeffpecky/gcurl/internal/config"
	"github.com/jeffpecky/gcurl/internal/output"
)

// newTestUTransport builds a uTransport that dials plain TCP directly, so the
// test never touches DNS or TLS.
func newTestUTransport(t *testing.T, location bool) *uTransport {
	t.Helper()

	out := output.NewOutputWithWriters(&bytes.Buffer{}, &bytes.Buffer{}, false, false)

	return &uTransport{d: &clientDialer{
		cfg: &config.Config{Location: location},
		out: out,
		dial: func(network, addr string) (net.Conn, error) {
			//nolint:gosec // G704: the address comes from httptest, not user input
			return net.Dial(network, addr)
		},
	}}
}

// TestRoundTripFollowsPlainHTTPRedirect pins curl's -L behaviour on plain
// HTTP: the redirect check must run for non-TLS requests too.
func TestRoundTripFollowsPlainHTTPRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/redirect", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	resp, err := newTestUTransport(t, true).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (redirect not followed)", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Request.URL.Path; got != "/final" {
		t.Fatalf("final path = %q, want /final", got)
	}
}

// TestRoundTripDoesNotFollowWithoutLocation makes sure the check is gated by
// -L rather than always following.
func TestRoundTripDoesNotFollowWithoutLocation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/redirect", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	resp, err := newTestUTransport(t, false).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
}
