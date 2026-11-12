// Package config is responsible for parsing and validating cmd arguments.
package config

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AdguardTeam/dnsproxy/upstream"
	ctls "github.com/ameshkov/cfcrypto/tls"
	"github.com/jeffpecky/gcurl/internal/profile"
)

// Config is a strictly-typed and validated configuration structure which is
// created from Options (command-line arguments).
type Config struct {
	// RequestURL is the URL where the target request will be sent.
	RequestURL *url.URL

	// Method is the HTTP method of the request.
	Method string

	// Head signals that the tool should only fetch headers. If specified,
	// headers will be written to the output.
	Head bool

	// Data specifies the data to be sent to the HTTP server.
	Data string

	// DataRaw is like Data but without @-file syntax.
	DataRaw string

	// DataBinary is like Data but sent as-is.
	DataBinary string

	// DataURLEncode is data that must be URL-encoded before sending.
	DataURLEncode []string

	// JSON is the --json payload pieces (concatenated, sent as JSON).
	JSON []string

	// UploadFile is the -T/--upload-file path, or "-" for stdin.
	UploadFile string

	// ContinueAt is the -C/--continue-at offset ("-" means auto).
	ContinueAt string

	// Range is the -r/--range byte range.
	Range string

	// FormStrings are --form-string fields (never @file interpreted).
	FormStrings []string

	// Fail enables -f/--fail (exit code 22 on HTTP >= 400).
	Fail bool

	// Retry is the number of retries on transient errors.
	Retry int

	// MaxRedirs is the redirect limit for -L. -1 means unlimited.  Default 50,
	// matching curl.
	MaxRedirs int

	// LimitRate is the transfer rate cap in bytes per second, 0 = unlimited.
	LimitRate int64

	// CACert is the CA bundle path (--cacert).
	CACert string

	// Cert is the client certificate path (--cert).
	Cert string

	// Key is the client private key path (--key).
	Key string

	// DumpHeader is the -D/--dump-header output file.
	DumpHeader string

	// StderrPath redirects logs/errors to a file (--stderr).
	StderrPath string

	// Netrc enables reading ~/.netrc credentials (curl -n).
	Netrc bool

	// ProxyUser overrides proxy credentials (--proxy-user).
	ProxyUser string

	// Form is the list of multipart form fields (curl -F).
	Form []string

	// Get makes the request use GET with the data moved to the URL query.
	Get bool

	// RemoteName writes the output to a file named after the remote URL path.
	RemoteName bool

	// Location makes the tool follow redirects.
	Location bool

	// User is the user:password pair for basic authentication.
	User string

	// Cookie is either a "name=value" pair or a file with cookies.
	Cookie string

	// CookieJar is a file to write cookies to after the request.
	CookieJar string

	// Compressed requests a compressed response.
	Compressed bool

	// UserAgent is a shortcut for the User-Agent header.
	UserAgent string

	// Referer is a shortcut for the Referer header.
	Referer string

	// Silent suppresses error messages (curl -s).
	Silent bool

	// Include includes the response headers in the output (curl -i).
	Include bool

	// WriteOut is the curl --write-out format string.
	WriteOut string

	// MaxTime is the maximum total time in seconds for the whole operation.
	MaxTime int

	// Impersonate is the name of the profile from the impersonation registry,
	// or "none"/"" to disable impersonation.  When the flag is omitted, this
	// is resolved to the default profile name (see profile.DefaultProfileName).
	Impersonate string

	// Headers is the HTTP headers that will be added to the request.
	Headers http.Header

	// RawHeaders is the header strings exactly as given on the command line
	// (order preserved) — used by the H2 fingerprint path where header order
	// matters.
	RawHeaders []string

	// ProxyURL is a URL of a proxy to use with this connection.
	ProxyURL *url.URL

	// ConnectTo is a mapping of "host1:port1" to "host2:port2" pairs that
	// allows retargeting the connection.
	ConnectTo map[string]string

	// Insecure disables TLS verification of the connection.
	Insecure bool

	// TLSMinVersion is a minimum supported TLS version.
	TLSMinVersion uint16

	// TLSMaxVersion is a maximum supported TLS version.
	TLSMaxVersion uint16

	// TLSCiphers is a list of ciphers that the client will send in the TLS
	// ClientHello.
	TLSCiphers []uint16

	// TLSServerName allows to send a specified server name in the TLS
	// ClientHello extension.
	TLSServerName string

	// ForceHTTP11 forces using HTTP/1.1.
	ForceHTTP11 bool

	// ForceHTTP2 forces using HTTP/2.
	ForceHTTP2 bool

	// ForceHTTP3 forces using HTTP/3.
	ForceHTTP3 bool

	// ECH forces usage of Encrypted Client Hello for the request.  If other
	// ECH-related fields are not specified, the ECH configuration will be
	// received from the DNS settings.
	ECH bool

	// ECHGrease forces sending ECH grease in the ClientHello.  This option
	// does not try to resolve the ECH configuration and is only used for
	// testing ECH grease.
	ECHGrease bool

	// ECHConfigs is a set of ECH configurations that will be used when opening
	// an encrypted connection.
	ECHConfigs []ctls.ECHConfig

	// Resolve is a map of host:ips pairs.  It allows specifying custom IP
	// addresses for a specific host or all hosts (if '*' is used instead of
	// the host name).
	Resolve map[string][]net.IP

	// IPv4 if configured forces usage of IP4 addresses only when doing DNS
	// resolution.
	IPv4 bool

	// IPv6 if configured forces usage of IP4 addresses only when doing DNS
	// resolution.
	IPv6 bool

	// DNSServers is a list of upstream DNS servers that will be used for
	// resolving hostnames.
	DNSServers []upstream.Upstream

	// TLSSplitChunkSize is a size of the first chunk of ClientHello that is
	// sent to the server.
	TLSSplitChunkSize int

	// TLSSplitDelay is a delay in milliseconds before sending the second
	// chunk of ClientHello.
	TLSSplitDelay int

	// TLSRandom is a 32-byte value to override the TLS ClientHello random.
	// If nil, use default.
	TLSRandom []byte

	// OutputJSON enables writing output in JSON format.
	OutputJSON bool

	// OutputPath defines where to write the received data. If not set, the
	// received data will be written to stdout.
	OutputPath string

	// ConnectTimeout limits the maximum time in seconds allowed for the
	// connection phase. Applied to SOCKS5 proxy connections as well.
	ConnectTimeout int

	// Experiments is a map where the key is Experiment and value is its
	// optional configuration.
	Experiments map[Experiment]string

	// OHTTPGatewayURL is the URL of the Oblivious HTTP gateway where the
	// request should be sent.
	OHTTPGatewayURL *url.URL

	// OHTTPKeysURL is the URL from which to retrieve Oblivious HTTP KeyConfig
	// to use for encrypting the request.
	OHTTPKeysURL *url.URL

	// Verbose defines whether we should write the DEBUG-level log or not.
	Verbose bool

	// RawOptions is the raw command-line arguments struct (for logging only).
	RawOptions *Options
}

