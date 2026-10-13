package helpers

import (
	"log"
	"os"
	"path/filepath"
)

func RepoRoot(consumer string) string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal("can't get current filepath, error:" + err.Error())
		return ""
	}

	for i := 0; i < 10; i++ {
		switch consumer {
		case "log":
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir
			}
		case "env":
			if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
				return dir
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	log.Fatal("repo root not found")
	return ""
}
