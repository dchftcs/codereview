package git

import "testing"

func TestParseRemoteTarget(t *testing.T) {
	t.Parallel()

	target, err := ParseRemoteTarget("dc@app.example.com:/srv/repo")
	if err != nil {
		t.Fatalf("ParseRemoteTarget returned error: %v", err)
	}
	if target.Host != "dc@app.example.com" {
		t.Fatalf("Host = %q, want %q", target.Host, "dc@app.example.com")
	}
	if target.Path != "/srv/repo" {
		t.Fatalf("Path = %q, want %q", target.Path, "/srv/repo")
	}
}

func TestParseRemoteTargetRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "host", "host:", ":/repo"} {
		if _, err := ParseRemoteTarget(raw); err == nil {
			t.Fatalf("ParseRemoteTarget(%q) error = nil, want non-nil", raw)
		}
	}
}

func TestParseContainerTarget(t *testing.T) {
	t.Parallel()

	target, err := ParseContainerTarget("api:/workspace/repo")
	if err != nil {
		t.Fatalf("ParseContainerTarget returned error: %v", err)
	}
	if target.Name != "api" {
		t.Fatalf("Name = %q, want %q", target.Name, "api")
	}
	if target.Path != "/workspace/repo" {
		t.Fatalf("Path = %q, want %q", target.Path, "/workspace/repo")
	}
}

func TestParseContainerTargetRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "api", "api:", ":/repo"} {
		if _, err := ParseContainerTarget(raw); err == nil {
			t.Fatalf("ParseContainerTarget(%q) error = nil, want non-nil", raw)
		}
	}
}

func TestRemoteServiceUsesSlowerPollInterval(t *testing.T) {
	t.Parallel()

	local := NewLocalService()
	remote := NewRemoteService(RemoteTarget{Host: "app", Path: "/srv/repo"})
	container := NewContainerService(ContainerTarget{Name: "api", Path: "/workspace/repo"})

	if got := local.StatusPollInterval(); got != DefaultStatusPollInterval {
		t.Fatalf("local StatusPollInterval = %v, want %v", got, DefaultStatusPollInterval)
	}
	if got := remote.StatusPollInterval(); got != RemoteStatusPollInterval {
		t.Fatalf("remote StatusPollInterval = %v, want %v", got, RemoteStatusPollInterval)
	}
	if got := container.StatusPollInterval(); got != RemoteStatusPollInterval {
		t.Fatalf("container StatusPollInterval = %v, want %v", got, RemoteStatusPollInterval)
	}
}
