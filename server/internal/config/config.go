// Package config reads Luma server settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL    string
	Listen         string
	PublicURL      string
	UploadDir      string
	MaxUploadBytes int64
	// Registration is "invite" (default), "open" or "closed".
	Registration string
	// TrustedProxy: read the client IP from CF-Connecting-IP / X-Forwarded-For,
	// but only on requests whose remote address is loopback (Caddy on the same host).
	TrustedProxy bool
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:  os.Getenv("LUMA_DATABASE_URL"),
		Listen:       env("LUMA_LISTEN", "127.0.0.1:8790"),
		PublicURL:    env("LUMA_PUBLIC_URL", "https://chat.folk3s.com"),
		UploadDir:    env("LUMA_UPLOAD_DIR", "./data/uploads"),
		Registration: env("LUMA_REGISTRATION", "invite"),
		TrustedProxy: os.Getenv("LUMA_TRUSTED_PROXY") == "1",
	}
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

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
