//go:build unix

package retention

import (
	"fmt"
	"syscall"
)

// FreeBytes returns the free space available to an unprivileged user.
//
// It uses Bavail and not Bfree on purpose: part of the free blocks is reserved
// for root, and counting on them would make dwnvr think there's still space
// when it can no longer write.
func FreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return int64(st.Bavail) * int64(st.Bsize), nil //nolint:unconvert // Bsize is int32 on linux/arm and uint32 on darwin
}

// TotalBytes is the file system capacity.
func TotalBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return int64(st.Blocks) * int64(st.Bsize), nil //nolint:unconvert // Bsize is int32 on linux/arm and uint32 on darwin
}
