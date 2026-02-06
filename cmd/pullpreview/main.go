package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pullpreview/action/internal/providers/lightsail"
	"github.com/pullpreview/action/internal/pullpreview"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	logger := pullpreview.NewLogger(pullpreview.ParseLogLevel(os.Getenv("PULLPREVIEW_LOGGER_LEVEL")))

	switch cmd {
	case "up":
		runUp(args, logger)
	case "down":
		runDown(args, logger)
	case "github-sync":
		runGithubSync(args, logger)
	case "list":
		runList(args, logger)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Usage: pullpreview [up|down|list|github-sync] [options]")
}

func runUp(args []string, logger *pullpreview.Logger) {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Enable verbose mode")
	name := fs.String("name", "", "Unique name for the environment (optional for local use)")
	commonFlags := registerCommonFlags(fs)
	leadingPath, parseArgs := splitLeadingPositional(args)
	fs.Parse(parseArgs)
	if *verbose {
		logger.SetLevel(pullpreview.LevelDebug)
	}
	appPath := strings.TrimSpace(leadingPath)
	if appPath == "" && fs.NArg() > 0 {
		appPath = fs.Arg(0)
	}
	if appPath == "" {
		fmt.Println("Usage: pullpreview up path/to/app [--name <name>]")
		os.Exit(1)
	}
	if strings.TrimSpace(*name) == "" {
		*name = defaultUpName(appPath)
	}
	provider := mustProvider(logger)
	_, err := pullpreview.RunUp(pullpreview.UpOptions{AppPath: appPath, Name: *name, Common: commonFlags.ToOptions()}, provider, logger)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func defaultUpName(appPath string) string {
	base := appPath
	if parsed, err := url.Parse(appPath); err == nil && parsed.Scheme != "" {
		base = parsed.Path
	}
	base = filepath.Base(strings.TrimSpace(base))
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "app"
	}
	return pullpreview.NormalizeName("local-" + base)
}

