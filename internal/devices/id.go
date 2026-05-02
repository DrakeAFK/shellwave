package devices

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
)

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func NewID(parts ...string) string {
	joined := strings.Join(parts, " ")
	slug := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(joined), "-"), "-")
	if slug == "" {
		slug = "device"
	}
	if len(slug) > 32 {
		slug = strings.Trim(slug[:32], "-")
	}
	sum := sha1.Sum([]byte(joined))
	return slug + "-" + hex.EncodeToString(sum[:])[:8]
}
