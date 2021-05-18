package gostorage

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"
)

// LocalStorageSignedURLBuilder is used to serve file temporarily in private directory mode
type LocalStorageSignedURLBuilder func(absoluteFilePath string, objectPath string, expireIn time.Duration) (string, error)

type storageLocalFile struct {
	baseDir          string
	publicBaseDir    string
	publicBaseURL    string
	signedURLBuilder LocalStorageSignedURLBuilder
}

// NewLocalStorage create local file storage
// with given params
// baseDir: base directory where a private file is stored (conventionally directory should not publicly serve over http)
// publicBaseDir: base directory where a public file reside, the file actually a link from baseDir, directory should be publicly serve over http
// publicBaseURL: base URL where to be concatenated with objectPath to build full file download URL
// signedURLBuilder: used to generate temporary download URL for serving private files if needed (provide nil will always return error)
func NewLocalStorage(
	baseDir string,
	publicBaseDir string,
	publicBaseURL string,
	signedURLBuilder LocalStorageSignedURLBuilder) Storage {
	if signedURLBuilder == nil {
		signedURLBuilder = func(absoluteFilePath string, objectPath string, expireIn time.Duration) (string, error) {
			return "", fmt.Errorf("[local-storage] unsupported signed url builder")
		}
	}

	return &storageLocalFile{
		baseDir:          baseDir,
		publicBaseDir:    publicBaseDir,
		publicBaseURL:    publicBaseURL,
		signedURLBuilder: signedURLBuilder,
	}
}

func (s *storageLocalFile) List(objectDir string) ([]ObjectInfo, error) {
	baseDir := filepath.Join(s.baseDir, objectDir)

	infoList, err := ioutil.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	var objectInfoList []ObjectInfo
	for _, i := range infoList {
		objectInfoList = append(objectInfoList, ObjectInfo{
			ObjectPath: i.Name(),
			IsDir:      i.IsDir(),
		})
	}

	return objectInfoList, nil
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

	destFile, err := os.Create(destFilePath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceStream)
	return err
}

func (s *storageLocalFile) URL(objectPath string) (string, error) {
	if objectPath == "" {
		return "", nil
	}

	filePath := filepath.Join(s.publicBaseDir, objectPath)
	if !isFileExists(filePath) {
		return "", fmt.Errorf("[local-storage] file not found in given public path")
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

	filePath := filepath.Join(s.baseDir, objectPath)
	if isFileExists(filePath) {
		return s.signedURLBuilder(filePath, objectPath, expireIn)
	}

	publicURL, err := s.URL(objectPath)
	if err != nil {
		return "", fmt.Errorf("[local-storage] err file not found in given public/private path")
	}

	return publicURL, nil
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
		return fmt.Errorf("[local-storage] err invalid object visibility: %s", visibility)
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
		return "", fmt.Errorf("[local-storage] err get visibility, object not found: %s", objectPath)
	}
}

func (s *storageLocalFile) makeObjectPublic(objectPath string) error {
	publicPath := filepath.Join(s.publicBaseDir, objectPath)
	if err := checkAndCreateParentDirectory(publicPath); err != nil {
		return err
	}

	if isFileExists(publicPath) {
		if err := os.Remove(publicPath); err != nil {
			return err
		}
	}

	filePath := filepath.Join(s.baseDir, objectPath)

	if runtime.GOOS == "linux" {
		absFilePath, err := filepath.Abs(filepath.ToSlash(filePath))
		if err != nil {
			return fmt.Errorf("[local-storage] err while creating abs path: %s", err)
		}

		if err := os.Symlink(absFilePath, publicPath); err != nil {
			return fmt.Errorf("[local-storage] err creating sym link: %s", err)
		}
		return nil
	}

	// windows
	// In windows there's an issue in creating symbolic link
	// issue: "A required privilege is not held by the client"
	// therefore the easiest solution is create a copy/hard link
	return os.Link(filePath, publicPath)
}
