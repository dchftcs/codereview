package git

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const DefaultStatusPollInterval = 300 * time.Millisecond
const RemoteStatusPollInterval = 2 * time.Second

type RemoteTarget struct {
	Host string
	Path string
}

type ContainerTarget struct {
	Name string
	Path string
}

type Service struct {
	remote    *RemoteTarget
	container *ContainerTarget
	cacheMu   sync.RWMutex
	fileCache map[string][]byte
	batchRead func([]string) (map[string][]byte, error)
}

func NewLocalService() *Service {
	return &Service{}
}

func NewRemoteService(target RemoteTarget) *Service {
	t := target
	return &Service{remote: &t}
}

func NewContainerService(target ContainerTarget) *Service {
	t := target
	return &Service{container: &t}
}

func ParseRemoteTarget(raw string) (RemoteTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RemoteTarget{}, fmt.Errorf("remote target is empty")
	}
	idx := strings.LastIndex(raw, ":")
	if idx <= 0 || idx == len(raw)-1 {
		return RemoteTarget{}, fmt.Errorf("invalid remote target %q (expected [user@]host:/path)", raw)
	}
	host := strings.TrimSpace(raw[:idx])
	path := strings.TrimSpace(raw[idx+1:])
	if host == "" || path == "" {
		return RemoteTarget{}, fmt.Errorf("invalid remote target %q (expected [user@]host:/path)", raw)
	}
	return RemoteTarget{Host: host, Path: path}, nil
}

func ParseContainerTarget(raw string) (ContainerTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ContainerTarget{}, fmt.Errorf("container target is empty")
	}
	idx := strings.LastIndex(raw, ":")
	if idx <= 0 || idx == len(raw)-1 {
		return ContainerTarget{}, fmt.Errorf("invalid container target %q (expected name:/path)", raw)
	}
	name := strings.TrimSpace(raw[:idx])
	path := strings.TrimSpace(raw[idx+1:])
	if name == "" || path == "" {
		return ContainerTarget{}, fmt.Errorf("invalid container target %q (expected name:/path)", raw)
	}
	return ContainerTarget{Name: name, Path: path}, nil
}

func (s *Service) IsRemote() bool {
	return s != nil && (s.remote != nil || s.container != nil)
}

func (s *Service) RepoRoot() string {
	if s == nil {
		return "."
	}
	if s.remote != nil {
		return s.remote.Path
	}
	if s.container != nil {
		return s.container.Path
	}
	out, err := s.runNoLock("rev-parse", "--show-toplevel")
	if err != nil {
		if wd, wdErr := os.Getwd(); wdErr == nil {
			return wd
		}
		return "."
	}
	return strings.TrimSpace(out)
}

func (s *Service) DisplayRoot() string {
	if s == nil {
		return s.RepoRoot()
	}
	if s.remote != nil {
		return s.remote.Host + ":" + s.remote.Path
	}
	if s.container != nil {
		return "docker:" + s.container.Name + ":" + s.container.Path
	}
	return s.RepoRoot()
}

func (s *Service) HasLocalWorkingTree() bool {
	return s == nil || (s.remote == nil && s.container == nil)
}

func (s *Service) StatusPollInterval() time.Duration {
	if s != nil && s.IsRemote() {
		return RemoteStatusPollInterval
	}
	return DefaultStatusPollInterval
}

func (s *Service) Log(n int) ([]CommitInfo, error) {
	out, err := s.runNoLock("log", "--oneline", fmt.Sprintf("-n%d", n), "--no-decorate")
	if err != nil {
		return nil, err
	}
	var commits []CommitInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		ci := CommitInfo{Hash: parts[0]}
		if len(parts) > 1 {
			ci.Subject = parts[1]
		}
		commits = append(commits, ci)
	}
	return commits, nil
}

func (s *Service) Diff(revSpec string) (string, []CollapsedDir, error) {
	return s.diffInternal(revSpec, false)
}

func (s *Service) DiffFull(revSpec string) (string, []CollapsedDir, error) {
	return s.diffInternal(revSpec, true)
}

func (s *Service) DiffUnstaged() (string, []CollapsedDir, error) {
	return s.diffUnstagedInternal(false)
}

func (s *Service) DiffUnstagedFull() (string, []CollapsedDir, error) {
	return s.diffUnstagedInternal(true)
}

