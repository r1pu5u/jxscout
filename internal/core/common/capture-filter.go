package common

import (
	"net/url"
	"path"
	"strings"
)

// IsCaptureNoise excludes browser icons and Cloudflare challenge machinery,
// which otherwise creates deep, rotating token directories unrelated to app JS.
func IsCaptureNoise(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	p := strings.ToLower(u.Path)
	return strings.HasPrefix(p, "/cdn-cgi/challenge-platform/") || strings.HasPrefix(p, "/cdn-cgi/scripts/") || path.Base(p) == "favicon.ico" || strings.HasSuffix(p, ".ico")
}

func IsStaticAssetURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".js", ".mjs", ".cjs", ".css", ".map", ".json", ".ico", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".woff", ".woff2", ".ttf", ".pdf":
		return true
	}
	return false
}
