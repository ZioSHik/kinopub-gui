package fsutil

import "os"

// IsNonEmptyFile reports whether path currently names a nonempty regular file.
// A completion record alone is not enough: files can be removed outside the app.
func IsNonEmptyFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}
