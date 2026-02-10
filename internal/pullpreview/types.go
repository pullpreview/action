package pullpreview

import (
	"context"
	"time"
)

type Provider interface {
	Launch(name string, opts LaunchOptions) (AccessDetails, error)
	Terminate(name string) error
	Running(name string) (bool, error)
	ListInstances(tags map[string]string) ([]InstanceSummary, error)
	Username() string
}

type AccessDetails struct {
	Username   string
	IPAddress  string
	CertKey    string
	PrivateKey string
}

type LaunchOptions struct {
	Size     string
	UserData string
	Ports    []string
	CIDRs    []string
	Tags     map[string]string
}

type InstanceSummary struct {
	Name      string
	PublicIP  string
	Size      string
	Region    string
	Zone      string
	CreatedAt time.Time
	Tags      map[string]string
}

type CommonOptions struct {
	Admins          []string
	AdminPublicKeys []string
	Context         context.Context
	CIDRs           []string
	Registries      []string
	ProxyTLS        string
	DNS             string
	Ports           []string
	InstanceType    string
	DefaultPort     string
	Tags            map[string]string
	ComposeFiles    []string
	ComposeOptions  []string
	PreScript       string
}

type UpOptions struct {
	AppPath   string
	Name      string
	Subdomain string
	Common    CommonOptions
}

type DownOptions struct {
	Name string
}

type ListOptions struct {
	Org  string
	Repo string
}

type GithubSyncOptions struct {
	AppPath           string
	Label             string
	AlwaysOn          []string
	DeploymentVariant string
	TTL               string
	Context           context.Context
	Common            CommonOptions
}
