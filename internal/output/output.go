// Package output is responsible for writing logs and received response data.
package output

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jeffpecky/gcurl/internal/config"
)

// Writer is an interface for writing output.
type Writer interface {
	io.Writer
	io.StringWriter
}

// Output is responsible for all the output, be it logging or writing received
// data.
type Output struct {
	dataFileWriter Writer
	logFileWriter  Writer
	verbose        bool
	jsonOutput     bool
}

// NewOutput creates a new instance of Output. path is an optional path to the
// file where the tool will write the received data. If not specified, this
// information will be written to stdout. verbose defines whether we need to
// write extended information. jsonOutput defines whether errors should be
// formatted as JSON.
func NewOutput(path string, verbose bool, jsonOutput bool) (o *Output, err error) {
	var dataWriter, logWriter Writer
	dataWriter = os.Stdout
	logWriter = os.Stderr

	if path != "" {
		dataWriter, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
	}

	o = NewOutputWithWriters(dataWriter, logWriter, verbose, jsonOutput)

	return o, err
}

// NewOutputWithWriters creates a new instance of Output, using the provided
// dataWriter and logWriter for output and logging respectively. verbose
// defines whether we need to write extended information. jsonOutput defines
// whether errors should be formatted as JSON.
func NewOutputWithWriters(dataWriter Writer, logWriter Writer, verbose bool, jsonOutput bool) (o *Output) {
	return &Output{
		dataFileWriter: dataWriter,
		logFileWriter:  logWriter,
		verbose:        verbose,
		jsonOutput:     jsonOutput,
	}
}

// SetLogFile redirects all log and error output to path (--stderr).
func (o *Output) SetLogFile(path string) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("cannot open log file %s: %w", path, err)
	}

	o.logFileWriter = f

	return nil
}

// SetAppendOutput makes the output file append instead of overwriting, which
// is what -C/--continue-at needs to resume a download.
func (o *Output) SetAppendOutput(path string) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("cannot open output file %s: %w", path, err)
	}

	o.dataFileWriter = f

	return nil
}

// Write writes received data to the output path (or stdout if not specified).
func (o *Output) Write(resp *http.Response, responseBody io.Reader, cfg *config.Config) {
	var err error

	if cfg.OutputJSON {
		var b []byte
		b, err = responseToJSON(resp, responseBody, o.logFileWriter)
		if err != nil {
			panic(err)
		}

		_, err = o.dataFileWriter.Write(b)
	} else if responseBody == nil {
		_, err = o.dataFileWriter.WriteString(ResponseToString(resp))
	} else {
		_, err = io.Copy(o.dataFileWriter, responseBody)
	}

	if err != nil {
		msg := fmt.Sprintf("Failed to write response: %v", err)
		_, _ = o.logFileWriter.WriteString(msg + "\n")
	}
}

// Info writes INFO-level log to the log file (or stderr).
func (o *Output) Info(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	_, err := o.logFileWriter.WriteString(msg + "\n")
	if err != nil {
		panic(err)
	}
}

// Error writes an error message. If jsonOutput is enabled, it formats the
// error as JSON and writes it to the dataFileWriter (stdout by default).
// Otherwise, it writes a plain text error to stderr.
func (o *Output) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	if o.jsonOutput {
		// Write error as JSON to the output file (stdout by default)
		errorJSON := map[string]string{"error": msg}
		b, err := json.MarshalIndent(errorJSON, "", "  ")
		if err != nil {
			panic(err)
		}
		_, err = o.dataFileWriter.Write(b)
		if err != nil {
			panic(err)
		}
		_, err = o.dataFileWriter.WriteString("\n")
		if err != nil {
			panic(err)
		}
	} else {
		// Write plain text error to stderr
		_, err := o.logFileWriter.WriteString(msg + "\n")
		if err != nil {
			panic(err)
		}
	}
}

// Debug writes DEBUG-level log to stderr (controlled by the verbose flag).
func (o *Output) Debug(format string, args ...any) {
	if !o.verbose {
		return
	}

	if _, err := fmt.Fprintf(o.logFileWriter, format+"\n", args...); err != nil {
		panic(err)
	}
}