func (s *Service) diffInternal(revSpec string, fullContext bool) (string, []CollapsedDir, error) {
	ctx := []string{}
	if fullContext {
		ctx = []string{"-U99999"}
	}
	if revSpec == "" {
		out, err := s.run(append([]string{"diff"}, append(ctx, "HEAD")...)...)
		if err != nil {
			return "", nil, err
		}
		return s.appendUntrackedDiff(out, fullContext)
	}
	if strings.Contains(revSpec, "...") {
		parts := strings.SplitN(revSpec, "...", 2)
		base, head := parts[0], parts[1]
		mergeBase, err := s.MergeBase(base, head)
		if err != nil {
			return "", nil, fmt.Errorf("finding merge base: %w", err)
		}
		if head == "HEAD" {
			out, err := s.run(append([]string{"diff"}, append(ctx, mergeBase)...)...)
			if err != nil {
				return "", nil, err
			}
			return s.appendUntrackedDiff(out, fullContext)
		}
		out, err := s.run(append([]string{"diff"}, append(ctx, mergeBase, head)...)...)
		return out, nil, err
	}
	if strings.Contains(revSpec, "..") {
		out, err := s.run(append([]string{"diff"}, append(ctx, revSpec)...)...)
		return out, nil, err
	}
	args := append([]string{"show", "--format="}, append(ctx, revSpec)...)
	out, err := s.run(args...)
	return out, nil, err
}

func (s *Service) diffUnstagedInternal(fullContext bool) (string, []CollapsedDir, error) {
	ctx := []string{}
	if fullContext {
		ctx = []string{"-U99999"}
	}
	out, err := s.run(append([]string{"diff"}, ctx...)...)
	if err != nil {
		return "", nil, err
	}
	return s.appendUntrackedDiff(out, fullContext)
}

func (s *Service) Show(commit string) (string, error) {
	return s.run("show", "--format=", commit)
}

func (s *Service) IsRevision(ref string) bool {
	if strings.TrimSpace(ref) == "" {
		return false
	}
	_, err := s.run("rev-parse", "--verify", ref+"^{commit}")
	return err == nil
}

func (s *Service) CurrentBranch() (string, error) {
	out, err := s.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *Service) MergeBase(a, b string) (string, error) {
	out, err := s.run("merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *Service) DefaultBranch() string {
	if out, err := s.run("rev-parse", "--verify", "main"); err == nil && strings.TrimSpace(out) != "" {
		return "main"
	}
	if out, err := s.run("rev-parse", "--verify", "master"); err == nil && strings.TrimSpace(out) != "" {
		return "master"
	}
	return "main"
}

func (s *Service) ListFiles() ([]string, error) {
	out, err := s.runNoLock("ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		files = append(files, l)
	}
	return files, nil
}

func (s *Service) UntrackedFiles() ([]string, error) {
	out, err := s.runNoLock("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		files = append(files, l)
	}
	return files, nil
}

func (s *Service) Status() ([]FileStatus, error) {
	out, err := s.runNoLock("status", "--porcelain=v1")
	if err != nil {
		return nil, err
	}
	return parsePorcelainStatus(out), nil
}

func (s *Service) Stage(path string) error {
	_, err := s.run("add", "--", path)
	return err
}

func (s *Service) Unstage(path string) error {
	_, err := s.run("restore", "--staged", "--", path)
	return err
}

func (s *Service) ReadFile(path string) ([]byte, error) {
	if s == nil || !s.IsRemote() {
		return os.ReadFile(path)
	}
	if content, ok := s.cachedFile(path); ok {
		return content, nil
	}
	if err := s.PrefetchFiles([]string{path}); err == nil {
		if content, ok := s.cachedFile(path); ok {
			return content, nil
		}
	}
	content, err := s.runRemoteBytes("cat -- " + shellQuote(path))
	if err != nil {
		return nil, err
	}
	s.storeCachedFiles(map[string][]byte{path: content})
	return content, nil
}

func (s *Service) PrefetchFiles(paths []string) error {
	if s == nil || !s.IsRemote() {
		return nil
	}
	paths = s.uncachedPaths(paths)
	if len(paths) == 0 {
		return nil
	}
	batchRead := s.batchRead
	if batchRead == nil {
		batchRead = s.readRemoteFilesBatch
	}
	files, err := batchRead(paths)
	if err != nil {
		return err
	}
	s.storeCachedFiles(files)
	return nil
}

func (s *Service) InvalidateFiles(paths []string) {
	if s == nil || !s.IsRemote() {
		return
	}
	paths = normalizeRelativePaths(paths)
	if len(paths) == 0 {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.fileCache == nil {
		return
	}
	for _, path := range paths {
		delete(s.fileCache, path)
	}
}

func (s *Service) ClearFileCache() {
	if s == nil || !s.IsRemote() {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	clear(s.fileCache)
}

func (s *Service) appendUntrackedDiff(base string, fullContext bool) (string, []CollapsedDir, error) {
	files, err := s.UntrackedFiles()
	if err != nil {
		return "", nil, err
	}
	if len(files) == 0 {
		return base, nil, nil
	}

	individual, collapsed := GroupUntrackedFiles(files, maxIndividualUntracked)

	var b strings.Builder
	b.WriteString(base)
	for _, p := range individual {
		args := []string{"diff", "--no-index"}
		if fullContext {
			args = append(args, "-U99999")
		}
		args = append(args, "--", "/dev/null", p)
		out, err := s.runAllowExitCode(args...)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(out) == "" {
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(out)
	}
	return b.String(), collapsed, nil
}

func (s *Service) run(args ...string) (string, error) {
	if s != nil && s.IsRemote() {
		return s.runTargetString("git " + joinShellArgs(args))
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), string(exitErr.Stderr))
		}
		return "", err
	}
	return string(out), nil
}

func (s *Service) runNoLock(args ...string) (string, error) {
	return s.run(append([]string{"--no-optional-locks"}, args...)...)
}

func (s *Service) runAllowExitCode(args ...string) (string, error) {
	if s != nil && s.IsRemote() {
		return s.runTargetAllowExitCode("git " + joinShellArgs(args))
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 1 {
			return string(out), nil
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), string(exitErr.Stderr))
	}
	return "", err
}

