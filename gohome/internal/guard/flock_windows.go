//go:build windows

package guard

import "os"

// lockFile and unlockFile are no-ops on Windows.
// The in-process sync.Mutex in Whitelist covers the single-process case.
// Cross-process file locking on Windows (LockFileEx) is left for future work.

func lockFile(_ *os.File) error   { return nil }
func unlockFile(_ *os.File) error { return nil }