// Experiment is an enumeration of experimental features available for us via
// the --experiment flag.
type Experiment string

const (
	// ExpNone is just an empty value, not an experiment.
	ExpNone Experiment = ""

	// ExpPostQuantum stands for post-quantum cryptography.  See the website for
	// more details: https://pq.cloudflareresearch.com/.
	ExpPostQuantum Experiment = "pq"
)

// NewExperiment tries to create an Experiment from string.  Returns error if
// the string is not a valid member of the enumeration.
func NewExperiment(str string) (e Experiment, err error) {
	switch str {
	case string(ExpPostQuantum):
		return ExpPostQuantum, nil
	}

	return ExpNone, fmt.Errorf("invalid experiment name: %s", str)
}

// ParseConfig parses and validates the provided args and returns the final *Config
// object.
//
// Disable gocyclo for ParseConfig as it's supposed to be a large function with
// if conditions.
//
// nolint:gocyclo
func ParseConfig(args []string) (cfg *Config, err error) {
	opts, err := parseOptions(args)
	if err != nil {
		return nil, err
	}

	cfg = &Config{
		Method:     opts.Method,
		Head:       opts.Head,
		Insecure:   opts.Insecure,
		Data:       opts.Data,
		DataRaw:    opts.DataRaw,
		DataBinary: opts.DataBinary,
		Form:       opts.Form,
		Get:        opts.Get,
		RemoteName: opts.RemoteName,
		Location:   opts.Location,
		User:       opts.User,
		Cookie:     opts.Cookie,
		CookieJar:  opts.CookieJar,
		Compressed: opts.Compressed,
		UserAgent:  opts.UserAgent,
		Referer:    opts.Referer,
		Silent:     opts.Silent,
		Include:    opts.Include,
		WriteOut:   opts.WriteOut,
		MaxTime:    opts.MaxTime,
		OutputJSON: opts.OutputJSON, OutputPath: opts.OutputPath,
		ConnectTimeout: opts.ConnectTimeout,
		Verbose:        opts.Verbose,
		ForceHTTP11:    opts.HTTPv11,
		ForceHTTP2:     opts.HTTPv2,
		ForceHTTP3:     opts.HTTPv3,
		ECH:            opts.ECH,
		ECHGrease:      opts.ECHGrease,
		IPv4:           opts.IPv4,
		IPv6:           opts.IPv6,
		DataURLEncode:  opts.DataURLEncode,
		JSON:           opts.JSON,
		UploadFile:     opts.UploadFile,
		ContinueAt:     opts.ContinueAt,
		Range:          opts.Range,
		FormStrings:    opts.FormStrings,
		Fail:           opts.Fail,
		Retry:          opts.Retry,
		MaxRedirs:      opts.MaxRedirs,
		CACert:         opts.CACert,
		Cert:           opts.Cert,
		Key:            opts.Key,
		DumpHeader:     opts.DumpHeader,
		StderrPath:     opts.StderrPath,
		Netrc:          opts.Netrc,
		ProxyUser:      opts.ProxyUser,
		TLSServerName:  opts.TLSServerName,
		RawOptions:     opts,
	}

	cfg.RequestURL, err = url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL specified %s: %w", opts.URL, err)
	}

	if cfg.RequestURL.Scheme == "" {
		// Use http scheme by default.
		cfg.RequestURL.Scheme = "http"
	}

	// --max-redirs defaults to curl's 50; -1 means unlimited.
	if opts.MaxRedirs == 0 {
		cfg.MaxRedirs = DefaultMaxRedirs
	} else {
		cfg.MaxRedirs = opts.MaxRedirs
	}

	if opts.LimitRate != "" {
		cfg.LimitRate, err = parseRateLimit(opts.LimitRate)
		if err != nil {
			return nil, fmt.Errorf("invalid limit-rate %s: %w", opts.LimitRate, err)
		}
	}

	if opts.ContinueAt != "" && opts.ContinueAt != "-" {
		if _, err = strconv.ParseInt(opts.ContinueAt, 10, 64); err != nil {
			return nil, fmt.Errorf("invalid continue-at %s: expected a byte offset or \"-\"", opts.ContinueAt)
		}
	}

	// -n/--netrc supplies credentials only when -u was not given.
	if opts.Netrc && cfg.User == "" {
		cfg.User, err = netrcCredentials(cfg.RequestURL.Hostname())
		if err != nil {
			return nil, err
		}
	}

	if opts.ProxyURL != "" {
		cfg.ProxyURL, err = url.Parse(opts.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL specified %s: %w", opts.ProxyURL, err)
		}
	}

	// --proxy-user overrides credentials embedded in --proxy.
	if opts.ProxyUser != "" {
		if cfg.ProxyURL == nil {
			return nil, fmt.Errorf("--proxy-user requires --proxy")
		}

		user, pass, _ := strings.Cut(opts.ProxyUser, ":")
		cfg.ProxyURL.User = url.UserPassword(user, pass)
	}

	if len(opts.ConnectTo) > 0 {
		cfg.ConnectTo, err = parseConnectTo(opts.ConnectTo)
		if err != nil {
			return nil, fmt.Errorf("invalid connect-to specified %v: %w", opts.ConnectTo, err)
		}
	}

	if len(opts.Resolve) > 0 {
		cfg.Resolve, err = parseResolve(opts.Resolve)
		if err != nil {
			return nil, fmt.Errorf("invalid resolve specified %v: %w", opts.Resolve, err)
		}
	}

	if opts.DNSServers != "" {
		cfg.DNSServers, err = parseDNSServers(opts.DNSServers, opts.Insecure)
		if err != nil {
			return nil, fmt.Errorf("invalid dns-servers specified %s: %w", opts.DNSServers, err)
		}
	}

	if len(opts.Headers) > 0 {
		cfg.Headers = createHeaders(opts.Headers)
		cfg.RawHeaders = opts.Headers
	}

	if opts.TLSv12 {
		cfg.TLSMinVersion = tls.VersionTLS12
	}

	if opts.TLSv13 {
		cfg.TLSMinVersion = tls.VersionTLS13
	}

	if opts.TLSMax == "1.2" {
		cfg.TLSMaxVersion = tls.VersionTLS12
	} else if opts.TLSMax == "1.3" {
		cfg.TLSMaxVersion = tls.VersionTLS13
	} else if opts.TLSMax != "" {
		return nil, fmt.Errorf("unsupported tls-max value: %s", opts.TLSMax)
	}

	if opts.TLSCiphers != "" {
		cipherNames := strings.Split(opts.TLSCiphers, " ")
		cfg.TLSCiphers = []uint16{}

		for _, cipherName := range cipherNames {
			cipher := getCipherSuiteByName(cipherName)

			if cipher == 0 {
				return nil, fmt.Errorf("cipher %s not found", cipherName)
			}

			cfg.TLSCiphers = append(cfg.TLSCiphers, cipher)
		}
	}

	if opts.TLSSplitHello != "" {
		cfg.TLSSplitChunkSize, cfg.TLSSplitDelay, err = parseTLSSplitHello(opts.TLSSplitHello)
		if err != nil {
			return nil, fmt.Errorf("invalid tls-split-hello: %w", err)
		}
	}

	if opts.TLSRandom != "" {
		var b []byte
		b, err = base64.StdEncoding.DecodeString(opts.TLSRandom)
		if err != nil {
			return nil, fmt.Errorf("--tls-random must be a valid base64 string: %w", err)
		}
		if len(b) != 32 {
			return nil, fmt.Errorf("--tls-random must decode to exactly 32 bytes (got %d)", len(b))
		}
		cfg.TLSRandom = b
	}

	if opts.ECHConfig != "" {
		cfg.ECHConfigs, err = unmarshalECHConfigs(opts.ECHConfig)
		if err != nil {
			return nil, fmt.Errorf("invalid echconfig: %w", err)
		}

		// --echconfig implicitly enables --ech as well.
		cfg.ECH = true
	}

	// --impersonate / --no-impersonate resolution: "none" or the boolean flag
	// disables impersonation; omitted flag falls back to the registry default.
	if opts.NoImpersonate {
		cfg.Impersonate = "none"
	} else if opts.Impersonate != "" {
		cfg.Impersonate = opts.Impersonate
	} else {
		cfg.Impersonate, err = profile.DefaultProfileName()
		if err != nil {
			return nil, err
		}
	}

	if len(opts.Experiments) > 0 {
		cfg.Experiments, err = parseExperiments(opts.Experiments)
		if err != nil {
			return nil, fmt.Errorf("invalid experiments %v: %w", opts.Experiments, err)
		}
	}

	// Parse and validate OHTTP options.
	if opts.OHTTPGatewayURL != "" || opts.OHTTPKeysURL != "" { // Both arguments must be specified together.
		if opts.OHTTPGatewayURL == "" || opts.OHTTPKeysURL == "" {
			return nil, fmt.Errorf("both --ohttp-gateway-url and --ohttp-keys-url must be specified")
		}

		cfg.OHTTPGatewayURL, err = url.Parse(opts.OHTTPGatewayURL)
		if err != nil {
			return nil, fmt.Errorf("invalid OHTTP gateway URL %s: %w", opts.OHTTPGatewayURL, err)
		}

		cfg.OHTTPKeysURL, err = url.Parse(opts.OHTTPKeysURL)
		if err != nil {
			return nil, fmt.Errorf("invalid OHTTP keys URL %s: %w", opts.OHTTPKeysURL, err)
		}
	}

	return cfg, nil
}

