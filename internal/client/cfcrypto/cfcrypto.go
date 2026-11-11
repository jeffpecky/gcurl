// Package cfcrypto is a package that uses Cloudflare's TLS fork to provide
// features missing in crypto/tls.
package cfcrypto

import (
	"crypto/tls"
	"net"

	ctls "github.com/ameshkov/cfcrypto/tls"
	"github.com/jeffpecky/gcurl/internal/config"
	"github.com/jeffpecky/gcurl/internal/output"
	"github.com/jeffpecky/gcurl/internal/resolve"
)

// Handshake attempts to establish a TLS connection using Cloudflare's TLS fork.
//
// Depending on the arguments, it may do the following:
//
//   - Encrypted ClientHello.
//   - Post-quantum cryptography.
//
// # Arguments
//
//   - conn is the underlying network connection that should already be
//     established.
//   - tlsConfig is the original tls.Config, its properties will be copied to
//     the ctls.Config used by this method.
//   - resolver is specified enables ECH support.
//   - cfg is the *config.Config configuration object.
//   - out is the *output.Output object that is used to write logs.
//
// # Encrypted ClientHello
//
// It is used if enabled in the cfg argument. A few things about the tlsConfig
// that is passed to it:
//
//   - ServerName will be used in the inner ClientHello.  For the outer
//     ClientHello it will attempt to use the "public name" field of the ECH
//     configuration.
//   - Regarding the multiple ECHConfig passed, it chooses the first with
//     a suitable cipher suite which effectively means that it will almost
//     always simply use the first ECHConfig from the slice.
//
// # Post-quantum cryptography
//
// This basically means that new curves will be added to CurvePreferences.
func Handshake(
	conn net.Conn,
	tlsConfig *tls.Config,
	resolver *resolve.Resolver,
	cfg *config.Config,
	out *output.Output,
) (tlsConn net.Conn, err error) {
	out.Debug("Attempting to establish a TLS connection")

	var echConfigs []ctls.ECHConfig
	if cfg.ECH {
		echConfigs, err = resolver.LookupECHConfigs(tlsConfig.ServerName)
		if err != nil {
			// Continue even if ECH config is not found.
			out.Info(
				"Warning: ECH config not found for %s: %v",
				tlsConfig.ServerName,
				err,
			)
		}
	}

	_, postQuantum := cfg.Experiments[config.ExpPostQuantum]

	// Copying the original tls config fields to ECH-enabled one.
	conf := &ctls.Config{
		ServerName:         tlsConfig.ServerName,
		MinVersion:         tlsConfig.MinVersion,
		MaxVersion:         tlsConfig.MaxVersion,
		InsecureSkipVerify: tlsConfig.InsecureSkipVerify,
		NextProtos:         tlsConfig.NextProtos,
	}

	// Copy Rand if set (for --tls-random support with ECH and regular TLS)
	if tlsConfig.Rand != nil {
		conf.Rand = tlsConfig.Rand
	}

	// NextProtos is passed through untouched: the transport dials first and
	// routes on the negotiated ALPN, so it no longer depends on the conn being
	// a *tls.Conn to reach HTTP/2.

	if cfg.ECH || cfg.ECHGrease {
		conf.ECHEnabled = true
	}

	if len(echConfigs) > 0 {
		conf.ClientECHConfigs = echConfigs
	}

	if postQuantum {
		conf.CurvePreferences = []ctls.CurveID{
			ctls.X25519MLKEM768,
			ctls.X25519,
			ctls.CurveP256,
		}
	}

	c := ctls.Client(conn, conf)

	out.Debug("Starting TLS handshake")

	err = c.Handshake()
	if err != nil {
		return nil, err
	}

	out.Debug("TLS connection has been established successfully")

	return &connWrapper{
		baseConn: c,
	}, nil
}
