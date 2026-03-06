package tui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dc/codereview/internal/diff"
	"github.com/dc/codereview/internal/review"
)

type fileListMode int

const (
	fileListModeModified fileListMode = iota
	fileListModeTree
)

type treeNode struct {
	name     string
	absPath  string
	isDir    bool
	loaded   bool
	expanded bool
	children []*treeNode
}

type treeRow struct {
	node  *treeNode
	depth int
}

type fileList struct {
	files         []diff.FileDiff
	selected      int
	height        int
	offset        int // scroll offset
	review        *review.Review
	mode          fileListMode
	repoRoot      string
	modifiedIndex map[string]int
	root          *treeNode
	treeRows      []treeRow
	treeSelected  int
	treeOffset    int
}

func newFileList(files []diff.FileDiff) fileList {
	root, _ := os.Getwd()
	if root == "" {
		root = "."
	}
	fl := fileList{
		files:         files,
		mode:          fileListModeModified,
		repoRoot:      root,
		modifiedIndex: make(map[string]int, len(files)),
		root: &treeNode{
			name:     filepath.Base(root),
			absPath:  root,
			isDir:    true,
			expanded: true,
		},
	}
	for i := range files {
		fl.modifiedIndex[fl.filePathForIndex(i)] = i
	}
	return fl
}

func (fl *fileList) isTreeMode() bool {
	return fl.mode == fileListModeTree
}

func (fl *fileList) filePathForIndex(i int) string {
	if i < 0 || i >= len(fl.files) {
		return ""
	}
	name := fl.files[i].NewName
	if name == "/dev/null" {
		name = fl.files[i].OldName
	}
	return filepath.ToSlash(name)
}

func (fl *fileList) selectedDiffPath() string {
	return fl.filePathForIndex(fl.selected)
}

func (fl *fileList) modifiedFileAtPath(path string) (*diff.FileDiff, int, bool) {
	idx, ok := fl.modifiedIndex[filepath.ToSlash(path)]
	if !ok {
		return nil, -1, false
	}
	if idx < 0 || idx >= len(fl.files) {
		return nil, -1, false
	}
	return &fl.files[idx], idx, true
}

func (fl *fileList) toggleMode() error {
	if fl.mode == fileListModeModified {
		fl.mode = fileListModeTree
		if err := fl.ensureNodeLoaded(fl.root); err != nil {
			return err
		}
		fl.rebuildTreeRows()
		fl.selectTreePath(fl.selectedDiffPath())
		fl.ensureTreeVisible()
		return nil
	}
	fl.mode = fileListModeModified
	fl.ensureVisible()
	return nil
}

func (fl *fileList) ensureNodeLoaded(node *treeNode) error {
	if node == nil || !node.isDir || node.loaded {
		return nil
	}
	entries, err := os.ReadDir(node.absPath)
	if err != nil {
		return err
	}

	children := make([]*treeNode, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if name == ".git" {
			continue
		}
		child := &treeNode{
			name:    name,
			absPath: filepath.Join(node.absPath, name),
			isDir:   e.IsDir(),
		}
		children = append(children, child)
	}

	sort.Slice(children, func(i, j int) bool {
		if children[i].isDir != children[j].isDir {
			return children[i].isDir
		}
		return strings.ToLower(children[i].name) < strings.ToLower(children[j].name)
	})

	node.children = children
	node.loaded = true
	return nil
}

func (fl *fileList) rebuildTreeRows() {
	fl.treeRows = nil
	if fl.root == nil {
		return
	}
	var walk func(node *treeNode, depth int)
	walk = func(node *treeNode, depth int) {
		for _, child := range node.children {
			fl.treeRows = append(fl.treeRows, treeRow{node: child, depth: depth})
			if child.isDir && child.expanded && child.loaded {
				walk(child, depth+1)
			}
		}
	}
	walk(fl.root, 0)
	if fl.treeSelected >= len(fl.treeRows) {
		fl.treeSelected = len(fl.treeRows) - 1
	}
	if fl.treeSelected < 0 {
		fl.treeSelected = 0
	}
}

