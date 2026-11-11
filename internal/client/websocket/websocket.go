// Package websocket is responsible for handling WebSocket connections.
package websocket

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/jeffpecky/gcurl/internal/output"
)

// acceptGUID is the RFC 6455 constant appended to the client key before
// hashing to form Sec-WebSocket-Accept.
const acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// NewWebSocket returns an io.ReadWriterCloser that can be used to send and
// receive data to/from a websocket.  binary selects the Write opcode
// (OpBinary for --data-binary, OpText otherwise).
func NewWebSocket(conn net.Conn, out *output.Output, binary bool) (rwc io.ReadWriteCloser) {
	r := wsutil.NewReader(conn, ws.StateClientSide)

	op := ws.OpText
	if binary {
		op = ws.OpBinary
	}
	w := wsutil.NewWriter(conn, ws.StateClientSide, op)

	return &wsConn{
		conn: conn,
		r:    r,
		w:    w,
		out:  out,
	}
}

// wsConn represents a WebSocket connection that's been already initialized.
type wsConn struct {
	conn net.Conn
	r    *wsutil.Reader
	w    *wsutil.Writer
	out  *output.Output
}

// type check
var _ io.ReadWriteCloser = (*wsConn)(nil)

// Read implements the io.ReadWriteCloser interface for *wsConn. Returning
// io.EOF error does not mean that the reader is closed, it just means that all
// messages from the current frame has been read, so it should be safe to use
// io.ReadAll several times with this reader.
func (w *wsConn) Read(b []byte) (n int, err error) {
	n, err = w.r.Read(b)
	if err == wsutil.ErrNoFrameAdvance {
		w.out.Debug("Reading next WebSocket frame")

		hdr, fErr := w.r.NextFrame()
		if fErr != nil {
			return 0, io.EOF
		}

		w.out.Debug("Received frame with opcode=%d len=%d fin=%v", hdr.OpCode, hdr.Length, hdr.Fin)

		// Reading again in this case
		n, err = w.r.Read(b)
	}

	return n, err
}

// Write implements the io.ReadWriteCloser interface for *wsConn.
func (w *wsConn) Write(b []byte) (n int, err error) {
	w.out.Debug("Writing data of len=%d to the WebSocket", len(b))

	n, err = w.w.Write(b)
	if err != nil {
		return 0, err
	}

	err = w.w.Flush()

	return n, err
}

// Close implements the io.ReadWriteCloser interface for *wsConn.
func (w *wsConn) Close() (err error) {
	return w.conn.Close()
}

// IsWebSocketResponse checks if the response is a valid 101 Switching Protocols
// response and the following data should follow the WebSocket protocol.  When
// the response echoes Sec-WebSocket-Key it is verified against
// Sec-WebSocket-Accept per RFC 6455.
func IsWebSocketResponse(resp *http.Response) (ok bool) {
	if resp.StatusCode != http.StatusSwitchingProtocols ||
		!strings.EqualFold(resp.Header.Get("Connection"), "upgrade") {
		return false
	}

	accept := resp.Header.Get("Sec-Websocket-Accept")
	if accept == "" {
		return false
	}

	key := ""
	if resp.Request != nil {
		key = resp.Request.Header.Get("Sec-WebSocket-Key")
	}
	if key == "" {
		return true
	}

	return accept == acceptKey(key)
}

// acceptKey derives the Sec-WebSocket-Accept value for a client key.
func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + acceptGUID))

	return base64.StdEncoding.EncodeToString(h[:])
}

// IsWebSocket returns true if the request is to WebSocket.
func IsWebSocket(u *url.URL) (ok bool) {
	return u.Scheme == "ws" || u.Scheme == "wss"
}

// UpgradeWebSocket checks if the request r is a WebSocket requests and adds
// Upgrade header if needed.
func UpgradeWebSocket(r *http.Request) (upgradeReq *http.Request) {
	if !IsWebSocket(r.URL) {
		return nil
	}

	upgradeReq = r.Clone(r.Context())

	if upgradeReq.URL.Scheme == "ws" {
		upgradeReq.URL.Scheme = "http"
	}

	if upgradeReq.URL.Scheme == "wss" {
		upgradeReq.URL.Scheme = "https"
	}

	upgradeReq.Method = http.MethodGet
	upgradeReq.Header.Set("Connection", "Upgrade")
	upgradeReq.Header.Set("Upgrade", "websocket")
	upgradeReq.Header.Set("Sec-WebSocket-Version", "13")

	key := make([]byte, 16)
	// crypto/rand.Read never returns an error (Go 1.24+); a failed read would
	// have already crashed the process.
	_, _ = rand.Read(key)
	upgradeReq.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString(key))

	return upgradeReq
}
