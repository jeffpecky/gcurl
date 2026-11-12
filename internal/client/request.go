package client

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jeffpecky/gcurl/internal/appversion"
	"github.com/jeffpecky/gcurl/internal/client/websocket"
	"github.com/jeffpecky/gcurl/internal/config"
	"github.com/jeffpecky/gcurl/internal/profile"
)

// NewRequest creates a new *http.Request based on *cmd.Options.
func NewRequest(cfg *config.Config) (req *http.Request, err error) {
	var bodyStream io.Reader
	var contentType string

	// Do not add body for WebSocket requests as in this case --data is handled
	// differently, and it is sent after the handshake.
	if !websocket.IsWebSocket(cfg.RequestURL) {
		bodyStream, contentType, err = createBody(cfg)
		if err != nil {
			return nil, err
		}
	}

	// -G/--get: move the data into the URL query string and use GET.
	method := getMethod(cfg)
	if cfg.Get && !websocket.IsWebSocket(cfg.RequestURL) {
		method = http.MethodGet

		u := *cfg.RequestURL
		if cfg.Data != "" {
			q := u.Query()
			for _, kv := range strings.Split(cfg.Data, "&") {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 2 {
					q.Add(parts[0], parts[1])
				}
			}
			u.RawQuery = q.Encode()
		}
		req, err = http.NewRequest(method, u.String(), bodyStream)
	} else {
		req, err = http.NewRequest(method, cfg.RequestURL.String(), bodyStream)
	}
	if err != nil {
		return nil, err
	}

	addHeaders(req, cfg)
	addBodyHeaders(req, cfg)

	// -F sets its own Content-Type (multipart boundary).
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// -u/--user: basic authentication.
	if cfg.User != "" {
		user, pass, _ := strings.Cut(cfg.User, ":")
		req.SetBasicAuth(user, pass)
	}

	// Default User-Agent and headers: impersonating uses the active
	// profile's browser values; otherwise the tool identifies itself.
	// Explicit user flags always win.
	applyProfileDefaults(req, cfg)

	if ur := websocket.UpgradeWebSocket(req); ur != nil {
		req = ur
	}

	return req, err
}

// applyProfileDefaults fills in the User-Agent and default headers from the
// active impersonation profile for anything the user did not set explicitly.
// When impersonation is off, gcurl's own User-Agent is set (it must not
// pretend to be something it isn't).
func applyProfileDefaults(req *http.Request, cfg *config.Config) {
	prof, err := profile.Get(cfg.Impersonate)
	if err != nil || prof == nil || prof.HTTP == nil {
		if req.Header.Get("User-Agent") == "" {
			req.Header.Set("User-Agent", fmt.Sprintf("gcurl/%s", appversion.Version()))
		}
		return
	}

	if req.Header.Get("User-Agent") == "" && prof.HTTP.UserAgent != "" {
		req.Header.Set("User-Agent", prof.HTTP.UserAgent)
	}

	for _, raw := range prof.HTTP.DefaultHeaders {
		name, value, _ := strings.Cut(raw, ":")
		if name == "" || req.Header.Get(name) != "" {
			continue
		}
		req.Header.Set(name, strings.TrimSpace(value))
	}
}

// createBody creates body stream if it's required by the command-line
// arguments.  Priority: -T (upload), --json, -F (multipart), --data-raw,
// --data-binary, --data-urlencode, --data.  Returns the body reader plus an
// optional Content-Type that must be set on the request.
func createBody(cfg *config.Config) (body io.Reader, contentType string, err error) {
	if cfg.UploadFile != "" {
		// -T/--upload-file: the file becomes the request body verbatim.
		if cfg.UploadFile == "-" {
			return os.Stdin, "", nil
		}

		b, rErr := os.ReadFile(cfg.UploadFile)
		if rErr != nil {
			return nil, "", fmt.Errorf("cannot read upload file %s: %w", cfg.UploadFile, rErr)
		}

		return bytes.NewReader(b), "", nil
	}

	if len(cfg.JSON) > 0 {
		return bytes.NewBufferString(joinData(cfg.JSON)), "application/json", nil
	}

	if fields := len(cfg.Form) + len(cfg.FormStrings); fields > 0 {
		buf := &bytes.Buffer{}
		w := multipart.NewWriter(buf)
		// --form-string values are literal; -F values interpret @file/<file.
		for _, field := range cfg.Form {
			name, value, _ := strings.Cut(field, "=")
			if name == "" {
				return nil, "", fmt.Errorf("invalid -F field %q, expected name=value", field)
			}
			if uErr := writeFormField(w, name, value); uErr != nil {
				return nil, "", uErr
			}
		}
		for _, field := range cfg.FormStrings {
			name, value, _ := strings.Cut(field, "=")
			if name == "" {
				return nil, "", fmt.Errorf("invalid --form-string %q, expected name=value", field)
			}
			if err := w.WriteField(name, value); err != nil {
				return nil, "", err
			}
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}

		return buf, w.FormDataContentType(), nil
	}

	data := BodyData(cfg)
	if data == "" {
		return nil, "", nil
	}

	return bytes.NewBufferString(data), "", nil
}

// joinData concatenates repeated data pieces the way curl does for --json.
func joinData(pieces []string) string {
	return strings.Join(pieces, "")
}

