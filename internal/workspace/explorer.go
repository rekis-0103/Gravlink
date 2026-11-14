package workspace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	ErrPathTraversal = errors.New("path escapes workspace root")
	ErrSensitiveFile = errors.New("access to sensitive file denied")
	ErrTooLarge      = errors.New("file exceeds 1 MB viewer limit")
	ErrBinaryFile    = errors.New("binary file cannot be displayed in text viewer")
)

const maxPreviewBytes = 1024 * 1024

// TreeNode represents a file or directory entry inside the workspace.
type TreeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"` // relative forward-slash path
	IsDir    bool       `json:"is_dir"`
	Size     int64      `json:"size,omitempty"`
	Language string     `json:"language,omitempty"`
	Children []TreeNode `json:"children,omitempty"`
}

// FileContent holds read-only file payload for the mobile viewer.
type FileContent struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Language string `json:"language"`
	Size     int64  `json:"size"`
	Content  string `json:"content"`
}

// Explorer provides strictly read-only workspace browsing with path and secret guards.
type Explorer struct {
	rootAbs      string
	ignoredDirs  map[string]bool
	gitIgnoreSet []string
}

// NewExplorer initializes a read-only Explorer anchored at workspaceRoot.
func NewExplorer(workspaceRoot string) (*Explorer, error) {
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil, errors.New("workspace root must be an existing directory")
	}

	exp := &Explorer{
		rootAbs: abs,
		ignoredDirs: map[string]bool{
			".git":         true,
			"node_modules": true,
			".next":        true,
			"dist":         true,
			"build":        true,
			"__pycache__":  true,
			".venv":        true,
			"venv":         true,
		},
	}
	exp.loadSimpleGitignore()
	return exp, nil
}

func (e *Explorer) Root() string {
	return e.rootAbs
}

func (e *Explorer) loadSimpleGitignore() {
	raw, err := os.ReadFile(filepath.Join(e.rootAbs, ".gitignore"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		line = strings.TrimSuffix(line, "/")
		e.gitIgnoreSet = append(e.gitIgnoreSet, line)
	}
}

// ResolveSafePath verifies relPath stays strictly within rootAbs and is not a sensitive file.
func (e *Explorer) ResolveSafePath(relPath string) (string, string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(relPath)))
	if cleaned == "." || cleaned == "" {
		return e.rootAbs, "", nil
	}
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", "", ErrPathTraversal
	}

	candidate := filepath.Join(e.rootAbs, cleaned)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", err
		}
		resolved = filepath.Clean(candidate)
	}

	rel, err := filepath.Rel(e.rootAbs, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", ErrPathTraversal
	}

	if IsSensitiveRelPath(filepath.ToSlash(rel)) {
		return "", "", ErrSensitiveFile
	}
	return resolved, filepath.ToSlash(rel), nil
}

// IsSensitiveRelPath blocks secrets, keys, and credential files by default.
func IsSensitiveRelPath(relSlash string) bool {
	lower := strings.ToLower(relSlash)
	base := strings.ToLower(filepath.Base(relSlash))

	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	if strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx") {
		return true
	}
	if strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") {
		return true
	}
	if strings.Contains(lower, ".git/config") || strings.Contains(lower, ".git/credentials") {
		return true
	}
	if base == "credentials.json" || base == "secrets.json" || base == ".npmrc" || base == ".pypirc" {
		return true
	}
	return false
}

// ListTree builds a bounded directory tree for mobile browsing.
func (e *Explorer) ListTree(relDir string, maxDepth int) ([]TreeNode, error) {
	if maxDepth <= 0 {
		maxDepth = 4
	}
	absDir, _, err := e.ResolveSafePath(relDir)
	if err != nil {
		return nil, err
	}
	return e.readDirRecursive(absDir, 0, maxDepth)
}

func (e *Explorer) readDirRecursive(absDir string, depth, maxDepth int) ([]TreeNode, error) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}

	nodes := make([]TreeNode, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		absChild := filepath.Join(absDir, name)
		rel, err := filepath.Rel(e.rootAbs, absChild)
		if err != nil {
			continue
		}
		relSlash := filepath.ToSlash(rel)

		if entry.IsDir() {
			if e.ignoredDirs[name] || e.isGitIgnored(name, relSlash) {
				continue
			}
			node := TreeNode{
				Name:  name,
				Path:  relSlash,
				IsDir: true,
			}
			if depth+1 < maxDepth {
				children, err := e.readDirRecursive(absChild, depth+1, maxDepth)
				if err == nil {
					node.Children = children
				}
			}
			nodes = append(nodes, node)
		} else {
			if IsSensitiveRelPath(relSlash) || e.isGitIgnored(name, relSlash) {
				continue
			}
			var size int64
			if info, err := entry.Info(); err == nil {
				size = info.Size()
			}
			nodes = append(nodes, TreeNode{
				Name:     name,
				Path:     relSlash,
				IsDir:    false,
				Size:     size,
				Language: DetectLanguage(name),
			})
		}
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	return nodes, nil
}

func (e *Explorer) isGitIgnored(name, relSlash string) bool {
	for _, pat := range e.gitIgnoreSet {
		if pat == name || pat == relSlash {
			return true
		}
		if matched, err := filepath.Match(pat, name); err == nil && matched {
			return true
		}
	}
	return false
}

// ReadFile reads a text file safely up to 1 MB.
func (e *Explorer) ReadFile(relPath string) (*FileContent, error) {
	absPath, relSlash, err := e.ResolveSafePath(relPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("cannot read directory as file")
	}
	if info.Size() > maxPreviewBytes {
		return nil, ErrTooLarge
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(raw) || strings.ContainsRune(string(raw), '\x00') {
		return nil, ErrBinaryFile
	}

	return &FileContent{
		Path:     relSlash,
		Name:     filepath.Base(absPath),
		Language: DetectLanguage(absPath),
		Size:     info.Size(),
		Content:  string(raw),
	}, nil
}

// DetectLanguage maps common file extensions to highlight.js language identifiers.
func DetectLanguage(filename string) string {
	lower := strings.ToLower(filepath.Base(filename))
	switch lower {
	case "dockerfile":
		return "dockerfile"
	case "makefile":
		return "makefile"
	}
	switch strings.ToLower(filepath.Ext(lower)) {
	case ".go":
		return "go"
	case ".ts", ".mts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".jsx":
		return "jsx"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".json":
		return "json"
	case ".md", ".markdown":
		return "markdown"
	case ".html", ".htm":
		return "xml"
	case ".css", ".scss":
		return "css"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "ini"
	case ".sh", ".bash", ".zsh":
		return "bash"
	case ".ps1":
		return "powershell"
	case ".sql":
		return "sql"
	default:
		return "plaintext"
	}
}
