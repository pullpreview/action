package pullpreview

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultMaxDomainLength = 62
)

var nonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9]+`)
var multiHyphen = regexp.MustCompile(`-+`)

func NormalizeName(name string) string {
	clean := nonAlphaNum.ReplaceAllString(name, "-")
	clean = strings.Trim(clean, "-")
	clean = strings.Join(strings.FieldsFunc(clean, func(r rune) bool { return r == '-' }), "-")
	if len(clean) > 61 {
		clean = clean[:61]
	}
	return strings.Trim(clean, "-")
}

func MaxDomainLength() int {
	value := strings.TrimSpace(os.Getenv("PULLPREVIEW_MAX_DOMAIN_LENGTH"))
	if value == "" {
		return defaultMaxDomainLength
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 || parsed > defaultMaxDomainLength {
		return defaultMaxDomainLength
	}
	return parsed
}

func ReservedSpaceForUserSubdomain(maxLen int) int {
	if maxLen != defaultMaxDomainLength {
		return 0
	}
	return 8
}

func PublicDNS(subdomain, dns, publicIP string) string {
	maxLen := MaxDomainLength()
	reserved := ReservedSpaceForUserSubdomain(maxLen)
	remaining := maxLen - reserved - len(dns) - len(publicIP) - len("ip") - 3
	if remaining < 0 {
		remaining = 0
	}
	if len(subdomain) > remaining {
		subdomain = subdomain[:remaining]
	}
	ipComponent := strings.ReplaceAll(publicIP, ".", "-")
	parts := []string{}
	if subdomain != "" {
		parts = append(parts, subdomain)
	}
	parts = append(parts, "ip", ipComponent)
	prefix := strings.Join(parts, "-")
	prefix = multiHyphen.ReplaceAllString(prefix, "-")
	if dns == "" {
		return prefix
	}
	return prefix + "." + dns
}