// DefaultMaxRedirs is curl's default redirect limit for -L.
const DefaultMaxRedirs = 50

// parseRateLimit parses a --limit-rate value ("100k", "1M", "512") into bytes
// per second.  Suffixes are k/K (1024), m/M (1024^2) and g/G (1024^3).
func parseRateLimit(s string) (bytesPerSec int64, err error) {
	mult := int64(1)
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'k', 'K':
			mult, s = 1024, s[:len(s)-1]
		case 'm', 'M':
			mult, s = 1024*1024, s[:len(s)-1]
		case 'g', 'G':
			mult, s = 1024*1024*1024, s[:len(s)-1]
		}
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("expected a number with an optional k/M/G suffix")
	}
	if n <= 0 {
		return 0, fmt.Errorf("rate must be positive")
	}

	return n * mult, nil
}

// netrcCredentials looks up the machine entry for host in ~/.netrc and returns
// its "login:password" pair.  An absent file is not an error (curl treats it
// the same way); a malformed file is.
func netrcCredentials(host string) (creds string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home directory for --netrc: %w", err)
	}

	b, err := os.ReadFile(filepath.Join(home, ".netrc"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("cannot read ~/.netrc: %w", err)
	}

	fields := strings.Fields(string(b))
	var login, password string
	matched := false
	// Entries are "machine HOST login USER password PASS", optionally followed
	// by a "default" catch-all.  Tokens are consumed in order.
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "machine":
			if i+1 >= len(fields) {
				return "", fmt.Errorf("malformed ~/.netrc: machine without a host")
			}
			matched = fields[i+1] == host
			login, password = "", ""
			i++
		case "default":
			matched = true
			login, password = "", ""
		case "login":
			if i+1 < len(fields) && matched {
				login = fields[i+1]
			}
			i++
		case "password":
			if i+1 < len(fields) && matched {
				password = fields[i+1]
			}
			i++
		}
	}

	if login == "" && !matched {
		return "", nil
	}

	return login + ":" + password, nil
}

