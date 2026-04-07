package git

import (
	"archive/tar"
	"bytes"
	"reflect"
	"testing"
)

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

func TestPrefetchFilesCachesRemoteContent(t *testing.T) {
	t.Parallel()

	var calls [][]string
	svc := &Service{
		remote: &RemoteTarget{Host: "app", Path: "/srv/repo"},
		batchRead: func(paths []string) (map[string][]byte, error) {
			calls = append(calls, append([]string(nil), paths...))
			return map[string][]byte{
				"a.txt": []byte("alpha"),
				"b.txt": []byte("beta"),
			}, nil
		},
	}

	if err := svc.PrefetchFiles([]string{"./b.txt", "a.txt", "a.txt"}); err != nil {
		t.Fatalf("PrefetchFiles returned error: %v", err)
	}
	if err := svc.PrefetchFiles([]string{"a.txt"}); err != nil {
		t.Fatalf("second PrefetchFiles returned error: %v", err)
	}
	if !reflect.DeepEqual(calls, [][]string{{"a.txt", "b.txt"}}) {
		t.Fatalf("batchRead calls = %#v, want %#v", calls, [][]string{{"a.txt", "b.txt"}})
	}

	got, err := svc.ReadFile("a.txt")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(got) != "alpha" {
		t.Fatalf("ReadFile = %q, want %q", string(got), "alpha")
	}
}

func TestInvalidateFilesRemovesCachedEntries(t *testing.T) {
	t.Parallel()

	svc := &Service{
		remote: &RemoteTarget{Host: "app", Path: "/srv/repo"},
		fileCache: map[string][]byte{
			"a.txt": []byte("alpha"),
			"b.txt": []byte("beta"),
		},
	}

	svc.InvalidateFiles([]string{"./a.txt"})

	if _, ok := svc.cachedFile("a.txt"); ok {
		t.Fatal("a.txt remained cached after invalidation")
	}
	if got, ok := svc.cachedFile("b.txt"); !ok || string(got) != "beta" {
		t.Fatalf("b.txt cache = %q, ok=%v, want beta,true", string(got), ok)
	}
}

func TestClearFileCacheRemovesAllCachedEntries(t *testing.T) {
	t.Parallel()

	svc := &Service{
		remote: &RemoteTarget{Host: "app", Path: "/srv/repo"},
		fileCache: map[string][]byte{
			"a.txt": []byte("alpha"),
		},
	}

	svc.ClearFileCache()

	if _, ok := svc.cachedFile("a.txt"); ok {
		t.Fatal("a.txt remained cached after ClearFileCache")
	}
}

func TestReadTarEntries(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files := map[string]string{
		"dir/a.txt": "alpha",
		"b.txt":     "beta",
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("WriteHeader(%q) returned error: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Write(%q) returned error: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	got, err := readTarEntries(buf.Bytes())
	if err != nil {
		t.Fatalf("readTarEntries returned error: %v", err)
	}
	if string(got["dir/a.txt"]) != "alpha" {
		t.Fatalf("dir/a.txt = %q, want %q", string(got["dir/a.txt"]), "alpha")
	}
	if string(got["b.txt"]) != "beta" {
		t.Fatalf("b.txt = %q, want %q", string(got["b.txt"]), "beta")
	}
}
