package worker

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HashOptions configures a source hash computation.
type HashOptions struct {
	RepoDir        string // root directory of the repository
	RuntimeProfile string // one of: node-http, go-http, worker-metrics, or empty for default
}

// Hash computes a deterministic SHA-256 hash of the source files relevant to
// the given runtime profile. Returns an empty string when the directory does
// not exist or no matching files are found.
func Hash(opts HashOptions) string {
	if opts.RepoDir == "" {
		return ""
	}
	info, err := os.Stat(opts.RepoDir)
	if err != nil || !info.IsDir() {
		return ""
	}

	files := collectManifest(opts.RepoDir, opts.RuntimeProfile)
	if len(files) == 0 {
		return ""
	}

	h := sha256.New()
	for _, f := range files {
		rel, _ := filepath.Rel(opts.RepoDir, f)
		// Match shell format: relative-path NUL file-contents NUL
		h.Write([]byte(rel))
		h.Write([]byte{0})
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func collectManifest(repoDir, profile string) []string {
	var files []string

	switch profile {
	case "node-http":
		srcDir := filepath.Join(repoDir, "src")
		if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
			nodeExts := map[string]bool{
				".ts": true, ".tsx": true, ".js": true, ".jsx": true,
				".mjs": true, ".cjs": true, ".json": true,
			}
			_ = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if nodeExts[filepath.Ext(path)] {
					files = append(files, path)
				}
				return nil
			})
		}
		extras := []string{
			"package.json", "pnpm-lock.yaml", "package-lock.json",
			"yarn.lock", "tsconfig.json", "tsconfig.build.json",
		}
		for _, name := range extras {
			p := filepath.Join(repoDir, name)
			if _, err := os.Stat(p); err == nil {
				files = append(files, p)
			}
		}

	case "go-http", "worker-metrics":
		goExts := map[string]bool{".go": true}
		goNames := map[string]bool{"go.mod": true, "go.sum": true}
		_ = filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if goExts[filepath.Ext(path)] || goNames[info.Name()] {
				files = append(files, path)
			}
			return nil
		})

	default:
		// Fallback: maxdepth 2
		_ = filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(repoDir, path)
			depth := strings.Count(rel, string(filepath.Separator))
			if info.IsDir() {
				if depth >= 2 {
					return filepath.SkipDir
				}
				return nil
			}
			if depth <= 2 {
				files = append(files, path)
			}
			return nil
		})
	}

	sort.Strings(files)
	return files
}
