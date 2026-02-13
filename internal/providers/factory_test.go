package providers

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pullpreview/action/internal/pullpreview"
)

type testProviderConfig struct{}

func (testProviderConfig) ProviderName() string {
	return "test"
}

func (testProviderConfig) ProviderDisplayName() string {
	return "Test"
}

func (testProviderConfig) Validate() error {
	return nil
}

type invalidConfigError struct {
	err error
}

func (invalidConfigError) ProviderName() string {
	return "invalid"
}

func (invalidConfigError) ProviderDisplayName() string {
	return "Invalid"
}

func (c invalidConfigError) Validate() error {
	return c.err
}

type testProvider struct{}

func (testProvider) Launch(name string, opts pullpreview.LaunchOptions) (pullpreview.AccessDetails, error) {
	return pullpreview.AccessDetails{}, nil
}
func (testProvider) Terminate(name string) error       { return nil }
func (testProvider) Running(name string) (bool, error) { return true, nil }
func (testProvider) ListInstances(tags map[string]string) ([]pullpreview.InstanceSummary, error) {
	return nil, nil
}
func (testProvider) Username() string { return "test" }

var providerNameCounter uint64

func uniqueProviderName() string {
	id := atomic.AddUint64(&providerNameCounter, 1)
	return fmt.Sprintf("test-provider-%d", id)
}

func TestRegisterProviderValidation(t *testing.T) {
	if err := RegisterProvider("", "display", func(map[string]string) (pullpreview.ProviderConfig, error) {
		return testProviderConfig{}, nil
	}, func(context.Context, pullpreview.ProviderConfig, *pullpreview.Logger) (pullpreview.Provider, error) {
		return nil, nil
	}); err == nil {
		t.Fatalf("expected error for empty provider name")
	}

	if err := RegisterProvider(uniqueProviderName(), "display", nil, func(context.Context, pullpreview.ProviderConfig, *pullpreview.Logger) (pullpreview.Provider, error) {
		return nil, nil
	}); err == nil {
		t.Fatalf("expected error for nil parser")
	}

	if err := RegisterProvider(uniqueProviderName(), "display", func(map[string]string) (pullpreview.ProviderConfig, error) {
		return testProviderConfig{}, nil
	}, nil); err == nil {
		t.Fatalf("expected error for nil factory")
	}

	name := uniqueProviderName()
	if err := RegisterProvider(name, "display", func(map[string]string) (pullpreview.ProviderConfig, error) {
		return testProviderConfig{}, nil
	}, func(context.Context, pullpreview.ProviderConfig, *pullpreview.Logger) (pullpreview.Provider, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("first registration should succeed: %v", err)
	}
	if err := RegisterProvider(name, "display", func(map[string]string) (pullpreview.ProviderConfig, error) {
		return testProviderConfig{}, nil
	}, func(context.Context, pullpreview.ProviderConfig, *pullpreview.Logger) (pullpreview.Provider, error) {
		return nil, nil
	}); err == nil {
		t.Fatalf("expected duplicate registration error")
	}
}

func TestNewProviderDefaultsToDefaultProviderName(t *testing.T) {
	if err := RegisterProvider(DefaultProviderName, "Default provider", func(map[string]string) (pullpreview.ProviderConfig, error) {
		return testProviderConfig{}, nil
	}, func(context.Context, pullpreview.ProviderConfig, *pullpreview.Logger) (pullpreview.Provider, error) {
		return testProvider{}, nil
	}); err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("failed to register default test provider: %v", err)
	}

	provider, actualName, err := NewProvider(context.Background(), "", map[string]string{}, nil)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if actualName != DefaultProviderName {
		t.Fatalf("expected provider name %q, got %q", DefaultProviderName, actualName)
	}
	if provider == nil {
		t.Fatalf("expected provider instance")
	}
}

func TestNewProviderUnknownProvider(t *testing.T) {
	_, _, err := NewProvider(context.Background(), "does-not-exist", map[string]string{}, nil)
	if err == nil {
		t.Fatalf("expected unsupported provider error")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewProviderParserAndConfigErrorsWrappedWithDisplayName(t *testing.T) {
	name := uniqueProviderName()
	if err := RegisterProvider(name, "CustomDisplay", func(env map[string]string) (pullpreview.ProviderConfig, error) {
		return nil, fmt.Errorf("parser error")
	}, func(context.Context, pullpreview.ProviderConfig, *pullpreview.Logger) (pullpreview.Provider, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("failed to register parser error provider: %v", err)
	}
	_, _, err := NewProvider(context.Background(), name, map[string]string{}, nil)
	if err == nil || !strings.Contains(err.Error(), "CustomDisplay") || !strings.Contains(err.Error(), "parser error") {
		t.Fatalf("expected parser error with display name, got %v", err)
	}

	name = uniqueProviderName()
	if err := RegisterProvider(name, "ConfigDisplay", func(env map[string]string) (pullpreview.ProviderConfig, error) {
		return invalidConfigError{err: fmt.Errorf("invalid config")}, nil
	}, func(context.Context, pullpreview.ProviderConfig, *pullpreview.Logger) (pullpreview.Provider, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("failed to register config error provider: %v", err)
	}
	_, _, err = NewProvider(context.Background(), name, map[string]string{}, nil)
	if err == nil || !strings.Contains(err.Error(), "ConfigDisplay") || !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected config error with display name, got %v", err)
	}
}
