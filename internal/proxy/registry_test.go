package proxy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type stubDriver struct {
	name          string
	containerName string
	caps          Capabilities
}

func (s *stubDriver) Name() string               { return s.name }
func (s *stubDriver) ContainerName() string      { return s.containerName }
func (s *stubDriver) Capabilities() Capabilities { return s.caps }
func (s *stubDriver) StackService(StackOptions) (map[string]any, map[string]any, error) {
	return nil, nil, nil
}
func (s *stubDriver) AppLabels([]Site, UpstreamRef) map[string]string   { return nil }
func (s *stubDriver) Render(*State) (Files, error)                      { return nil, nil }
func (s *stubDriver) Validate(context.Context, string) error            { return nil }
func (s *stubDriver) Apply(context.Context, Files) (ApplyResult, error) { return ApplyResult{}, nil }
func (s *stubDriver) Status(context.Context) (Status, error)            { return Status{}, nil }
func (s *stubDriver) Logs(context.Context, int) ([]string, error)       { return nil, nil }
func (s *stubDriver) Restart(context.Context) error                     { return nil }

func TestRegistryLifecycleAndValidation(t *testing.T) {
	reg := NewRegistry()

	if err := reg.Register(nil); !errors.Is(err, ErrInvalidDriver) {
		t.Fatalf("Register(nil) error = %v, want ErrInvalidDriver", err)
	}
	if err := reg.Register(&stubDriver{name: "   "}); !errors.Is(err, ErrInvalidDriver) {
		t.Fatalf("Register(empty) error = %v, want ErrInvalidDriver", err)
	}
	if _, err := reg.Get("missing"); !errors.Is(err, ErrDriverNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrDriverNotFound", err)
	}

	if err := reg.Register(&stubDriver{name: "Ferron"}); err != nil {
		t.Fatalf("Register ferron: %v", err)
	}
	if err := reg.Register(&stubDriver{name: "angie"}); err != nil {
		t.Fatalf("Register angie: %v", err)
	}
	if err := reg.Register(&stubDriver{name: "caddy"}); err != nil {
		t.Fatalf("Register caddy: %v", err)
	}

	got, err := reg.Get("  FERRON ")
	if err != nil {
		t.Fatalf("Get(FERRON): %v", err)
	}
	if got.Name() != "Ferron" {
		t.Fatalf("got.Name() = %q, want Ferron", got.Name())
	}

	// Verify List() returns drivers sorted alphabetically by normalized registry key.
	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("len(List) = %d, want 3", len(list))
	}
	wantNames := []string{"angie", "caddy", "Ferron"}
	for i, want := range wantNames {
		if list[i].Name() != want {
			t.Fatalf("List[%d].Name() = %q, want %q", i, list[i].Name(), want)
		}
	}

	// Re-registering replaces existing driver cleanly.
	if err := reg.Register(&stubDriver{name: "caddy", containerName: "caddy-v2"}); err != nil {
		t.Fatalf("re-register caddy: %v", err)
	}
	replaced, err := reg.Get("caddy")
	if err != nil || replaced.ContainerName() != "caddy-v2" {
		t.Fatalf("expected replaced driver, got %+v err=%v", replaced, err)
	}
}

func TestRegistryConcurrency(t *testing.T) {
	reg := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("drv-%d", id%4)
			_ = reg.Register(&stubDriver{name: name})
			_, _ = reg.Get(name)
			_ = reg.List()
		}(i)
	}
	wg.Wait()
	if len(reg.List()) != 4 {
		t.Fatalf("expected 4 drivers after concurrent registration, got %d", len(reg.List()))
	}
}

func TestGlobalRegistryHelpers(t *testing.T) {
	testName := "test-global-driver-proxy-unit"
	t.Cleanup(func() { defaultRegistry.Unregister(testName) })

	if err := Register(&stubDriver{name: testName}); err != nil {
		t.Fatalf("global Register: %v", err)
	}
	d, err := Get(testName)
	if err != nil {
		t.Fatalf("global Get: %v", err)
	}
	if d.Name() != testName {
		t.Fatalf("global Get Name() = %q, want %q", d.Name(), testName)
	}
	found := false
	for _, item := range List() {
		if item.Name() == testName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("global List did not include %q", testName)
	}
}
