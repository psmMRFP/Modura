package migrationtest

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// migrationsDir resolves the repository migration directory relative to this
// file so every calling package observes the same migration set.
func migrationsDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "migrations")
}

func migrationNames() ([]string, error) {
	entries, err := os.ReadDir(migrationsDir())
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func migrationFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(migrationsDir(), name))
}