func (s *Service) runTargetString(command string) (string, error) {
	out, err := s.runTargetBytes(command)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *Service) runTargetAllowExitCode(command string) (string, error) {
	cmd, err := s.targetCommand(command)
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 1 {
			return string(out), nil
		}
		return "", fmt.Errorf("%s: %s", s.targetDescription(command), string(out))
	}
	return "", err
}

func (s *Service) runRemoteBytes(command string) ([]byte, error) {
	return s.runTargetBytes(command)
}

func (s *Service) readRemoteFilesBatch(paths []string) (map[string][]byte, error) {
	paths = normalizeRelativePaths(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	command := "tar -h -cf - -- " + joinShellArgs(paths)
	archiveBytes, err := s.runTargetBytes(command)
	if err != nil {
		return nil, err
	}
	return readTarEntries(archiveBytes)
}

func (s *Service) runTargetBytes(command string) ([]byte, error) {
	cmd, err := s.targetCommand(command)
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.TrimSpace(stderr) == "" {
				stderr = string(out)
			}
			return nil, fmt.Errorf("%s: %s", s.targetDescription(command), stderr)
		}
		return nil, err
	}
	return out, nil
}

func (s *Service) wrapRemoteCommand(command string) string {
	return "cd " + shellQuote(s.RepoRoot()) + " && " + command
}

func (s *Service) targetCommand(command string) (*exec.Cmd, error) {
	wrapped := s.wrapRemoteCommand(command)
	if s.remote != nil {
		return exec.Command("ssh", s.remote.Host, wrapped), nil
	}
	if s.container != nil {
		return exec.Command("docker", "exec", "-i", s.container.Name, "sh", "-lc", wrapped), nil
	}
	return nil, fmt.Errorf("no remote target configured")
}

func (s *Service) targetDescription(command string) string {
	if s.remote != nil {
		return fmt.Sprintf("ssh %s %s", s.remote.Host, command)
	}
	if s.container != nil {
		return fmt.Sprintf("docker exec %s %s", s.container.Name, command)
	}
	return command
}

func joinShellArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func normalizeRelativePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		for strings.HasPrefix(path, "./") {
			path = strings.TrimPrefix(path, "./")
		}
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		switch path {
		case "", ".", "/dev/null":
			continue
		}
		if strings.HasPrefix(path, "../") {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	slices.Sort(out)
	return out
}

func readTarEntries(archiveBytes []byte) (map[string][]byte, error) {
	files := make(map[string][]byte)
	tr := tar.NewReader(bytes.NewReader(archiveBytes))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if hdr == nil || hdr.Typeflag != tar.TypeReg {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		path := filepath.ToSlash(filepath.Clean(filepath.FromSlash(hdr.Name)))
		files[path] = content
	}
}

func (s *Service) uncachedPaths(paths []string) []string {
	paths = normalizeRelativePaths(paths)
	if len(paths) == 0 {
		return nil
	}
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if s.fileCache == nil {
			out = append(out, path)
			continue
		}
		if _, ok := s.fileCache[path]; !ok {
			out = append(out, path)
		}
	}
	return out
}

func (s *Service) cachedFile(path string) ([]byte, bool) {
	path = firstNormalizedPath(path)
	if path == "" {
		return nil, false
	}
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	if s.fileCache == nil {
		return nil, false
	}
	content, ok := s.fileCache[path]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), content...), true
}

func (s *Service) storeCachedFiles(files map[string][]byte) {
	if len(files) == 0 {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.fileCache == nil {
		s.fileCache = make(map[string][]byte, len(files))
	}
	for path, content := range files {
		path = firstNormalizedPath(path)
		if path == "" {
			continue
		}
		s.fileCache[path] = append([]byte(nil), content...)
	}
}

func firstNormalizedPath(path string) string {
	paths := normalizeRelativePaths([]string{path})
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
