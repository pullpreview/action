package providers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/pullpreview/action/internal/pullpreview"
)

type ConfigParser func(env map[string]string) (pullpreview.ProviderConfig, error)
type ProviderFactory func(context.Context, pullpreview.ProviderConfig, *pullpreview.Logger) (pullpreview.Provider, error)

type registration struct {
	name        string
	displayName string
	parser      ConfigParser
	factory     ProviderFactory
}

const (
	DefaultProviderName = "lightsail"
)

var (
	mu           sync.RWMutex
	registrations = map[string]registration{}
)

func RegisterProvider(name, displayName string, parser ConfigParser, factory ProviderFactory) error {
	name = normalizeProviderName(name)
	if name == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	if parser == nil {
		return fmt.Errorf("provider %q parser cannot be nil", name)
	}
	if factory == nil {
		return fmt.Errorf("provider %q factory cannot be nil", name)
	}
	if displayName == "" {
		displayName = name
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registrations[name]; exists {
		return fmt.Errorf("provider %q is already registered", name)
	}
	registrations[name] = registration{
		name:        name,
		displayName: displayName,
		parser:      parser,
		factory:     factory,
	}
	return nil
}

func NewProvider(ctx context.Context, providerName string, env map[string]string, logger *pullpreview.Logger) (pullpreview.Provider, string, error) {
	providerName = normalizeProviderName(providerName)
	if providerName == "" {
		providerName = DefaultProviderName
	}
	entry, ok := getRegistration(providerName)
	if !ok {
		return nil, "", fmt.Errorf("unsupported provider %q (supported: %s)", providerName, supportedProviders())
	}
	cfg, err := entry.parser(env)
	if err != nil {
		return nil, "", fmt.Errorf("%s config error: %w", entry.displayName, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, "", fmt.Errorf("%s config validation failed: %w", entry.displayName, err)
	}
	if logger != nil {
		logger.Infof("Using provider %s (%s)", entry.name, entry.displayName)
	}
	provider, err := entry.factory(ctx, cfg, logger)
	return provider, entry.name, err
}

func getRegistration(name string) (registration, bool) {
	mu.RLock()
	defer mu.RUnlock()
	entry, ok := registrations[name]
	return entry, ok
}

func SupportedProviders() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func ProviderDisplayName(name string) string {
	name = normalizeProviderName(name)
	entry, ok := getRegistration(name)
	if !ok {
		return ""
	}
	return entry.displayName
}

func normalizeProviderName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func supportedProviders() string {
	return strings.Join(SupportedProviders(), ", ")
}
