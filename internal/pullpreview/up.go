package pullpreview

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func RunUp(opts UpOptions, provider Provider, logger *Logger) (*Instance, error) {
	if logger != nil {
		logger.Debugf("options=%+v", opts)
	}
	instance := NewInstance(opts.Name, opts.Common, provider, logger)
	if opts.Subdomain != "" {
		instance.WithSubdomain(opts.Subdomain)
	}
	if err := instance.ValidateDeploymentConfig(); err != nil {
		return nil, err
	}

	appPath := opts.AppPath
	clonePath, cloneCleanup, err := instance.CloneIfURL(appPath)
	if err != nil {
		return nil, err
	}
	defer cloneCleanup()
	appPath = clonePath

	if logger != nil {
		logger.Infof("Preparing deployment sources from %s", appPath)
	}

	if err := instance.LaunchAndWait(); err != nil {
		return nil, err
	}

	if logger != nil {
		logger.Infof("Synchronizing instance name=%s", instance.Name)
	}

	if err := instance.SetupScripts(); err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Infof("SSH keys synced on instance")
	}

	instructions := fmt.Sprintf("\nTo connect to the instance (authorized GitHub users: %s):\n  ssh %s\n", join(instance.Admins, ", "), instance.SSHAddress())
	stop := make(chan struct{})
	emitDeploymentHeartbeat(instance, logger)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				emitDeploymentHeartbeat(instance, logger)
			case <-stop:
				return
			}
		}
	}()

	if err := instance.DeployApp(appPath); err != nil {
		close(stop)
		return nil, err
	}
	close(stop)

	writeGithubOutputs(instance)

	fmt.Println("\nYou can access your application at the following URL:")
	fmt.Printf("  %s\n\n", instance.URL())
	fmt.Println(instructions)
	fmt.Println("Then to view the logs:")
	if instance.DeploymentTarget == DeploymentTargetHelm {
		fmt.Printf("  ssh %s sudo KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl logs -n %s deploy/pullpreview-caddy -f\n", instance.SSHAddress(), instance.HelmNamespace())
	} else {
		fmt.Println("  docker-compose logs --tail 1000 -f")
	}
	fmt.Println()

	return instance, nil
}

func join(values []string, sep string) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += sep
		}
		out += v
	}
	return out
}

func emitDeploymentHeartbeat(instance *Instance, logger *Logger) {
	admins := join(instance.Admins, ", ")
	if strings.TrimSpace(admins) == "" {
		admins = "none"
	}
	line := fmt.Sprintf(
		"Heartbeat: preview_url=%s ssh=\"ssh %s\" authorized_users=\"%s\" (keys uploaded on server)",
		instance.URL(),
		instance.SSHAddress(),
		admins,
	)
	if logger != nil {
		logger.Infof("%s", line)
		return
	}
	fmt.Println(line)
}

func writeGithubOutputs(instance *Instance) {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "url=%s\n", instance.URL())
	fmt.Fprintf(f, "host=%s\n", instance.PublicIP())
	fmt.Fprintf(f, "username=%s\n", instance.Username())
}
