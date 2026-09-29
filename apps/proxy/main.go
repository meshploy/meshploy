// The proxy image: Meshploy's edge reverse proxy, all of it in
// packages/proxy so that an edition can build its own proxy on it.
package main

import "github.com/meshploy/packages/proxy"

func main() {
	proxy.Main()
}