// getCipherSuiteByName tries to get the cipher suite by its name. Returns 0
// if no matching cipher found.
func getCipherSuiteByName(cipherName string) (cipher uint16) {
	for _, c := range tls.CipherSuites() {
		if c.Name == cipherName {
			return c.ID
		}
	}

	for _, c := range tls.InsecureCipherSuites() {
		if c.Name == cipherName {
			return c.ID
		}
	}

	return 0
}

// parseConnectTo creates a "connect-to" map from the string representation.
func parseConnectTo(connectTo []string) (m map[string]string, err error) {
	m = map[string]string{}
	for _, ct := range connectTo {
		parts := strings.SplitN(ct, ":", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid connect-to format %s, expected HOST1:PORT1:HOST2:PORT2", ct)
		}

		oldHost := parts[0] + ":" + parts[1]
		newHost := parts[2] + ":" + parts[3]
		m[oldHost] = newHost
	}

	return m, nil
}

// parseResolve creates a "resolve" map from the string representation.
func parseResolve(resolve []string) (m map[string][]net.IP, err error) {
	m = map[string][]net.IP{}

	for _, r := range resolve {
		parts := strings.SplitN(r, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid resolve format %s, expected HOST:PORT:ADDRS", r)
		}

		host := parts[0]
		addrs := parts[2]
		var ipAddresses []net.IP

		for _, a := range strings.Split(addrs, ",") {
			ipAddr := net.ParseIP(a)
			if ipAddr == nil {
				return nil, fmt.Errorf("invalid addr %s", a)
			}

			// Trim zero bytes.
			if ipAddr.To4() != nil {
				ipAddr = ipAddr.To4()
			}

			ipAddresses = append(ipAddresses, ipAddr)
		}

		if len(ipAddresses) == 0 {
			return nil, fmt.Errorf("no addrs for %s", host)
		}

		m[host] = ipAddresses
	}

	return m, nil
}

