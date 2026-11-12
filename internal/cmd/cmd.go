// Package cmd is the entry point of the tool.
package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeffpecky/gcurl/internal/appversion"
	"github.com/jeffpecky/gcurl/internal/client"
	"github.com/jeffpecky/gcurl/internal/client/websocket"
	"github.com/jeffpecky/gcurl/internal/config"
	"github.com/jeffpecky/gcurl/internal/output"
	goFlags "github.com/jessevdk/go-flags"
)

// Main is the entry point for the command-line tool.
func Main() {
	// curl compat: --version and -V print the version.  -v is verbose — it
	// must NOT be treated as --version (curl's own version short flag is -V).
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-V") {
		fmt.Printf("gcurl version: %s\n", appversion.Version())

		os.Exit(0)
	}

	cfg, err := config.ParseConfig(os.Args[1:])
	var flagErr *goFlags.Error
	if errors.As(err, &flagErr) && flagErr.Type == goFlags.ErrHelp {
		// This is a special case when we exit process here as we received
		// --help.
		os.Exit(0)
	}

	if err != nil {
		if !cfg.Silent {
			_, _ = fmt.Fprintf(os.Stderr, "Failed to parse args: %v", err)
		}

		os.Exit(1)
	}

	out, err := output.NewOutput(cfg.OutputPath, cfg.Verbose, cfg.OutputJSON)
	if err != nil {
		panic(err)
	}

	if err := Run(cfg, out); err != nil {
		if !cfg.Silent {
			out.Error("%v", err)
		}

		os.Exit(exitCodeFor(err))
	}
}

// exitCodeFor maps request errors to curl's exit codes (see
// https://everything.curl.dev/cmdline/exitcode.html).  Only the codes that
// can arise from this tool's operations are mapped; anything else is
// curl's generic failure code 1.
func exitCodeFor(err error) (code int) {
	errStr := err.Error()

	switch {
	// 22: HTTP page not retrieved (-f/--fail).
	case errors.Is(err, errFail):
		return 22

	// 6: couldn't resolve host.
	case strings.Contains(errStr, "no such host"),
		strings.Contains(errStr, "NXDOMAIN"):
		return 6

	// 28: operation timeout (--max-time expiry).  Must precede the
	// connection-failure mapping: deadline expiries surface as generic
	// network errors ("use of closed network connection", "i/o timeout")
	// depending on which socket op was interrupted.
	case strings.Contains(errStr, "context deadline exceeded"),
		strings.Contains(errStr, "deadline exceeded"),
		strings.Contains(errStr, "use of closed network connection"):
		return 28

	// 7: couldn't connect to server (refused, unreachable, timeouts during
	// the connection phase).
	case strings.Contains(errStr, "connection refused"),
		strings.Contains(errStr, "no route to host"),
		strings.Contains(errStr, "unreachable"),
		strings.Contains(errStr, "i/o timeout"),
		strings.Contains(errStr, "connection reset"):
		return 7

	// 47: too many redirects.
	case strings.Contains(errStr, "too many redirects"):
		return 47
	}

	return 1
}

