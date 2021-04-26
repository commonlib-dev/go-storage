package gostorage

import (
	"os"
)

// mkdirIfNotExists create directory including children directory if not exists
func mkdirIfNotExists(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, os.ModePerm)
	}

	return err
}

// isFileExists check if given file path exists and not a directory
func isFileExists(path string) bool {
	stat, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !stat.IsDir()
}