// parseDNSServers parses --dns-servers command-line argument and returns the
// list of upstream.Upstream created from them.  If insecure is true and the
// upstreams use encrypted DNS, certificate verification will be disabled for
// them.
func parseDNSServers(dnsServers string, insecure bool) (upstreams []upstream.Upstream, err error) {
	addrs := strings.Split(dnsServers, ",")
	for _, addr := range addrs {
		opts := &upstream.Options{InsecureSkipVerify: insecure}
		u, uErr := upstream.AddressToUpstream(addr, opts)
		if uErr != nil {
			return nil, fmt.Errorf("invalid DNS server %s: %w", addr, uErr)
		}

		upstreams = append(upstreams, u)
	}

	return upstreams, nil
}

// createHeaders creates HTTP headers map from the string array.
func createHeaders(headers []string) (h http.Header) {
	h = http.Header{}

	for _, header := range headers {
		parts := strings.SplitN(header, ":", 2)

		headerName := parts[0]
		headerValue := ""
		if len(parts) == 2 {
			headerValue = parts[1]
		}

		h.Add(headerName, headerValue)
	}

	return h
}

// parseTLSSplitHello parses --tls-split-hello, returns error if it's invalid.
func parseTLSSplitHello(tlsSplitHello string) (chunkSize int, delay int, err error) {
	parts := strings.SplitN(tlsSplitHello, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid tls-split-hello format: %s", tlsSplitHello)
	}

	chunkSize, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid tls-split-hello: %w", err)
	}

	delay, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid tls-split-hello: %w", err)
	}

	return chunkSize, delay, nil
}

// unmarshalECHConfigs parses the base64-encoded ECH config.
func unmarshalECHConfigs(echConfig string) (echConfigs []ctls.ECHConfig, err error) {
	var b []byte
	b, err = base64.StdEncoding.DecodeString(echConfig)
	if err != nil {
		return nil, err
	}

	return ctls.UnmarshalECHConfigs(b)
}

// parseExperiments parses the --experiment command-line arguments into a map.
// Returns an error if the experiment name is invalid.
func parseExperiments(exps []string) (expMap map[Experiment]string, err error) {
	expMap = map[Experiment]string{}

	for _, exp := range exps {
		parts := strings.SplitN(exp, ":", 2)
		expName := parts[0]
		var value string
		if len(parts) == 2 {
			value = parts[1]
		}

		var e Experiment
		e, err = NewExperiment(expName)
		if err != nil {
			return nil, fmt.Errorf("invalid experiment: %s", exp)
		}

		expMap[e] = value
	}

	return expMap, nil
}
