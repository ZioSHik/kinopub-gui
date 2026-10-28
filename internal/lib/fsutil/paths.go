package fsutil

import "path/filepath"

// CanonicalPath resolves relative paths and existing symlinked parents, including
// output folders that will only be created when downloading starts.
func CanonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	var tail []string
	for {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return filepath.Clean(path)
		}
		tail = append(tail, filepath.Base(abs))
		abs = parent
	}
}
