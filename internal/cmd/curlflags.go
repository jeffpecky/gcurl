package cmd

// This file implements the curl flags that need per-request behaviour rather
// than just configuration: --fail, --retry, --limit-rate, -D/--dump-header and
// -C/--continue-at.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/jeffpecky/gcurl/internal/config"
	"github.com/jeffpecky/gcurl/internal/output"
)

// errFail is returned for -f/--fail so Main can map it to curl's exit code 22.
var errFail = errors.New("the requested URL returned error")

// doRequest performs the request, honouring --retry.  Only transient failures
// are retried: connection errors and the statuses curl itself retries on.
func doRequest(rt http.RoundTripper, req *http.Request, cfg *config.Config) (*http.Response, error) {
	attempts := cfg.Retry + 1
	if attempts < 1 {
		attempts = 1
	}

	var (
		resp *http.Response
		err  error
	)

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			// A consumed body cannot be replayed; GetBody gives a fresh copy.
			body, bErr := req.GetBody()
			if bErr != nil {
				return nil, bErr
			}
			req.Body = body
		}

		resp, err = rt.RoundTrip(req)

		if err == nil && !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		if err == nil {
			resp.Body.Close()
		}

		if attempt+1 < attempts {
			// curl backs off linearly between attempts (1s, 2s, 3s...).
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}

	return resp, err
}

// isRetryableStatus reports whether curl's --retry would retry this status.
func isRetryableStatus(code int) bool {
	return code == http.StatusRequestTimeout ||
		code == http.StatusTooManyRequests ||
		code >= 500
}

// failOnHTTPError implements -f/--fail: HTTP >= 400 becomes an error and the
// response body is discarded, like curl.
func failOnHTTPError(cfg *config.Config, resp *http.Response) error {
	if !cfg.Fail || resp.StatusCode < 400 {
		return nil
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return fmt.Errorf("%w %d", errFail, resp.StatusCode)
}

// handleFail implements -f/--fail: on an HTTP error it prints what curl still
// prints (-v/info, -D headers, -i headers) and returns errFail for exit code 22.
func handleFail(cfg *config.Config, out *output.Output, resp *http.Response) error {
	failErr := failOnHTTPError(cfg, resp)
	if failErr == nil {
		return nil
	}

	out.CurlResponse(resp)

	if cfg.DumpHeader != "" {
		_ = dumpHeaders(cfg.DumpHeader, resp)
	}
	if cfg.Include {
		out.Write(resp, nil, cfg)
	}

	return failErr
}

// dumpHeaders implements -D/--dump-header: the status line and headers go to
// the given file (or stdout when path is "-").
func dumpHeaders(path string, resp *http.Response) error {
	w := io.Writer(os.Stdout)
	if path != "-" {
		//nolint:gosec // G703: path is the operator's own -D file, like curl
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("cannot open header dump file %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()

		w = f
	}

	// Same rendering as -i/--include: h2 names lowercase, sorted, CRLF.
	//nolint:gosec // G705: raw headers written to a file or terminal, never HTML
	_, err := io.WriteString(w, resp.Proto+" "+resp.Status+"\r\n"+output.HeaderLines(resp))

	return err
}

// limitRateReader throttles reads from the response body to cfg.LimitRate
// bytes per second (--limit-rate).
type limitRateReader struct {
	r       io.Reader
	rate    int64
	started time.Time
	read    int64
}

// Read implements io.Reader for *limitRateReader, sleeping when the transfer
// runs ahead of the configured rate.
func (l *limitRateReader) Read(p []byte) (n int, err error) {
	n, err = l.r.Read(p)
	if n == 0 {
		return n, err
	}

	if l.started.IsZero() {
		l.started = time.Now()
	}
	l.read += int64(n)

	// The bytes are allowed to be read by elapsed * rate; sleep the excess.
	allowed := time.Duration(float64(l.read) / float64(l.rate) * float64(time.Second))
	if behind := allowed - time.Since(l.started); behind > 0 {
		time.Sleep(behind)
	}

	return n, err
}

// limitRate wraps body when --limit-rate is set.
func limitRate(cfg *config.Config, body io.ReadCloser) io.ReadCloser {
	if cfg.LimitRate <= 0 || body == nil {
		return body
	}

	return struct {
		io.Reader
		io.Closer
	}{Reader: &limitRateReader{r: body, rate: cfg.LimitRate}, Closer: body}
}
