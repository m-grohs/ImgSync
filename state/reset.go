package state

import (
	"errors"
	"fmt"
	"os"
)

// Reset removes the state database and its SQLite sidecar files, then creates
// a fresh empty database. Bookmark IDs start at 1 again; downloaded files are
// not touched.
func Reset(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", p, err)
		}
	}
	st, err := Open(path)
	if err != nil {
		return err
	}
	return st.Close()
}
