# gcurl

[![Go Report Card](https://goreportcard.com/badge/github.com/jeffpecky/gcurl)](https://goreportcard.com/report/jeffpecky/gcurl)
[![Latest release](https://img.shields.io/github/release/jeffpecky/gcurl/all.svg)](https://github.com/jeffpecky/gcurl/releases)

[`curl`](https://curl.se/) compatible tool written in Go that focuses on
features curl does not provide.

1. Supports a subset of curl options needed for its use cases.
2. Adds flags that curl does not support.
   [Read more about the new stuff](#newstuff).

- [Why in the world you need another curl?](#why)
- [How to install gcurl?](#install)
- [How to use gcurl?](#howtouse)
- [New stuff](#newstuff)
    - [Encrypted ClientHello](#ech)
    - [Custom DNS servers](#dns)
    - [Oblivious HTTP](#ohttp)
    - [Experimental flags](#exp)
        - [Post-quantum cryptography](#pq)
    - [WebSocket support](#websocket)
- [Development](#development)
- [All command-line arguments](#allcmdarguments)

<a id="why"></a>

## Why in the world you need another curl?

Curl is certainly awesome, but sometimes I need to have better control over
what's happening on the inside and to be able to debug it. It seemed easier to
me to implement the necessary parts of curl in Go.

Also, I'd like to be able to extend it with what fits my specific needs.
Unfortunately, curl is a bit too huge for that now.

<a id="install"></a>

## How to install gcurl?

- From source:

    ```shell
    go install github.com/jeffpecky/gcurl@latest
    ```

- You can use [a Docker image][dockerimage]:

    ```shell
    docker run --rm ghcr.io/jeffpecky/gcurl --help
    ```

- You can get a binary from the [releases page][releases].

[dockerimage]: https://github.com/jeffpecky/gcurl/pkgs/container/gcurl

[releases]: https://github.com/jeffpecky/gcurl/releases

<a id="howtouse"></a>

## How to use gcurl?

Use it the same way you use original curl.

- `gcurl https://httpbin.agrd.workers.dev/get` make a `GET` request.
- `gcurl -d "test" -v https://httpbin.agrd.workers.dev/post` make a `POST`
  request with `test` data.
- `gcurl -I https://httpbin.agrd.workers.dev/head` make a `HEAD` request.
- `gcurl -I --insecure https://expired.badssl.com/` do not verify TLS
  certificate.
- `gcurl -I --http1.1 https://httpbin.agrd.workers.dev/head` force use
  HTTP/1.1.
- `gcurl -I --http2 https://httpbin.agrd.workers.dev/head` force use HTTP/2.
- `gcurl -I --http3 https://httpbin.agrd.workers.dev/head` force use HTTP/3.
- `gcurl -x socks5://user:pass@host:port https://httpbin.agrd.workers.dev/get`
  use a proxy server.
- `gcurl -I --tlsv1.3 https://tls-v1-2.badssl.com:1012/` force use TLS v1.3.
- `gcurl -I --connect-to "httpbin.agrd.workers.dev:443:172.67.152.85:443"
  https://httpbin.agrd.workers.dev/head` connect to the specified IP addresses.
- `gcurl -I --resolve "httpbin.agrd.workers.dev:443:172.67.152.85"
  https://httpbin.agrd.workers.dev/head` resolve the hostname to the specified
  IP address. Note, that unlike `curl`, `gcurl` ignores port in this option.
- `gcurl -I --connect-timeout 3 http://10.255.255.1:9999/test`
  set connection timeout to 3 seconds.

<a id="newstuff"></a>

### New stuff

Also, you can use some new stuff that is not supported by curl.

- `gcurl --impersonate chrome-133 https://tls.peet.ws/api/all` impersonates a
  real browser's TLS and HTTP/2 fingerprint (ClientHello, SETTINGS order,
  header order).  More on this [below](#impersonate).

- `gcurl --json-output https://httpbin.agrd.workers.dev/get` write output in
  machine-readable format (JSON).
- `gcurl --tls-split-hello 5:50 https://httpbin.agrd.workers.dev/get` split
  TLS ClientHello in two parts and make a 50ms delay after sending the first
  part.
- `gcurl --tls-random "gyufwmGYeIiq0B4nUjEYu3NcqVdlHbIXhx74fq4terc=" https://httpbin.agrd.workers.dev/get`
  use a custom TLS ClientHello random value.
- `gcurl -v --ech https://crypto.cloudflare.com/cdn-cgi/trace` enables support
  for ECH (Encrypted Client Hello) for the request. More on this [below](#ech).
- `gcurl --dns-servers "tls://dns.google" https://httpbin.agrd.workers.dev/get`
  uses custom DNS-over-TLS server to resolve hostnames. More on this
  [below](#dns).
- `gcurl --experiment pq https://pq.cloudflareresearch.com/` enables
  post-quantum cryptography support for the request. More on this [below](#pq).
- `gcurl wss://httpbin.agrd.workers.dev/ws` sends a WS upgrade request.
- `gcurl -d "test message" wss://httpbin.agrd.workers.dev/ws` establishes a WS
  connection, sends the first message through it and reads the response.

<a id="impersonate"></a>

#### TLS/HTTP fingerprint impersonation

`--impersonate <profile>` makes gcurl present the TLS ClientHello and HTTP/2
fingerprint of a real browser instead of Go's default stack.  Available
profiles (from `internal/profile/profiles/`):

| Profile | Browser |
| --- | --- |
| `chrome-133` | Chrome 133 (default) |
| `chrome-131` | Chrome 131 |
| `chrome-120` | Chrome 120 |
| `firefox-120` | Firefox 120 |
| `firefox-105` | Firefox 105 |
| `safari-16` | Safari 16.0 (macOS) |
| `ios-14` | iOS 14 (Safari) |
| `edge-85` | Edge 85 (Chromium) |

```shell
# Explicit profile:
gcurl --impersonate firefox-120 https://tls.peet.ws/api/all

# The default profile is used even without the flag:
gcurl https://tls.peet.ws/api/all

# Stock Go TLS stack (for comparison/debugging):
gcurl --no-impersonate https://tls.peet.ws/api/all
```

A profile bundles a uTLS ClientHello preset (cipher suites, extension
order, GREASE, key exchange) and an HTTP/2 fingerprint (SETTINGS values and
order, connection WINDOW_UPDATE, pseudo-header order, and regular header
order — the last one taken from the order of your `-H` flags).

When a new browser version needs supporting, add a JSON file to
`internal/profile/profiles/` — no code changes.  To pin and later re-verify
its fingerprints:

```shell
make capture PROFILE=chrome-134   # prints the fingerprints block to paste
# paste it into internal/profile/profiles/chrome-134.json, commit
make selftest                     # fails the build if a fingerprint drifts
```

The `make selftest` gate (also wired as a GitHub workflow) hits the
[fingerprint echo endpoint][peetws] through the real binary for every
profile that declares hashes and fails on drift.

[peetws]: https://tls.peet.ws/api/all

<a id="ech"></a>

#### Encrypted ClientHello

ECH or Encrypted Client Hello is a new standard that allows completely
encrypting TLS Client Hello. Currently, the RFC is in the [draft stage][echrfc],
but it is already supported by some big names [like Cloudflare][echcloudflare].
`gcurl` supports ECH and provides several options to use it.

The simple option is just add `--ech` flag and see what happens:

```shell
gcurl -v --ech https://crypto.cloudflare.com/cdn-cgi/trace
```

In this case, `gcurl` will try to discover the ECH configuration from DNS
records and then use them to establish the connection.

Instead of that, you may choose to supply your own configuration in the same
base64-encoded format as used by the SVCB record:

```shell
# Send a type=https query and find ech record there.
% dig -t https crypto.cloudflare.com. +short
1 . alpn="http/1.1,h2" ipv4hint=162.159.137.85,162.159.138.85 ech=AEX+DQBBvgAgACARWS42g5NmDZo5pIpTWSwHzTwzdRKPdUW732QbyUeyDQAEAAEAAQASY2xvdWRmbGFyZS1lY2guY29tAAA= ipv6hint=2606:4700:7::a29f:8955,2606:4700:7::a29f:8a55

# You can now pass it to gcurl.
gcurl -v \
  --echconfig "AEX+DQBBvgAgACARWS42g5NmDZo5pIpTWSwHzTwzdRKPdUW732QbyUeyDQAEAAEAAQASY2xvdWRmbGFyZS1lY2guY29tAAA=" \
  https://crypto.cloudflare.com/cdn-cgi/trace
```

> Interesting thing about ECH is that it may connect even if you use an expired
> configuration (see HelloRetryRequest in the RFC). It depends on both the
> server and the client implementation and does not work with Cloudflare at the
> moment.

Here's what happens under the hood:

1. `gcurl` resolves `crypto.cloudflare.com` IP address and connects to it.
2. It sends TLS ClientHello (outer) with encrypted inner ClientHello to that IP
   address. The ServerName field in the outer ClientHello is set to the one that
   is encoded in the ECH configuration (in this example it will be
   `cloudflare-ech.com`), and in the inner encrypted ClientHello it will be
   set to `crypto.cloudflare.com`.

You may want to configure a specific "client-facing" server instead and the way
to do that is to use `--connect-to`. Let's send a request to `cloudflare.com`
and use `crypto.cloudflare.com` as a client-facing server for that.

```shell
gcurl -v \
  --connect-to "cloudflare.com:443:crypto.cloudflare.com:443" \
  --echconfig "AEX+DQBBvgAgACARWS42g5NmDZo5pIpTWSwHzTwzdRKPdUW732QbyUeyDQAEAAEAAQASY2xvdWRmbGFyZS1lY2guY29tAAA=" \
  https://cloudflare.com/cdn-cgi/trace
```

> For this command to work you may need to replace `--echconfig` with the
> current one discovered using DNS as was explained before.

Here's what happens now:

1. `gcurl` connects to `crypto.cloudflare.com` (client-facing relay).
2. Sends a TLS ClientHello with `cloudflare-ech.com` in the Server Name
   extension.
3. Establishes a TLS connection with `cloudflare.com` using the inner encrypted
   ClientHello.

[echrfc]: https://datatracker.ietf.org/doc/draft-ietf-tls-esni/

[echcloudflare]: https://blog.cloudflare.com/handshake-encryption-endgame-an-ech-update/

<a id="dns"></a>

#### Custom DNS servers

`gcurl` allows using custom DNS servers to resolve hostnames when making
requests. This can be achieved by using `--dns-servers` command-line argument.
`curl` with `c-ares` also supports this command-line argument, but `gcurl`
adds encrypted DNS support on top of it, it supports all popular DNS encryption
protocols: DNS-over-QUIC, DNS-over-HTTPS, DNS-over-TLS and DNSCrypt.

You can specify multiple DNS servers, in this case `gcurl` will attempt to use
them one by one until it receives a response or until all of them fail:

```shell
gcurl \
  --dns-servers "tls://dns.adguard-dns.com,tls://dns.google" \
  https://example.org/

```

- DNS-over-QUIC

  ```shell
  gcurl --dns-servers "quic://dns.adguard-dns.com" https://example.org/
  ```

- DNS-over-HTTPS

  ```shell
  gcurl --dns-servers "https://dns.adguard-dns.com/dns-query" https://example.org/
  ```

- DNS-over-TLS

  ```shell
  gcurl --dns-servers "tls://dns.adguard-dns.com" https://example.org/
  ```

- DNSCrypt

  ```shell
  gcurl \
      --dns-servers sdns://AQIAAAAAAAAAFDE3Ni4xMDMuMTMwLjEzMDo1NDQzINErR_JS3PLCu_iZEIbq95zkSV2LFsigxDIuUso_OQhzIjIuZG5zY3J5cHQuZGVmYXVsdC5uczEuYWRndWFyZC5jb20 \
      https://example.org/
  ```

<a id="ohttp"></a>

#### Oblivious HTTP

[Oblivious HTTP (OHTTP)][ohttp] is an IETF standard protocol that provides
end-to-end encryption for HTTP requests and responses while hiding the client's
identity from the target server. It works by routing encrypted requests through
a relay and gateway, ensuring that:

- The **relay** sees who is making requests but not what is being requested.
- The **gateway** sees what is being requested but not who is making the request.
- The **target server** receives a normal HTTP request from the gateway.

This separation provides strong privacy guarantees, making OHTTP useful for
privacy-sensitive applications.

`gcurl` has built-in support for OHTTP and can send requests through any
OHTTP gateway by specifying two command-line arguments:

- `--ohttp-gateway-url` - URL of the OHTTP gateway where the encrypted request
  will be sent.
- `--ohttp-keys-url` - URL from which to retrieve the OHTTP KeyConfig needed to
  encrypt the request.

Here's how to make an OHTTP request to `httpbin.agrd.workers.dev` using a demo
gateway:

```shell
gcurl -v \
  --ohttp-gateway-url "https://httpbin.agrd.workers.dev/ohttp/gateway" \
  --ohttp-keys-url "https://httpbin.agrd.workers.dev/ohttp/config" \
  https://httpbin.agrd.workers.dev/get
```

This command will:

1. Download the OHTTP KeyConfig from the keys URL.
2. Encrypt your request to `https://httpbin.agrd.workers.dev/get` using OHTTP.
3. Send the encrypted request to the gateway.
4. Receive the encrypted response from the gateway.
5. Decrypt and display the response.

You can also make POST requests through OHTTP:

```shell
gcurl -v \
  --ohttp-gateway-url "https://httpbin.agrd.workers.dev/ohttp/gateway" \
  --ohttp-keys-url "https://httpbin.agrd.workers.dev/ohttp/config" \
  -d "test data" \
  https://httpbin.agrd.workers.dev/post
```

One more example that uses a demo gateway from [Oblivious Network][ohttpdemo]:

```shell
gcurl -v \
  --ohttp-gateway-url "https://demo-gateway.oblivious.network/gateway" \
  --ohttp-keys-url "https://demo-gateway.oblivious.network/ohttp-configs" \
  https://httpbin.agrd.workers.dev/get
```

[ohttp]: https://www.ietf.org/rfc/rfc9458.html

[ohttpdemo]: https://docs.oblivious.network/docs/quickstart/

<a id="websocket"></a>

#### WebSocket support

`gcurl` provides some initial support for WebSocket protocol. It may be
extended in the future, see the corresponding [Github issue][wsissue].

- `gcurl wss://httpbin.agrd.workers.dev/ws` sends a WS upgrade request.
- `gcurl -d "test message" wss://httpbin.agrd.workers.dev/ws` establishes a WS
  connection, sends the first message through it and reads the response.

[wsissue]: https://github.com/jeffpecky/gcurl/issues/17

<a id="exp"></a>

#### Experimental flags

Experimental flags are added to `gcurl` whenever there's a feature that may be
completely changed or removed in the future. Experiments can be enabled using
the `--experiment=<name[:value]>` argument where `name` is the experiment name
and `value` is an optional string value (the need for it depends on the actual
experiment).

<a id="pq"></a>

##### Post-quantum cryptography

Post-quantum (PQ) cryptography has been designed to be secure against the
threat of quantum computers. You can learn more about it from Cloudflare's
[blog post][postquantum]. `gcurl` supports it via the `--experiment=pq` flag.

Note, that it is not available for `--http3` at the moment.

```shell
gcurl --experiment pq https://pq.cloudflareresearch.com/
```

[postquantum]: https://blog.cloudflare.com/post-quantum-for-all/

<a id="development"></a>

## Development

To set up this repository for development, clone it and run:

```shell
make init
```

`make init` installs a git **pre-commit** hook to
`.git/hooks/pre-commit`. On every commit the hook:

1. Checks the staged content for temporary todo markers (`FIXME` and `TODO!!`)
   and blocks the commit if any are found. Note that documentation files (like
   `README.md`) are exempt from this check, since they may legitimately
   mention those markers.
2. Runs `make check`, the same quality gate as CI, which runs linters and
   tests.

The hook is stored in [`.githooks/pre-commit`](./.githooks/pre-commit) so you
can review exactly what it does. Re-run `make init` to re-install it after
pulling changes.

Requirements:

- [Go](https://go.dev/) (see `go.mod` for the required version).
- [golangci-lint](https://golangci-lint.run/).
- [markdownlint-cli](https://github.com/igorshubovych/markdownlint-cli) (run
  via `npx`, needs Node.js).
- `make` and `git`.

Useful commands:

- `make lint` — run `markdownlint-cli` and `golangci-lint`.
- `make test` — run the test suite.
- `make check` — run `make lint` and `make test` in sequence.
- `make build` — build the `gcurl` binary.

<a id="allcmdarguments"></a>

## All command-line arguments

```shell
Usage:
  gcurl [OPTIONS]

Application Options:
      --url=<URL>                                           URL the request will be made to. Can be specified without
                                                            any flags.
  -X, --request=<method>                                    HTTP method. GET by default.
  -d, --data=<data>                                         Sends the specified data to the HTTP server using content
                                                            type application/x-www-form-urlencoded.
  -H, --header=                                             Extra header to include in the request. Can be specified
                                                            multiple times.
  -x, --proxy=[protocol://username:password@]host[:port]    Use the specified proxy. The proxy string can be specified
                                                            with a protocol:// prefix.
      --connect-to=<HOST1:PORT1:HOST2:PORT2>                For a request to the given HOST1:PORT1 pair, connect to
                                                            HOST2:PORT2 instead. Can be specified multiple times.
      --connect-timeout=<seconds>                           Maximum time in seconds allowed for the connection phase.
  -I, --head                                                Fetch the headers only.
  -k, --insecure                                            Disables TLS verification of the connection.
      --tlsv1.3                                             Forces gcurl to use TLS v1.3 or newer.
      --tlsv1.2                                             Forces gcurl to use TLS v1.2 or newer.
      --tls-max=<VERSION>                                   (TLS) VERSION defines maximum supported TLS version. Can be
                                                            1.2 or 1.3. The minimum acceptable version is set by
                                                            tlsv1.2 or tlsv1.3.
      --ciphers=<space-separated list of ciphers>           Specifies which ciphers to use in the connection, see
                                                            https://go.dev/src/crypto/tls/cipher_suites.go for the full
                                                            list of available ciphers.
      --tls-servername=<HOSTNAME>                           Specifies the server name that will be sent in TLS
                                                            ClientHello
      --http1.1                                             Forces gcurl to use HTTP v1.1.
      --http2                                               Forces gcurl to use HTTP v2.
      --http3                                               Forces gcurl to use HTTP v3.
      --ech                                                 Enables ECH support for the request.
      --echgrease                                           Forces sending ECH grease in the ClientHello, but does not
                                                            try to resolve the ECH configuration.
      --echconfig=<base64-encoded data>                     ECH configuration to use for this request. Implicitly
                                                            enables --ech when specified.
  -4, --ipv4                                                This option tells gcurl to use IPv4 addresses only when
                                                            resolving host names.
  -6, --ipv6                                                This option tells gcurl to use IPv6 addresses only when
                                                            resolving host names.
      --dns-servers=<DNSADDR1,DNSADDR2>                     DNS servers to use when making the request. Supports
                                                            encrypted DNS: tls://, https://, quic://, sdns://
      --resolve=<[+]host:port:addr[,addr]...>               Provide a custom address for a specific host. port is
                                                            ignored by gcurl. '*' can be used instead of the host
                                                            name. Can be specified multiple times.
      --tls-split-hello=<CHUNKSIZE:DELAY>                   An option that allows splitting TLS ClientHello in two
                                                            parts in order to avoid common DPI systems detecting TLS.
                                                            CHUNKSIZE is the size of the first bytes before ClientHello
                                                            is split, DELAY is delay in milliseconds before sending the
                                                            second part.
      --tls-random=<base64>                                 Base64-encoded 32-byte TLS ClientHello random value.
      --json-output                                         Makes gcurl write machine-readable output in JSON format.
  -o, --output=<file>                                       Defines where to write the received data. If not set,
                                                            gcurl will write everything to stdout.
      --experiment=<name[:value]>                           Allows enabling experimental options. See the documentation
                                                            for available options. Can be specified multiple times.
      --ohttp-gateway-url=<URL>                             URL of the Oblivious HTTP gateway where the request should
                                                            be sent.
      --ohttp-keys-url=<URL>                                URL from which to retrieve Oblivious HTTP KeyConfig to use
                                                            for encrypting the request.
  -v, --verbose                                             Verbose output (optional).

Help Options:
  -h, --help                                                Show this help message
```
