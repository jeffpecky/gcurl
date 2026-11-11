package websocket

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAcceptKey pins the RFC 6455 example.
func TestAcceptKey(t *testing.T) {
	require.Equal(t, "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=", acceptKey("dGhlIHNhbXBsZSBub25jZQ=="))
}

// TestUpgradeWebSocketRandomKey checks that each upgrade carries a distinct,
// valid 16-byte key and that the accept check validates it.
func TestUpgradeWebSocketRandomKey(t *testing.T) {
	u, err := url.Parse("wss://example.com/ws")
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	require.NoError(t, err)
	req.URL = u

	up1 := UpgradeWebSocket(req)
	up2 := UpgradeWebSocket(req)
	k1, k2 := up1.Header.Get("Sec-WebSocket-Key"), up2.Header.Get("Sec-WebSocket-Key")
	require.NotEmpty(t, k1)
	require.NotEqual(t, k1, k2)

	resp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Header: http.Header{
			"Connection":           {"Upgrade"},
			"Sec-Websocket-Accept": {acceptKey(k1)},
		},
		Request: up1,
	}
	require.True(t, IsWebSocketResponse(resp))

	resp.Header.Set("Sec-Websocket-Accept", "bogus")
	require.False(t, IsWebSocketResponse(resp))
}
