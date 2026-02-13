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

type ProviderMetadata interface {
	Name() string
	DisplayName() string
}

type SupportsSnapshots interface {
	SupportsSnapshots() bool
}

type SupportsRestore interface {
	SupportsRestore() bool
}

type SupportsFirewall interface {
	SupportsFirewall() bool
}

type UserDataProvider interface {
	BuildUserData(options UserDataOptions) (string, error)
}

type AccessDetails struct {
	Username   string
	IPAddress  string
	CertKey    string
	PrivateKey string
}

type UserDataOptions struct {
	AppPath       string
	SSHPublicKeys []string
	Username      string
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
	Region          string
	Image           string
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
	Preflight       bool
	EnableLock      bool
}

type DownOptions struct {
	Name string
	Tags map[string]string
}

type ProviderConfig interface {
	ProviderName() string
	ProviderDisplayName() string
	Validate() error
}

type UpOptions struct {
	AppPath   string
	Name      string
	Subdomain string
	Common    CommonOptions
}

type ListOptions struct {
	Org  string
	Repo string
}

type GithubSyncOptions struct {
	AppPath           string
	Label             string
	DeploymentVariant string
	TTL               string
	Context           context.Context
	Common            CommonOptions
}
