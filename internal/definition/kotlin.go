package definition

import (
	"os"
	"path/filepath"
	"strings"
)

// Use the enclosing build's root, not the Git root or a Gradle submodule.
// Separate builds inside a monorepo get separate language server sessions.
func kotlinRoot(repo, path string) string {
	rel, err := filepath.Rel(repo, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return repo
	}
	root, build := "", ""
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		for _, marker := range []string{"settings.gradle.kts", "settings.gradle", "pom.xml"} {
			if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && info.Mode().IsRegular() {
				root = dir
				break
			}
		}
		if build == "" {
			for _, marker := range []string{"build.gradle.kts", "build.gradle"} {
				if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && info.Mode().IsRegular() {
					build = dir
					break
				}
			}
		}
		if dir == repo || dir == filepath.Dir(dir) {
			break
		}
	}
	if root != "" {
		return root
	}
	if build != "" {
		return build
	}
	return repo
}
