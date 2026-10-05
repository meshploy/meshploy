package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/meshploy/packages/client"
)

// A client that asks who the server is gets Meshploy's mark, in a form every
// client may render (PNG) and the sharp one (SVG).
func TestTheServerIntroducesItselfWithItsIcon(t *testing.T) {
	ms := New(client.New("http://127.0.0.1:1", ""), "org")
	resp := ms.HandleMessage(context.Background(), json.RawMessage(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
	b, _ := json.Marshal(resp)
	var out struct {
		Result struct {
			ServerInfo struct {
				Name  string `json:"name"`
				Icons []struct {
					Src      string `json:"src"`
					MIMEType string `json:"mimeType"`
				} `json:"icons"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	icons := out.Result.ServerInfo.Icons
	if out.Result.ServerInfo.Name != "meshploy" || len(icons) != 2 ||
		!strings.HasPrefix(icons[0].Src, "data:image/svg+xml;base64,") || !strings.HasPrefix(icons[1].Src, "data:image/png;base64,") {
		t.Fatalf("server info: %s", b)
	}
}
