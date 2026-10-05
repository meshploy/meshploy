package mcpserver

import (
	_ "embed"
	"encoding/base64"

	"github.com/mark3labs/mcp-go/mcp"
)

// Meshploy's mark, as the console's own favicon, for clients that show a
// server's icon. Data URIs, so they show whether the server is the gateway's
// /mcp or a local meshploy mcp with nothing to fetch from.
var (
	//go:embed icon/meshploy.svg
	iconSVG []byte
	//go:embed icon/meshploy.png
	iconPNG []byte
)

func icons() []mcp.Icon {
	return []mcp.Icon{
		{Src: "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(iconSVG), MIMEType: "image/svg+xml", Sizes: []string{"any"}},
		{Src: "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconPNG), MIMEType: "image/png", Sizes: []string{"180x180"}},
	}
}
