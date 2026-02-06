package pullpreview

import (
	"fmt"
	"net/url"
	"strings"
)

type RegistryCredential struct {
	Host     string
	Username string
	Password string
}

func ParseRegistryCredentials(registries []string, logger *Logger) []RegistryCredential {
	parsed := []RegistryCredential{}
	for i, raw := range registries {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		cred, err := parseRegistryCredential(value)
		if err != nil {
			if logger != nil {
				logger.Warnf("Registry #%d is invalid: %v", i, err)
			}
			continue
		}
		parsed = append(parsed, cred)
	}
	return parsed
}

func parseRegistryCredential(value string) (RegistryCredential, error) {
	uri, err := url.Parse(value)
	if err != nil {
		return RegistryCredential{}, err
	}
	if uri.Scheme != "docker" || strings.TrimSpace(uri.Host) == "" {
		return RegistryCredential{}, fmt.Errorf("invalid registry")
	}
	username := uri.User.Username()
	password, hasPassword := uri.User.Password()
	if !hasPassword {
		password = username
		username = "doesnotmatter"
	}
	if strings.TrimSpace(password) == "" {
		return RegistryCredential{}, fmt.Errorf("missing registry password/token")
	}
	return RegistryCredential{
		Host:     uri.Host,
		Username: username,
		Password: password,
	}, nil
}
