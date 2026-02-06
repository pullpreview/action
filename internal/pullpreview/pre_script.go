package pullpreview

import (
	"fmt"
	"net/url"
	"strings"
)

func BuildPreScript(registries []string, preScript string, logger *Logger) string {
	lines := []string{"#!/bin/bash -e"}
	for i, registry := range registries {
		registry = strings.TrimSpace(registry)
		if registry == "" {
			continue
		}
		uri, err := url.Parse(registry)
		if err != nil || uri.Host == "" || uri.Scheme != "docker" {
			if logger != nil {
				if err == nil {
					err = fmt.Errorf("invalid registry")
				}
				logger.Warnf("Registry #%d is invalid: %v", i, err)
			}
			continue
		}
		username := uri.User.Username()
		password, hasPassword := uri.User.Password()
		if !hasPassword {
			password = username
			username = "doesnotmatter"
		}
		lines = append(lines,
			fmt.Sprintf("echo \"Logging into %s...\"", uri.Host),
			fmt.Sprintf("echo \"%s\" | docker login \"%s\" -u \"%s\" --password-stdin", password, uri.Host, username),
		)
	}
	if strings.TrimSpace(preScript) != "" {
		lines = append(lines,
			fmt.Sprintf("echo 'Attempting to run pre-script at %s...'", preScript),
			fmt.Sprintf("bash -e %s", preScript),
		)
	}
	return strings.Join(lines, "\n") + "\n"
}