// Run executes the main logic of the gcurl command with the provided config
// and output. This function is extracted to make it testable.
func Run(cfg *config.Config, out *output.Output) error {
	out.Debug("Starting gcurl %s with arguments:\n%s", appversion.Version(), cfg.RawOptions)

	out.CurlInfo("Request started; target=%s impersonate=%s", cfg.RequestURL, cfg.Impersonate)

	started := time.Now()

	var err error

	// --stderr: redirect all log/error output to a file.
	if cfg.StderrPath != "" {
		if err = out.SetLogFile(cfg.StderrPath); err != nil {
			return err
		}
	}

	// -C/--continue-at: resume appends to the existing output file and the
	// request carries a Range header (added in client.NewRequest).
	if cfg.ContinueAt != "" && cfg.OutputPath != "" {
		if err = out.SetAppendOutput(cfg.OutputPath); err != nil {
			return err
		}
	}

	transport, err := client.NewTransport(cfg, out)
	if err != nil {
		return fmt.Errorf("failed to create HTTP transport: %w", err)
	}

	req, err := client.NewRequest(cfg)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// --max-time: cap the whole operation via the request context.
	if cfg.MaxTime > 0 {
		var cancel context.CancelFunc
		ctx := req.Context()
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.MaxTime)*time.Second)
		defer cancel()
		req = req.WithContext(ctx)
	}

	// -v request/response dumps are emitted by the transport, which is the
	// first place the negotiated protocol is known (curl prints the real one).

	resp, err := doRequest(transport, req, cfg)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	defer func(body io.ReadCloser) {
		_ = body.Close()
	}(resp.Body)

	// -f/--fail: HTTP errors become exit code 22 and the body is not written.
	// curl still prints the headers when -i/-D is in play.
	if failErr := handleFail(cfg, out, resp); failErr != nil {
		return failErr
	}

	// --limit-rate: throttle the body read.
	resp.Body = limitRate(cfg, resp.Body)

	// -D/--dump-header: write the headers to a file.
	if cfg.DumpHeader != "" {
		if err = dumpHeaders(cfg.DumpHeader, resp); err != nil {
			return err
		}
	}

	responseBody := responseBodyFor(cfg, req, resp)

	out.CurlResponse(resp)

	// WebSocket is processed differently: the response is an upgrade, so the
	// payload (if any) goes out as a single frame and the body is whatever the
	// server streams back until it closes.
	if websocket.IsWebSocketResponse(resp) {
		wsConn := websocket.NewWebSocket(transport.Conn(), out, client.IsBinaryWebSocketData(cfg))
		defer func() {
			_ = wsConn.Close()
		}()

		responseBody = drainWebSocket(cfg, wsConn, responseBody)
	}

	// Write the response contents to the output.
	//
	// -i/--include: prepend the status line and headers to the output body.
	// -O/--remote-name: write the body to a file named after the URL path
	// instead of stdout.
	if cfg.Include && responseBody != nil {
		responseBody = io.MultiReader(
			strings.NewReader(output.ResponseToString(resp)),
			responseBody,
		)
	}

	if cfg.CookieJar != "" {
		_ = writeCookieJar(cfg.CookieJar, resp)
	}

	if cfg.RemoteName {
		return writeRemoteName(cfg, responseBody)
	}

	out.Write(resp, responseBody, cfg)

	// -w/--write-out: print the formatted string after the transfer, like
	// curl does.
	if cfg.WriteOut != "" {
		fmt.Print(renderWriteOut(cfg.WriteOut, resp, started, transport.ConnectTime()))
	}

	return nil
}

// responseBodyFor returns the reader the body should be written from, or nil
// when the body cannot be read to completion.
func responseBodyFor(cfg *config.Config, req *http.Request, resp *http.Response) io.Reader {
	if req.Method == http.MethodHead {
		return nil
	}

	// Response body is only written when we're sure that it is there and can
	// be fully read.
	if resp.ProtoMajor >= 2 ||
		// Content length guarantees that the response can be fully read.
		resp.ContentLength > 0 ||
		// Transfer-Encoding also guarantees that it's clear when body is
		// finished.
		len(resp.TransferEncoding) > 0 ||
		// OHTTP responses may not have any headers apart from content-type,
		// but they contain the full body right away.
		cfg.OHTTPGatewayURL != nil ||
		// When Connection: close we must read the body until the connection is
		// closed.
		resp.Header.Get("Connection") == "close" {
		return resp.Body
	}

	return nil
}

// drainWebSocket sends the configured payload as one frame and returns the
// server's reply.  It falls back to the passed-in body when nothing is sent.
func drainWebSocket(cfg *config.Config, wsConn io.ReadWriteCloser, fallback io.Reader) io.Reader {
	payload := client.BodyData(cfg)
	if payload == "" {
		return fallback
	}

	if _, err := wsConn.Write([]byte(payload)); err != nil {
		return fallback
	}

	b, err := io.ReadAll(wsConn)
	if err != nil {
		return fallback
	}

	return io.NopCloser(bytes.NewReader(b))
}