func (fl *fileList) selectTreePath(path string) {
	path = filepath.ToSlash(path)
	for i := range fl.treeRows {
		if fl.treePath(fl.treeRows[i].node) == path {
			fl.treeSelected = i
			fl.syncModifiedSelectionFromTree()
			return
		}
	}
}

func (fl *fileList) treePath(node *treeNode) string {
	if node == nil {
		return ""
	}
	rel, err := filepath.Rel(fl.repoRoot, node.absPath)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (fl *fileList) selectedTreeRow() (treeRow, bool) {
	if fl.treeSelected < 0 || fl.treeSelected >= len(fl.treeRows) {
		return treeRow{}, false
	}
	return fl.treeRows[fl.treeSelected], true
}

func (fl *fileList) selectedTreePath() (string, bool, bool) {
	row, ok := fl.selectedTreeRow()
	if !ok {
		return "", false, false
	}
	return fl.treePath(row.node), row.node.isDir, true
}

func (fl *fileList) syncModifiedSelectionFromTree() {
	path, isDir, ok := fl.selectedTreePath()
	if !ok || isDir {
		return
	}
	if idx, ok := fl.modifiedIndex[path]; ok {
		fl.selected = idx
	}
}

func (fl *fileList) toggleTreeExpand() error {
	row, ok := fl.selectedTreeRow()
	if !ok || !row.node.isDir {
		return nil
	}
	if !row.node.expanded {
		if err := fl.ensureNodeLoaded(row.node); err != nil {
			return err
		}
	}
	row.node.expanded = !row.node.expanded
	fl.rebuildTreeRows()
	fl.ensureTreeVisible()
	return nil
}

func (fl *fileList) selectionStateKey() string {
	if !fl.isTreeMode() {
		return "diff:" + fl.selectedDiffPath()
	}
	path, _, ok := fl.selectedTreePath()
	if !ok {
		return "tree:"
	}
	return "tree:" + path
}

func (fl *fileList) modifiedSelection() (path string, idx int, ok bool) {
	if fl.isTreeMode() {
		p, isDir, exists := fl.selectedTreePath()
		if !exists || isDir {
			return "", -1, false
		}
		i, exists := fl.modifiedIndex[p]
		if !exists {
			return "", -1, false
		}
		return p, i, true
	}
	if len(fl.files) == 0 {
		return "", -1, false
	}
	return fl.selectedDiffPath(), fl.selected, true
}

func (fl *fileList) refSelection() (path string, isDir bool, ok bool) {
	if !fl.isTreeMode() {
		return "", false, false
	}
	return fl.selectedTreePath()
}

func (fl *fileList) counts() (int, int) {
	if fl.isTreeMode() {
		if len(fl.treeRows) == 0 {
			return 0, 0
		}
		return fl.treeSelected + 1, len(fl.treeRows)
	}
	if len(fl.files) == 0 {
		return 0, 0
	}
	return fl.selected + 1, len(fl.files)
}

func (fl *fileList) search(term string) (bool, error) {
	needle := strings.ToLower(strings.TrimSpace(term))
	if needle == "" {
		return false, nil
	}
	if !fl.isTreeMode() {
		return fl.searchModifiedList(needle), nil
	}

	if fl.searchTreeRows(needle) {
		return true, nil
	}

	var matched string
	bestScore := -1
	walkErr := filepath.WalkDir(fl.repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(fl.repoRoot, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		score := fileSearchScore(rel, needle)
		if score < 0 {
			return nil
		}
		if bestScore < 0 || score < bestScore || (score == bestScore && rel < matched) {
			bestScore = score
			matched = rel
		}
		return nil
	})
	if walkErr != nil {
		return false, walkErr
	}
	if matched == "" {
		return false, nil
	}
	if err := fl.revealTreePath(matched); err != nil {
		return false, err
	}
	return true, nil
}

func (fl *fileList) searchModifiedList(needle string) bool {
	if len(fl.files) == 0 {
		return false
	}
	start := (fl.selected + 1) % len(fl.files)
	bestIdx, ok := bestPathMatchFrom(start, len(fl.files), func(i int) string {
		return fl.filePathForIndex(i)
	}, needle)
	if !ok {
		return false
	}
	fl.selected = bestIdx
	fl.ensureVisible()
	return true
}

func (fl *fileList) searchContent(term string) (string, bool, error) {
	needle := strings.ToLower(strings.TrimSpace(term))
	if needle == "" {
		return "", false, nil
	}

	start := fl.selected
	if _, idx, ok := fl.modifiedSelection(); ok {
		start = idx
	}
	if len(fl.files) > 0 {
		for off := 1; off <= len(fl.files); off++ {
			idx := (start + off) % len(fl.files)
			path := fl.filePathForIndex(idx)
			if path == "" {
				continue
			}
			ok, err := fileContains(filepath.Join(fl.repoRoot, filepath.FromSlash(path)), needle)
			if err != nil {
				return "", false, err
			}
			if ok {
				return path, true, nil
			}
		}
	}

	var matched string
	err := filepath.WalkDir(fl.repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(fl.repoRoot, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ok, containsErr := fileContains(path, needle)
		if containsErr != nil {
			return containsErr
		}
		if ok {
			matched = rel
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && err != fs.SkipAll {
		return "", false, err
	}
	if matched == "" {
		return "", false, nil
	}
	return matched, true, nil
}

const maxContentSearchBytes = 2 << 20 // 2MB

func fileContains(path, needle string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() || info.Size() > maxContentSearchBytes {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if bytesContainsZero(data) {
		return false, nil
	}
	return strings.Contains(strings.ToLower(string(data)), needle), nil
}

func bytesContainsZero(b []byte) bool {
	for _, v := range b {
		if v == 0 {
			return true
		}
	}
	return false
}

func (fl *fileList) focusPath(path string) error {
	path = filepath.ToSlash(path)
	if _, idx, ok := fl.modifiedFileAtPath(path); ok {
		fl.selected = idx
		fl.ensureVisible()
		if fl.isTreeMode() {
			if err := fl.revealTreePath(path); err != nil {
				return err
			}
		}
		return nil
	}
	if !fl.isTreeMode() {
		if err := fl.toggleMode(); err != nil {
			return err
		}
	}
	return fl.revealTreePath(path)
}

func (fl *fileList) searchTreeRows(needle string) bool {
	if len(fl.treeRows) == 0 {
		return false
	}
	start := (fl.treeSelected + 1) % len(fl.treeRows)
	bestIdx, ok := bestPathMatchFrom(start, len(fl.treeRows), func(i int) string {
		return fl.treePath(fl.treeRows[i].node)
	}, needle)
	if !ok {
		return false
	}
	fl.treeSelected = bestIdx
	fl.syncModifiedSelectionFromTree()
	fl.ensureTreeVisible()
	return true
}

func fileSearchScore(path, needle string) int {
	pathLower := strings.ToLower(filepath.ToSlash(path))
	baseLower := strings.ToLower(filepath.Base(pathLower))
	switch {
	case baseLower == needle:
		return 0
	case strings.HasPrefix(baseLower, needle):
		return 1
	case strings.Contains(baseLower, needle):
		return 2
	case strings.Contains(pathLower, needle):
		return 3
	default:
		return -1
	}
}

func bestPathMatchFrom(start, count int, pathAt func(i int) string, needle string) (int, bool) {
	bestIdx := -1
	bestScore := -1
	for off := 0; off < count; off++ {
		idx := (start + off) % count
		path := pathAt(idx)
		score := fileSearchScore(path, needle)
		if score < 0 {
			continue
		}
		if bestScore < 0 || score < bestScore || (score == bestScore && off < ((bestIdx-start+count)%count)) {
			bestScore = score
			bestIdx = idx
		}
	}
	return bestIdx, bestIdx >= 0
}

func (fl *fileList) nextModified() bool {
	if len(fl.files) == 0 {
		return false
	}
	if !fl.isTreeMode() {
		return fl.nextModifiedList()
	}
	for i := fl.treeSelected + 1; i < len(fl.treeRows); i++ {
		path := fl.treePath(fl.treeRows[i].node)
		if _, ok := fl.modifiedIndex[path]; ok && !fl.treeRows[i].node.isDir {
			fl.treeSelected = i
			fl.syncModifiedSelectionFromTree()
			fl.ensureTreeVisible()
			return true
		}
	}
	return false
}

func (fl *fileList) prevModified() bool {
	if len(fl.files) == 0 {
		return false
	}
	if !fl.isTreeMode() {
		return fl.prevModifiedList()
	}
	for i := fl.treeSelected - 1; i >= 0; i-- {
		path := fl.treePath(fl.treeRows[i].node)
		if _, ok := fl.modifiedIndex[path]; ok && !fl.treeRows[i].node.isDir {
			fl.treeSelected = i
			fl.syncModifiedSelectionFromTree()
			fl.ensureTreeVisible()
			return true
		}
	}
	return false
}

func (fl *fileList) firstModified() bool {
	if len(fl.files) == 0 {
		return false
	}
	if !fl.isTreeMode() {
		fl.selected = 0
		fl.ensureVisible()
		return true
	}
	for i := 0; i < len(fl.treeRows); i++ {
		path := fl.treePath(fl.treeRows[i].node)
		if _, ok := fl.modifiedIndex[path]; ok && !fl.treeRows[i].node.isDir {
			fl.treeSelected = i
			fl.syncModifiedSelectionFromTree()
			fl.ensureTreeVisible()
			return true
		}
	}
	return false
}

func (fl *fileList) nextModifiedList() bool {
	if fl.selected < len(fl.files)-1 {
		fl.selected++
		fl.ensureVisible()
		return true
	}
	return false
}

func (fl *fileList) prevModifiedList() bool {
	if fl.selected > 0 {
		fl.selected--
		fl.ensureVisible()
		return true
	}
	return false
}

func (fl *fileList) revealTreePath(path string) error {
	path = filepath.Clean(filepath.FromSlash(path))
	if path == "." || path == "" {
		return nil
	}
	parts := strings.Split(path, string(filepath.Separator))
	curr := fl.root
	for _, p := range parts {
		if err := fl.ensureNodeLoaded(curr); err != nil {
			return err
		}
		var next *treeNode
		for _, child := range curr.children {
			if child.name == p {
				next = child
				break
			}
		}
		if next == nil {
			return nil
		}
		if next.isDir {
			next.expanded = true
		}
		curr = next
	}
	fl.rebuildTreeRows()
	fl.selectTreePath(filepath.ToSlash(path))
	fl.ensureTreeVisible()
	return nil
}

func (fl *fileList) next() {
	if fl.isTreeMode() {
		if fl.treeSelected < len(fl.treeRows)-1 {
			fl.treeSelected++
			fl.syncModifiedSelectionFromTree()
			fl.ensureTreeVisible()
		}
		return
	}
	if fl.selected < len(fl.files)-1 {
		fl.selected++
		fl.ensureVisible()
	}
}

func (fl *fileList) prev() {
	if fl.isTreeMode() {
		if fl.treeSelected > 0 {
			fl.treeSelected--
			fl.syncModifiedSelectionFromTree()
			fl.ensureTreeVisible()
		}
		return
	}
	if fl.selected > 0 {
		fl.selected--
		fl.ensureVisible()
	}
}

func (fl *fileList) ensureVisible() {
	if fl.height <= 0 {
		return
	}
	if fl.selected < fl.offset {
		fl.offset = fl.selected
	}
	if fl.selected >= fl.offset+fl.height {
		fl.offset = fl.selected - fl.height + 1
	}
}

func (fl *fileList) ensureTreeVisible() {
	if fl.height <= 0 {
		return
	}
	if fl.treeSelected < fl.treeOffset {
		fl.treeOffset = fl.treeSelected
	}
	if fl.treeSelected >= fl.treeOffset+fl.height {
		fl.treeOffset = fl.treeSelected - fl.height + 1
	}
}

func (fl *fileList) selectedFile() *diff.FileDiff {
	if len(fl.files) == 0 {
		return nil
	}
	return &fl.files[fl.selected]
}

func (fl *fileList) view(width int) string {
	if fl.isTreeMode() {
		return fl.viewTree(width)
	}
	return fl.viewModified(width)
}

func (fl *fileList) viewModified(width int) string {
	if len(fl.files) == 0 {
		return fileListStyle.Width(width).Height(fl.height).Render("No files")
	}

	maxW := width - 2 // content width inside border+padding
	if maxW < 1 {
		maxW = 1
	}

	var lines []string
	end := fl.offset + fl.height
	if end > len(fl.files) {
		end = len(fl.files)
	}

	for i := fl.offset; i < end; i++ {
		f := fl.files[i]
		fname := f.NewName
		if fname == "/dev/null" {
			fname = f.OldName
		}

		// Count changes
		adds, dels := 0, 0
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				switch l.Op {
				case diff.OpInsert:
					adds++
				case diff.OpDelete:
					dels++
				}
			}
		}

		indicator := "M"
		if f.OldName == "/dev/null" {
			indicator = "A"
		} else if f.NewName == "/dev/null" {
			indicator = "D"
		}

		stat := fmt.Sprintf(" +%d -%d", adds, dels)

		commentMarker := ""
		cmLen := 0
		if fl.review != nil {
			if cc := fl.review.CommentsForFile(fname); len(cc) > 0 {
				commentMarker = fmt.Sprintf(" [%d]", len(cc))
				cmLen = len(commentMarker)
			}
		}

		// Truncate name so indicator + name + stat + marker fits in one line
		prefix := indicator + " "
		nameBudget := maxW - len(prefix) - len(stat) - cmLen
		name := shortenPath(fname, nameBudget)
		if len(name) > nameBudget && nameBudget > 0 {
			name = name[:nameBudget]
		}

		label := prefix + name + stat
		if commentMarker != "" {
			label += lipgloss.NewStyle().Foreground(colorYellow).Render(commentMarker)
		}

		if i == fl.selected {
			label = selectedFileStyle.Width(maxW).MaxWidth(maxW).Render(label)
		} else {
			label = normalFileStyle.Width(maxW).MaxWidth(maxW).Render(label)
		}
		lines = append(lines, label)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return fileListStyle.Width(width).Height(fl.height).Render(content)
}

func (fl *fileList) viewTree(width int) string {
	if len(fl.treeRows) == 0 {
		return fileListStyle.Width(width).Height(fl.height).Render("No files")
	}

	maxW := width - 2
	if maxW < 1 {
		maxW = 1
	}

	var lines []string
	end := fl.treeOffset + fl.height
	if end > len(fl.treeRows) {
		end = len(fl.treeRows)
	}

	for i := fl.treeOffset; i < end; i++ {
		row := fl.treeRows[i]
		indent := strings.Repeat("  ", row.depth)
		name := row.node.name
		prefix := "- "
		if row.node.isDir {
			name += "/"
			if row.node.expanded {
				prefix = "v "
			} else {
				prefix = "> "
			}
		}
		rel := fl.treePath(row.node)
		modified := false
		if _, ok := fl.modifiedIndex[rel]; ok && !row.node.isDir {
			modified = true
		}

		// "* " or "  " prefix takes 2 chars
		labelBudget := maxW - 2
		label := indent + prefix + name
		if len(label) > labelBudget && labelBudget > 0 {
			label = label[:labelBudget]
		}

		if modified {
			label = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("* " + label)
		} else {
			label = "  " + label
		}
		if fl.review != nil && modified {
			if cc := fl.review.CommentsForFile(rel); len(cc) > 0 {
				label += lipgloss.NewStyle().Foreground(colorYellow).Render(fmt.Sprintf(" [%d]", len(cc)))
			}
		}

		if i == fl.treeSelected {
			label = selectedFileStyle.Width(maxW).MaxWidth(maxW).Render(label)
		} else {
			label = normalFileStyle.Width(maxW).MaxWidth(maxW).Render(label)
		}
		lines = append(lines, label)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return fileListStyle.Width(width).Height(fl.height).Render(content)
}

// shortenPath shows the full path if it fits, otherwise shows the last
// directory component + filename (e.g., "auth/handler.go").
func shortenPath(p string, maxWidth int) string {
	if len(p) <= maxWidth {
		return p
	}
	base := filepath.Base(p)
	dir := filepath.Dir(p)
	if dir == "." || dir == "/" {
		return base
	}
	short := filepath.Base(dir) + "/" + base
	if len(short) <= maxWidth {
		return short
	}
	return base
}
