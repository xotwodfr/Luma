package httpx

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP_OnlyTrustedPeersMayForward(t *testing.T) {
	gw := []netip.Prefix{netip.MustParsePrefix("172.16.79.1/32")}
	loop := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	for _, c := range []struct {
		peer    string
		trusted []netip.Prefix
		want    string
	}{
		{"172.16.79.1:5000", gw, "203.0.113.9"}, // Caddy via the docker bridge gateway
		{"172.16.79.2:5000", gw, "172.16.79.2"}, // another container: headers ignored
		{"127.0.0.1:5000", loop, "203.0.113.9"}, // bare-metal Caddy
		{"[::1]:5000", loop, "203.0.113.9"},
		{"198.51.100.7:5000", loop, "198.51.100.7"}, // direct hit: headers ignored
		{"127.0.0.1:5000", nil, "127.0.0.1"},        // trust off
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.peer
		r.Header.Set("CF-Connecting-IP", "203.0.113.9")
		if got := ClientIP(r, c.trusted); got != c.want {
			t.Errorf("peer %s: got %s, want %s", c.peer, got, c.want)
		}
	}
	// Without CF-Connecting-IP, only the hop Caddy appended counts, not what the client sent.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.16.79.1:5000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.20")
	if got := ClientIP(r, gw); got != "198.51.100.20" {
		t.Errorf("X-Forwarded-For: got %s, want the right-most entry", got)
	}
}
