package pullpreview

import (
	"fmt"
	"strings"
)

func BuildPreScript(registries []string, preScript string, logger *Logger) string {
	lines := []string{"#!/bin/bash -e"}
	for _, registry := range ParseRegistryCredentials(registries, logger) {
		lines = append(lines,
			fmt.Sprintf("echo \"Logging into %s...\"", registry.Host),
			fmt.Sprintf("echo \"%s\" | docker login \"%s\" -u \"%s\" --password-stdin", registry.Password, registry.Host, registry.Username),
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
