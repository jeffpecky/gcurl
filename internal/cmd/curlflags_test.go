package cmd_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeffpecky/gcurl/internal/cmd"
	"github.com/jeffpecky/gcurl/internal/config"
	"github.com/jeffpecky/gcurl/internal/output"
	"github.com/mccutchen/go-httpbin/v2/httpbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runWithArgs parses args, runs the command and returns the data and log output.
func runWithArgs(t *testing.T, args ...string) (data, logOutput string, err error) {
	t.Helper()

	cfg, cErr := config.ParseConfig(args)
	require.NoError(t, cErr)

	dataBuffer := &bytes.Buffer{}
	logBuffer := &bytes.Buffer{}
	out := output.NewOutputWithWriters(dataBuffer, logBuffer, cfg.Verbose, cfg.OutputJSON)

	err = cmd.Run(cfg, out)

	return dataBuffer.String(), logBuffer.String(), err
}

// TestRunFail tests that -f turns an HTTP error into an error and writes no body.
func TestRunFail(t *testing.T) {
	handler := httpbin.New()
	server := httptest.NewServer(handler.Handler())
	defer server.Close()

	data, _, err := runWithArgs(t, "-s", "-f", server.URL+"/status/404")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the requested URL returned error")
	assert.Empty(t, data, "-f must not write the error page body")

	// Without -f the same request succeeds.
	data, _, err = runWithArgs(t, "-s", server.URL+"/status/404")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

// TestRunFailIncludeKeepsHeaders tests that -f -i still emits the headers,
// which is what curl does.
func TestRunFailIncludeKeepsHeaders(t *testing.T) {
	handler := httpbin.New()
	server := httptest.NewServer(handler.Handler())
	defer server.Close()

	data, _, err := runWithArgs(t, "-s", "-f", "-i", server.URL+"/status/404")
	require.Error(t, err)
	assert.Contains(t, data, "404 Not Found")
	assert.Contains(t, data, "Content-Length")
}

// TestRunDumpHeader tests that -D writes the status line and headers to a file.
func TestRunDumpHeader(t *testing.T) {
	handler := httpbin.New()
	server := httptest.NewServer(handler.Handler())
	defer server.Close()

	path := filepath.Join(t.TempDir(), "headers.txt")
	_, _, err := runWithArgs(t, "-s", "-D", path, "-o", filepath.Join(t.TempDir(), "body"), server.URL+"/status/201")
	require.NoError(t, err)

	b, rErr := os.ReadFile(path) //nolint:gosec // G304: test-owned temp path
	require.NoError(t, rErr)
	assert.Contains(t, string(b), "201 Created")
	assert.Contains(t, string(b), "Content-Length:")
}

// TestRunJSON tests that --json sets both Content-Type and Accept.
func TestRunJSON(t *testing.T) {
	handler := httpbin.New()
	server := httptest.NewServer(handler.Handler())
	defer server.Close()

	data, _, err := runWithArgs(t, "-s", "--json", `{"a":1}`, server.URL+"/post")
	require.NoError(t, err)

	var got struct {
		JSON  any               `json:"json"`
		Data  string            `json:"data"`
		Form  map[string]string `json:"form"`
		Files map[string]string `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(data), &got))
	assert.Equal(t, `{"a":1}`, got.Data)
}

// TestRunJSONHeaders checks the request headers --json must set.
func TestRunJSONHeaders(t *testing.T) {
	var contentType, accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType, accept = r.Header.Get("Content-Type"), r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, _, err := runWithArgs(t, "-s", "--json", `{"a":1}`, server.URL)
	require.NoError(t, err)
	assert.Equal(t, "application/json", contentType)
	assert.Equal(t, "application/json", accept)
}

// TestRunRange tests that -r/--range sends a Range header.
func TestRunRange(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	_, _, err := runWithArgs(t, "-s", "--range", "0-99", server.URL)
	require.NoError(t, err)
	assert.Equal(t, "bytes=0-99", got)
}

// TestRunUploadFile tests that -T/--upload-file sends a PUT with the file body.
func TestRunUploadFile(t *testing.T) {
	var method, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := &bytes.Buffer{}
		_, _ = buf.ReadFrom(r.Body)
		method, body = r.Method, buf.String()
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "upload.txt")
	require.NoError(t, os.WriteFile(path, []byte("upload-content"), 0o600))

	_, _, err := runWithArgs(t, "-s", "-T", path, server.URL+"/put")
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, method)
	assert.Equal(t, "upload-content", body)
}

// TestRunFormString tests that --form-string is sent verbatim, without @file
// interpretation.
func TestRunFormString(t *testing.T) {
	handler := httpbin.New()
	server := httptest.NewServer(handler.Handler())
	defer server.Close()

	data, _, err := runWithArgs(t, "-s", "--form-string", "field=@notafile", server.URL+"/post")
	require.NoError(t, err)
	assert.Contains(t, data, `"field": [`)
	assert.Contains(t, data, `"@notafile"`)
	assert.Contains(t, data, `"files": {}`)
}

// TestRunDataURLEncode tests that --data-urlencode percent-encodes the value.
func TestRunDataURLEncode(t *testing.T) {
	handler := httpbin.New()
	server := httptest.NewServer(handler.Handler())
	defer server.Close()

	data, _, err := runWithArgs(t, "-s", "--data-urlencode", "q=a b&c", server.URL+"/post")
	require.NoError(t, err)

	var got struct {
		Form map[string][]string `json:"form"`
	}
	require.NoError(t, json.Unmarshal([]byte(data), &got))
	// Same as curl: the value is encoded on the wire and decoded here.
	assert.Equal(t, []string{"a b&c"}, got.Form["q"])
}

// TestRunMaxRedirs tests that --max-redirs bounds the redirect chain.
func TestRunMaxRedirs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer server.Close()

	_, _, err := runWithArgs(t, "-s", "-L", "--max-redirs", "2", server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many redirects")
}