func runDown(args []string, logger *pullpreview.Logger) {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Enable verbose mode")
	name := fs.String("name", "", "Name of the environment to destroy")
	fs.Parse(args)
	if *verbose {
		logger.SetLevel(pullpreview.LevelDebug)
	}
	if *name == "" {
		fmt.Println("Usage: pullpreview down --name <name>")
		os.Exit(1)
	}
	provider := mustProvider(logger)
	if err := pullpreview.RunDown(pullpreview.DownOptions{Name: *name}, provider, logger); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func runGithubSync(args []string, logger *pullpreview.Logger) {
	fs := flag.NewFlagSet("github-sync", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Enable verbose mode")
	label := fs.String("label", "pullpreview", "Label to use for triggering preview deployments")
	deploymentVariant := fs.String("deployment-variant", "", "Deployment variant (4 chars max)")
	alwaysOn := fs.String("always-on", "", "List of branches to always deploy")
	ttl := fs.String("ttl", "infinite", "Maximum time to live for deployments (e.g. 10h, 5d, infinite)")
	commentPR := fs.Bool("comment-pr", true, "Whether to post/update pull request comments while deployment is building/ready/error")
	commonFlags := registerCommonFlags(fs)
	leadingPath, parseArgs := splitLeadingPositional(args)
	fs.Parse(parseArgs)
	if *verbose {
		logger.SetLevel(pullpreview.LevelDebug)
	}
	appPath := strings.TrimSpace(leadingPath)
	if appPath == "" && fs.NArg() > 0 {
		appPath = fs.Arg(0)
	}
	if appPath == "" {
		fmt.Println("Usage: pullpreview github-sync path/to/app [options]")
		os.Exit(1)
	}
	provider := mustProvider(logger)
	opts := pullpreview.GithubSyncOptions{
		AppPath:           appPath,
		Label:             *label,
		AlwaysOn:          splitCommaList(*alwaysOn),
		DeploymentVariant: *deploymentVariant,
		TTL:               *ttl,
		CommentPR:         *commentPR,
		Common:            commonFlags.ToOptions(),
	}
	if err := pullpreview.RunGithubSync(opts, provider, logger); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func runList(args []string, logger *pullpreview.Logger) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Enable verbose mode")
	org := fs.String("org", "", "Restrict to given organization name")
	repo := fs.String("repo", "", "Restrict to given repository name")
	leadingTarget, parseArgs := splitLeadingPositional(args)
	fs.Parse(parseArgs)
	if *verbose {
		logger.SetLevel(pullpreview.LevelDebug)
	}
	target := strings.TrimSpace(leadingTarget)
	if target == "" && fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	if target != "" {
		parts := strings.SplitN(target, "/", 2)
		if len(parts) > 0 {
			*org = parts[0]
		}
		if len(parts) == 2 {
			*repo = parts[1]
		}
	}
	provider := mustProvider(logger)
	if err := pullpreview.RunList(pullpreview.ListOptions{Org: *org, Repo: *repo}, provider, logger); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

type commonFlagValues struct {
	admins         string
	cidrs          string
	registries     string
	ports          string
	composeFiles   string
	composeOptions string
	tags           multiValue
	options        pullpreview.CommonOptions
}

func registerCommonFlags(fs *flag.FlagSet) *commonFlagValues {
	values := &commonFlagValues{}
	fs.StringVar(&values.admins, "admins", "", "Logins of GitHub users that will have their SSH key installed on the instance")
	fs.StringVar(&values.cidrs, "cidrs", "0.0.0.0/0", "CIDRs allowed to connect to the instance")
	fs.StringVar(&values.registries, "registries", "", "URIs of docker registries to authenticate against")
	fs.StringVar(&values.options.ProxyTLS, "proxy-tls", "", "Enable automatic HTTPS proxying with Let's Encrypt (format: service:port, e.g. web:80)")
	fs.StringVar(&values.options.DNS, "dns", "my.preview.run", "DNS suffix to use")
	fs.StringVar(&values.ports, "ports", "80/tcp,443/tcp", "Ports to open for external access")
	fs.StringVar(&values.options.InstanceType, "instance-type", "small", "Instance type to use")
	fs.StringVar(&values.options.DefaultPort, "default-port", "80", "Default port for URL")
	fs.Var(&values.tags, "tags", "Tags to add to the instance (key:value), comma-separated")
	fs.StringVar(&values.composeFiles, "compose-files", "docker-compose.yml", "Compose files to use")
	fs.StringVar(&values.composeOptions, "compose-options", "--build", "Additional options to pass to docker-compose up")
	fs.StringVar(&values.options.PreScript, "pre-script", "", "Path to a bash script to run on the instance before docker compose")
	return values
}

func (c *commonFlagValues) ToOptions() pullpreview.CommonOptions {
	opts := c.options
	opts.Admins = splitCommaList(c.admins)
	opts.CIDRs = splitCommaList(c.cidrs)
	opts.Registries = splitCommaList(c.registries)
	opts.Ports = splitCommaList(c.ports)
	opts.ComposeFiles = splitCommaList(c.composeFiles)
	opts.ComposeOptions = splitCommaList(c.composeOptions)
	opts.Tags = parseTags(c.tags)
	return opts
}

type multiValue []string

func (m *multiValue) String() string {
	return strings.Join(*m, ",")
}

func (m *multiValue) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func splitCommaList(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseTags(values []string) map[string]string {
	result := map[string]string{}
	for _, raw := range values {
		for _, part := range splitCommaList(raw) {
			pair := strings.SplitN(part, ":", 2)
			if len(pair) == 2 {
				result[strings.TrimSpace(pair[0])] = strings.TrimSpace(pair[1])
			}
		}
	}
	return result
}

func splitLeadingPositional(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	first := strings.TrimSpace(args[0])
	if first == "" || strings.HasPrefix(first, "-") {
		return "", args
	}
	return first, args[1:]
}

func mustProvider(logger *pullpreview.Logger) pullpreview.Provider {
	providerName := strings.TrimSpace(os.Getenv("PULLPREVIEW_PROVIDER"))
	if providerName == "" || providerName == "lightsail" {
		provider, err := lightsail.New(context.Background(), os.Getenv("AWS_REGION"), logger)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		return provider
	}
	fmt.Printf("Unsupported provider: %s\n", providerName)
	os.Exit(1)
	return nil
}
