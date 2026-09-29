package cache

import "testing"

// Only the hostnames the old edge still serves go to it, whatever their case;
// with no fallback, nothing does.
func TestFallbackServesOnlyItsHostnames(t *testing.T) {
	c := New(nil, 0)
	if _, ok := c.FallbackFor("app.example.com"); ok {
		t.Fatal("no fallback set, yet one answered")
	}
	c.SetFallback(&Fallback{Upstream: "127.0.0.1:18080", Hostnames: map[string]bool{"app.example.com": true}})
	if up, ok := c.FallbackFor("App.Example.com"); !ok || up != "127.0.0.1:18080" {
		t.Fatalf("got %q %v", up, ok)
	}
	if _, ok := c.FallbackFor("other.example.com"); ok {
		t.Fatal("a hostname the old edge does not serve went to it")
	}
}