// ResponseToString converts HTTP response to a string.
func ResponseToString(resp *http.Response) (str string) {
	return fmt.Sprintf(
		"%s %s\r\n%s",
		resp.Proto,
		resp.Status,
		headersToString(resp.Header, resp.ProtoMajor),
	)
}

// HeaderLines renders a response's headers the way curl does: h2 names
// lowercase, sorted for determinism, CRLF terminated (used by -D too).
func HeaderLines(resp *http.Response) string {
	return headersToString(resp.Header, resp.ProtoMajor)
}

// headersToString converts HTTP headers to a string.  curl prints h2 header
// names lowercase (that is how they travel on the wire) and both protocols in
// a stable order, so the same response always renders the same bytes.
func headersToString(headers http.Header, protoMajor int) (str string) {
	names := make([]string, 0, len(headers))
	for key := range headers {
		names = append(names, key)
	}
	sort.Strings(names)

	for _, key := range names {
		name := key
		if protoMajor == 2 {
			name = strings.ToLower(name)
		}
		for _, value := range headers[key] {
			str = str + fmt.Sprintf("%s: %s\r\n", name, value)
		}
	}

	return str
}

// TLSCertificate is a helper object for serializing information about x509
// certificates.
type TLSCertificate struct {
	Subject     string    `json:"subject"`
	Issuer      string    `json:"issuer"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	DNSNames    []string  `json:"dns_names"`
	IPAddresses []string  `json:"ip_addresses"`
	Raw         string    `json:"raw"`
}

// TLSState is a helper object for serializing response data to JSON.
type TLSState struct {
	ServerName         string           `json:"server_name"`
	Version            string           `json:"version"`
	CipherSuite        string           `json:"cipher_suite"`
	NegotiatedProtocol string           `json:"negotiated_protocol"`
	Certificates       []TLSCertificate `json:"certificates"`
}

// ResponseData is a helper object for serializing response data to JSON.
type ResponseData struct {
	StatusCode int                 `json:"status_code"`
	Status     string              `json:"status"`
	Proto      string              `json:"proto"`
	TLS        *TLSState           `json:"tls"`
	Headers    map[string][]string `json:"headers"`
	BodyBase64 string              `json:"body_base64"`
}

// stateToTLSState converts tls.ConnectionState to TLSState.
func stateToTLSState(state *tls.ConnectionState) (s *TLSState) {
	s = &TLSState{
		ServerName:         state.ServerName,
		Version:            tls.VersionName(state.Version),
		CipherSuite:        tls.CipherSuiteName(state.CipherSuite),
		NegotiatedProtocol: state.NegotiatedProtocol,
	}

	for _, cert := range state.PeerCertificates {
		var certInfo TLSCertificate
		certInfo.Subject = cert.Subject.String()
		certInfo.Issuer = cert.Issuer.String()
		certInfo.NotBefore = cert.NotBefore
		certInfo.NotAfter = cert.NotAfter
		certInfo.DNSNames = cert.DNSNames
		for _, ip := range cert.IPAddresses {
			certInfo.IPAddresses = append(certInfo.IPAddresses, ip.String())
		}
		certInfo.Raw = certToPEM(cert.Raw)
		s.Certificates = append(s.Certificates, certInfo)
	}

	return s
}

// responseToJSON transforms response data to JSON format.  A failed body read
// is reported through the log writer and the partial body is still emitted.
func responseToJSON(resp *http.Response, responseBody io.Reader, log io.Writer) (b []byte, err error) {
	var body []byte

	if responseBody != nil {
		var readErr error
		body, readErr = io.ReadAll(responseBody)
		if readErr != nil {
			_, _ = fmt.Fprintf(log, "Failed to read response body: %v\n", readErr)
		}
	}

	if body == nil {
		body = []byte{}
	}

	data := ResponseData{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Proto:      resp.Proto,
		Headers:    resp.Header,
		BodyBase64: base64.StdEncoding.EncodeToString(body),
	}

	if resp.TLS != nil {
		data.TLS = stateToTLSState(resp.TLS)
	}

	b, err = json.MarshalIndent(data, "", "  ")

	return b, err
}

// certToPEM serializes certificate bytes to PEM format.
func certToPEM(certBytes []byte) (str string) {
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}

	pemBytes := pem.EncodeToMemory(block)

	return string(pemBytes)
}
