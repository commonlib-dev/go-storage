package gostorage

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

type LocalStorageSignedURLBuilder func(absoluteFilePath string, objectPath string, expireIn time.Duration) (string, error)

type storageLocalFile struct {
	baseDir          string
	publicBaseDir    string
	publicBaseURL    string
	signedURLBuilder LocalStorageSignedURLBuilder
}

// NewLocalStorage create local file storage
func NewLocalStorage(
	baseDir string,
	publicBaseDir string,
	publicBaseURL string,
	signedURLBuilder LocalStorageSignedURLBuilder) Storage {
	if signedURLBuilder == nil {
		signedURLBuilder = func(absoluteFilePath string, objectPath string, expireIn time.Duration) (string, error) {
			return "", fmt.Errorf("[LocalStorage] unsupported signed url builder")
		}
	}

	return &storageLocalFile{
		baseDir:          baseDir,
		publicBaseDir:    publicBaseDir,
		publicBaseURL:    publicBaseURL,
		signedURLBuilder: signedURLBuilder,
	}
}

func (s *storageLocalFile) Read(objectPath string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.baseDir, objectPath))
}

func checkAndCreateParentDirectory(filePath string) error {
	fileDir := filepath.Dir(filePath)
	return mkdirIfNotExists(fileDir)
}

func (s *storageLocalFile) Put(objectPath string, source io.Reader, visibility ObjectVisibility) error {
	filePath := filepath.Join(s.baseDir, objectPath)
	if err := checkAndCreateParentDirectory(filePath); err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, source)

	if visibility == ObjectPublicRead || visibility == ObjectPublicReadWrite {
		return s.makeObjectPublic(objectPath)
	}

	return err
}

func (s *storageLocalFile) Delete(objectPaths ...string) error {
	for _, objectPath := range objectPaths {
		publicPath := filepath.Join(s.publicBaseDir, objectPath)
		if isFileExists(publicPath) {
			if err := os.Remove(publicPath); err != nil {
				return err
			}
		}

		privatePath := filepath.Join(s.baseDir, objectPath)
		if isFileExists(privatePath) {
			if err := os.Remove(privatePath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *storageLocalFile) Copy(srcObjectPath string, dstObjectPath string) error {
	sourceFilePath := filepath.Join(s.baseDir, srcObjectPath)
	if err := checkAndCreateParentDirectory(sourceFilePath); err != nil {
		return err
	}

	sourceStream, err := os.Open(sourceFilePath)
	if err != nil {
		return err
	}
	defer sourceStream.Close()

	destFilePath := filepath.Join(s.baseDir, dstObjectPath)
	if err := checkAndCreateParentDirectory(destFilePath); err != nil {
		return err
	}

	destStream, err := os.Open(destFilePath)
	if err != nil {
		return err
	}
	defer destStream.Close()

	_, err = io.Copy(destStream, sourceStream)
	return err
}

func (s *storageLocalFile) URL(objectPath string) (string, error) {
	if objectPath == "" {
		return "", nil
	}

	filePath := filepath.Join(s.publicBaseDir, objectPath)
	if !isFileExists(filePath) {
		return "", fmt.Errorf("local file does not exists")
	}

	u, err := url.Parse(s.publicBaseURL)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, objectPath)
	return u.String(), nil
}

func (s *storageLocalFile) TemporaryURL(objectPath string, expireIn time.Duration) (string, error) {
	if objectPath == "" {
		return "", nil
	}

	filePath := filepath.Join(s.publicBaseDir, objectPath)
	if !isFileExists(filePath) {
		return "", fmt.Errorf("local file does not exists")
	}

	return s.signedURLBuilder(filePath, objectPath, expireIn)
}

func (s *storageLocalFile) Size(objectPath string) (int64, error) {
	info, err := os.Stat(filepath.Join(s.baseDir, objectPath))
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

func (s *storageLocalFile) LastModified(objectPath string) (time.Time, error) {
	info, err := os.Stat(filepath.Join(s.baseDir, objectPath))
	if err != nil {
		return time.Time{}, err
	}

	return info.ModTime(), nil
}

func (s *storageLocalFile) Exist(objectPath string) (bool, error) {
	info, err := os.Stat(filepath.Join(s.baseDir, objectPath))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		} else {
			return false, err
		}
	}

	return !info.IsDir(), nil
}

func (s *storageLocalFile) SetVisibility(objectPath string, visibility ObjectVisibility) error {
	publicPath := filepath.Join(s.publicBaseDir, objectPath)
	if visibility == ObjectPrivate {
		if isFileExists(publicPath) {
			return os.Remove(publicPath)
		}
	} else if visibility == ObjectPublicRead || visibility == ObjectPublicReadWrite {
		if !isFileExists(publicPath) {
			return s.makeObjectPublic(objectPath)
		}
	} else {
		return fmt.Errorf("err invalid object visibility: %s", visibility)
	}
	return nil
}

func (s *storageLocalFile) GetVisibility(objectPath string) (ObjectVisibility, error) {
	publicPath := filepath.Join(s.publicBaseDir, objectPath)
	if isFileExists(publicPath) {
		return ObjectPublicRead, nil
	}

	filePath := filepath.Join(s.baseDir, objectPath)
	if isFileExists(filePath) {
		return ObjectPrivate, nil
	} else {
		return "", fmt.Errorf("err get visibility, object not found: %s", objectPath)
	}
}

func (s *storageLocalFile) makeObjectPublic(objectPath string) error {
	publicPath := filepath.Join(s.publicBaseDir, objectPath)
	if err := checkAndCreateParentDirectory(publicPath); err != nil {
		return err
	}

	// In windows there's an issue in creating symbolic link
	// issue: "A required privilege is not held by the client"
	// therefore the easiest solution is create a copy/hard link
	// TODO use symbolic link for linux
	if isFileExists(publicPath) {
		if err := os.Remove(publicPath); err != nil {
			return err
		}
	}

	filePath := filepath.Join(s.baseDir, objectPath)
	if err := os.Link(filePath, publicPath); err != nil {
		logrus.Error(err)
		return err
	}
	return nil
}