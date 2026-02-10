package pullpreview

import (
	"strings"
	"testing"
)

func TestUserDataScriptIncludesExpectedCommands(t *testing.T) {
	script := UserData{
		AppPath:       "/app",
		Username:      "ec2-user",
		SSHPublicKeys: []string{"ssh-ed25519 AAA", "ssh-rsa BBB"},
	}.Script()

	checks := []string{
		"#!/bin/bash",
		"echo 'ssh-ed25519 AAA\nssh-rsa BBB' > /home/ec2-user/.ssh/authorized_keys",
		"mkdir -p /app && chown -R ec2-user.ec2-user /app",
		"yum install -y docker",
		"touch /etc/pullpreview/ready",
	}
	for _, fragment := range checks {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected script to contain %q, script:\n%s", fragment, script)
		}
	}
}

func TestUserDataScriptWithoutSSHKeys(t *testing.T) {
	script := UserData{
		AppPath:  "/app",
		Username: "ec2-user",
	}.Script()
	if strings.Contains(script, "authorized_keys") {
		t.Fatalf("did not expect authorized_keys setup without keys, script:\n%s", script)
	}
}
