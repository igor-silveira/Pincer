package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

func CheckPathAllowed(path string, allowedPaths []string) error {
	if len(allowedPaths) == 0 {
		return nil
	}

	resolved := resolvePath(path)
	for _, allowed := range allowedPaths {
		if isSubPath(resolved, resolvePath(allowed)) {
			return nil
		}
	}

	return fmt.Errorf("sandbox: path %q is not under any allowed directory", path)
}

func CheckPathWritable(path string, readOnlyPaths []string) error {
	if len(readOnlyPaths) == 0 {
		return nil
	}

	resolved := resolvePath(path)
	for _, ro := range readOnlyPaths {
		if isSubPath(resolved, resolvePath(ro)) {
			return fmt.Errorf("sandbox: path %q is under read-only directory %q", path, ro)
		}
	}

	return nil
}

func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	evaled, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return evaled
	}

	cur := abs
	var trail []string
	for {
		parent := filepath.Dir(cur)
		trail = append(trail, filepath.Base(cur))
		if parent == cur {
			break
		}
		resolved, resolveErr := filepath.EvalSymlinks(parent)
		if resolveErr == nil {
			for i := len(trail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, trail[i])
			}
			return resolved
		}
		cur = parent
	}
	return abs
}

func isSubPath(child, parent string) bool {
	if child == parent {
		return true
	}
	prefix := parent + string(filepath.Separator)
	return strings.HasPrefix(child, prefix)
}
