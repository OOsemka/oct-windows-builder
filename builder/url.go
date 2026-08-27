package main

import (
	"net/url"
	"os"
	"regexp"
	"strings"
)

var dns1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validDiskName(name string) bool {
	return len(name) > 0 && len(name) <= 50 && dns1123.MatchString(name)
}

// sanitizeURLForLog returns host + path only (no userinfo, query, or fragment).
func sanitizeURLForLog(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "(unparseable-url)"
	}
	if u.Host == "" {
		return u.Scheme + ":(redacted)"
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.Scheme == "" {
		return u.Host + path
	}
	return u.Scheme + "://" + u.Host + path
}

func envOr(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}
