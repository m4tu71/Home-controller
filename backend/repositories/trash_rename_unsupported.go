//go:build !linux || (!amd64 && !arm64)

package repositories

// Platforms without an audited no-replace rename fail closed.
func trashRenameNoReplace(source, target string) error { return ErrTrashBoundary }