// writeRemoteName writes the body to a file named after the URL path
// (-O/--remote-name).
func writeRemoteName(cfg *config.Config, responseBody io.Reader) error {
	fname := remoteFileName(cfg.RequestURL)
	f, err := os.Create(fname) //nolint:gosec // G703: remoteFileName strips directories
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %w", fname, err)
	}
	defer func() {
		_ = f.Close()
	}()

	if responseBody != nil {
		if _, err = io.Copy(f, responseBody); err != nil {
			return fmt.Errorf("failed to write to %s: %w", fname, err)
		}
	}

	return nil
}

// renderWriteOut expands curl's --write-out variables.  Supported:
// %{http_code}, %{url_effective}, %{time_total}, %{time_connect},
// %{time_appconnect}, %{size_download}, %{response_code}, %{scheme},
// %{remote_ip}, %{remote_port}, %{content_type}, %{http_version};
// \n, \r, \t literals.
func renderWriteOut(format string, resp *http.Response, started time.Time, connectTime time.Duration) (s string) {
	total := time.Since(started).Seconds()
	size := int64(0)
	if resp.ContentLength > 0 {
		size = resp.ContentLength
	}

	remoteIP, remotePort := "0.0.0.0", "0"
	if resp.Request != nil && resp.Request.URL != nil {
		if host := resp.Request.URL.Hostname(); net.ParseIP(host) != nil {
			remoteIP = host
			remotePort = resp.Request.URL.Port()
		}
	}

	replacer := strings.NewReplacer(
		"%{http_code}", fmt.Sprintf("%03d", resp.StatusCode),
		"%{response_code}", fmt.Sprintf("%03d", resp.StatusCode),
		"%{url_effective}", resp.Request.URL.String(),
		"%{time_total}", fmt.Sprintf("%.6f", total),
		"%{time_connect}", fmt.Sprintf("%.6f", connectTime.Seconds()),
		"%{time_appconnect}", fmt.Sprintf("%.6f", connectTime.Seconds()),
		"%{size_download}", fmt.Sprintf("%d", size),
		"%{scheme}", resp.Request.URL.Scheme,
		"%{http_version}", fmt.Sprintf("%d", resp.ProtoMajor),
		"%{content_type}", resp.Header.Get("Content-Type"),
		"%{remote_ip}", remoteIP,
		"%{remote_port}", remotePort,
		"\\n", "\n",
		"\\r", "\r",
		"\\t", "\t",
	)

	return replacer.Replace(format)
}

// writeCookieJar writes received cookies (from the Set-Cookie headers) to
// the cookie jar file in Netscape format (-c/--cookie-jar).
func writeCookieJar(fname string, resp *http.Response) (err error) {
	host := resp.Request.URL.Hostname()

	var b strings.Builder
	b.WriteString("# Netscape HTTP Cookie File\n")
	for _, sc := range resp.Header.Values("Set-Cookie") {
		parts := strings.Split(sc, ";")
		kv := strings.SplitN(parts[0], "=", 2)
		if len(kv) != 2 {
			continue
		}
		secure := strings.Contains(strings.ToLower(sc), "secure")
		//nolint:gosec // G705: written to a Netscape cookie file, never HTML
		fmt.Fprintf(&b, "%s\tFALSE\t/\t%s\t0\t%s\t%s\n",
			host,
			secureFlag(secure),
			kv[0],
			kv[1],
		)
	}

	//nolint:gosec // G703: fname is the operator's -c path, not remote input
	return os.WriteFile(fname, []byte(b.String()), 0o600)
}

// secureFlag converts a boolean to the Netscape cookie file secure column.
func secureFlag(secure bool) string {
	if secure {
		return "TRUE"
	}

	return "FALSE"
}

// remoteFileName derives the -O/--remote-name output filename from the URL
// path: last path segment, defaulting to the hostname.
// remoteFileName returns the file name to write to for -O/--remote-name: the
// last path segment, sanitized so a URL cannot escape the working directory.
func remoteFileName(u *url.URL) (fname string) {
	trimmed := strings.Trim(u.Path, "/")
	if trimmed != "" {
		segs := strings.Split(trimmed, "/")
		// filepath.Base strips any directory component ("..", "/", "\"), so
		// the result always names a file in the current directory.
		if base := filepath.Base(segs[len(segs)-1]); base != "." && base != ".." && base != string(filepath.Separator) {
			fname = base
		}
	}
	if fname == "" {
		fname = u.Hostname()
	}

	return fname
}
