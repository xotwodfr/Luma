// Package config reads Luma server settings from the environment.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL    string
	Listen         string
	PublicURL      string
	UploadDir      string
	MaxUploadBytes int64
	// Registration is "invite" (default), "open" or "closed".
	Registration string
	// TrustedProxies: read the client IP from CF-Connecting-IP / X-Forwarded-For, but only on
	// requests whose TCP peer is in one of these prefixes (Caddy on the same host). From
	// LUMA_TRUSTED_PROXY: "1" means loopback (bare-metal); in a container, set it to the bridge
	// gateway, which is where host connections to the published port come from.
	TrustedProxies []netip.Prefix
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:  os.Getenv("LUMA_DATABASE_URL"),
		Listen:       env("LUMA_LISTEN", "127.0.0.1:8790"),
		PublicURL:    env("LUMA_PUBLIC_URL", "https://chat.folk3s.com"),
		UploadDir:    env("LUMA_UPLOAD_DIR", "./data/uploads"),
		Registration: env("LUMA_REGISTRATION", "invite"),
	}
	tp, err := parseProxies(os.Getenv("LUMA_TRUSTED_PROXY"))
	if err != nil {
		return c, err
	}
	c.TrustedProxies = tp
	if c.DatabaseURL == "" {
		return c, errors.New("LUMA_DATABASE_URL is required")
	}
	switch c.Registration {
	case "invite", "open", "closed":
	default:
		return c, fmt.Errorf("LUMA_REGISTRATION must be invite, open or closed, got %q", c.Registration)
	}
	max, err := strconv.ParseInt(env("LUMA_MAX_UPLOAD_BYTES", "26214400"), 10, 64)
	if err != nil || max <= 0 {
		return c, fmt.Errorf("LUMA_MAX_UPLOAD_BYTES must be a positive integer")
	}
	c.MaxUploadBytes = max
	return c, nil
}

func parseProxies(v string) ([]netip.Prefix, error) {
	if v == "" {
		return nil, nil
	}
	if v == "1" {
		return []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}, nil
	}
	var out []netip.Prefix
	for _, f := range strings.Split(v, ",") {
		f = strings.TrimSpace(f)
		p, err := netip.ParsePrefix(f)
		if err != nil {
			a, aerr := netip.ParseAddr(f)
			if aerr != nil {
				return nil, fmt.Errorf("LUMA_TRUSTED_PROXY: %q is not an IP or CIDR", f)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		out = append(out, p)
	}
	return out, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