// writeFormField writes one multipart part.  "name=@file" uploads the file and
// "name=<file" embeds its contents; --form-string values never reach here
// because they are passed through as-is by createBody.
func writeFormField(w *multipart.Writer, name, value string) error {
	if strings.HasPrefix(value, "@") || strings.HasPrefix(value, "<") {
		path := value[1:]
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot read form file %s: %w", path, err)
		}

		if strings.HasPrefix(value, "@") {
			// @file: send with the file's basename as the filename.
			part, cErr := w.CreateFormFile(name, filepath.Base(path))
			if cErr != nil {
				return cErr
			}
			_, err = part.Write(b)

			return err
		}

		// <file: send as a plain field value.
		return w.WriteField(name, string(b))
	}

	return w.WriteField(name, value)
}

// BodyData returns the effective request body data (--data/--data-raw/
// --data-binary/--data-urlencode).
func BodyData(cfg *config.Config) (data string) {
	if cfg.DataRaw != "" {
		return cfg.DataRaw
	}
	if cfg.DataBinary != "" {
		return cfg.DataBinary
	}
	if len(cfg.DataURLEncode) > 0 {
		return urlEncodeData(cfg.DataURLEncode)
	}

	return cfg.Data
}

// urlEncodeData implements --data-urlencode: "name=content" encodes only the
// content, bare "content" encodes the whole string, and "=content" sends an
// encoded value under an empty name.
func urlEncodeData(pieces []string) string {
	encoded := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		if name, value, ok := strings.Cut(piece, "="); ok {
			encoded = append(encoded, name+"="+url.QueryEscape(value))
		} else {
			encoded = append(encoded, url.QueryEscape(piece))
		}
	}

	return strings.Join(encoded, "&")
}

// resumeOffset implements -C -: the offset is the current size of the output
// file, so the transfer continues where it stopped.  It returns false when
// there is no output file to measure.
func resumeOffset(cfg *config.Config) (offset int64, ok bool) {
	fname := cfg.OutputPath
	if fname == "" && cfg.RemoteName {
		fname = filepath.Base(cfg.RequestURL.Path)
	}
	if fname == "" {
		return 0, false
	}

	fi, err := os.Stat(fname) //nolint:gosec // G703: fname is the operator's own -o/-O path
	if err != nil || fi.Size() == 0 {
		return 0, false
	}

	return fi.Size(), true
}

// addRangeHeader implements -r/--range and -C/--continue-at.
func addRangeHeader(req *http.Request, cfg *config.Config) {
	if req.Header.Get("Range") != "" {
		return
	}

	if cfg.Range != "" {
		req.Header.Set("Range", "bytes="+cfg.Range)

		return
	}

	switch {
	case cfg.ContinueAt == "-":
		if offset, ok := resumeOffset(cfg); ok {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
	case cfg.ContinueAt != "":
		req.Header.Set("Range", "bytes="+cfg.ContinueAt+"-")
	}
}

// addBodyHeaders adds necessary HTTP headers if they're required by the
// command-line arguments. For instance, -d/--data requires adding the
// Content-Type: application/x-www-form-urlencoded header.
func addBodyHeaders(req *http.Request, cfg *config.Config) {
	data := BodyData(cfg)
	if data != "" && !websocket.IsWebSocket(cfg.RequestURL) {
		if req.Header.Get("Content-Type") == "" && cfg.DataBinary == "" && len(cfg.JSON) == 0 {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}

	// --json: curl sets both Content-Type and Accept.
	if len(cfg.JSON) > 0 {
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/json")
		}
	}

	// -r/--range and -C/--continue-at both set Range; first one wins.
	addRangeHeader(req, cfg)

	// --compressed: ask for a compressed response.
	if cfg.Compressed && req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	}

	// -A/--user-agent and -e/--referer are shortcuts for -H.
	if cfg.UserAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", cfg.UserAgent)
	}
	if cfg.Referer != "" && req.Header.Get("Referer") == "" {
		req.Header.Set("Referer", cfg.Referer)
	}

	// -b/--cookie: name=value pairs are sent directly; a file is parsed.
	if cfg.Cookie != "" {
		if cookieHeader := parseCookieArg(cfg.Cookie); cookieHeader != "" {
			req.Header.Add("Cookie", cookieHeader)
		}
	}
}

// IsBinaryWebSocketData reports whether the WebSocket payload should be sent
// with the binary opcode rather than text.  --data-binary is binary by
// definition; --data and --data-raw are text.
func IsBinaryWebSocketData(cfg *config.Config) bool {
	return cfg.DataBinary != ""
}

// parseCookieArg handles -b/--cookie: if the argument looks like a file
// (starts with @ or contains no "=" and exists on disk), read "name=value"
// lines from it; otherwise treat it as literal cookie pairs.
func parseCookieArg(arg string) (cookieHeader string) {
	var pairs []string

	isFile := strings.HasPrefix(arg, "@")
	if !isFile && !strings.Contains(arg, "=") {
		isFile = true
	}

	if isFile {
		fname := strings.TrimPrefix(arg, "@")
		b, err := os.ReadFile(fname)
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Netscape cookie file format: domain \t flag \t path \t secure
			// \t expiry \t name \t value.
			fields := strings.Split(line, "\t")
			if len(fields) == 7 {
				pairs = append(pairs, fields[5]+"="+fields[6])
			} else {
				pairs = append(pairs, line)
			}
		}
	} else {
		pairs = append(pairs, arg)
	}

	return strings.Join(pairs, "; ")
}

// addHeaders adds HTTP headers that are specified in command-line arguments.
func addHeaders(req *http.Request, cfg *config.Config) {
	for k, l := range cfg.Headers {
		for _, v := range l {
			req.Header.Add(k, v)
		}
	}
}
