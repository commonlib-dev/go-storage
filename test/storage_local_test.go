package test

import (
	gostorage "github.com/commonlib-dev/go-storage"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func cleanTestDir() {
	if err := os.RemoveAll("./storage-test"); err != nil {
		panic(err)
	}
}

func getLocalStorage() gostorage.Storage {
	cleanTestDir()

	return gostorage.NewLocalStorage(
		"storage-test/private",
		"storage-test/public",
		"http://localhost:8000/files",
		nil)
}

func Test_CreateReadDeleteFile(t *testing.T) {
	storage := getLocalStorage()
	srcData := "Hello, this is file content 😊 😅"
	objectPath := "user-files/sample.txt"

	// Save data
	err := storage.Put(objectPath, strings.NewReader(srcData), gostorage.ObjectPublicRead)
	require.NoError(t, err)

	// Check if exist
	exist, err := storage.Exist(objectPath)
	require.NoError(t, err)
	require.True(t, exist)

	// Read file content
	obj, err := storage.Read(objectPath)
	require.NoError(t, err)

	content, err := ioutil.ReadAll(obj)
	require.NoError(t, err)
	require.Equal(t, srcData, string(content))
	_ = obj.Close()

	// Delete file object
	err = storage.Delete(objectPath)
	require.NoError(t, err)

	// Check if exist and should not
	exist, err = storage.Exist(objectPath)
	require.NoError(t, err)
	require.False(t, exist)

	// Clean up
	cleanTestDir()
}

func Test_CopyFile(t *testing.T) {
	storage := getLocalStorage()
	srcData := "Hello, this is file content 😊 😅"
	objectPath := "test-file-original.txt"
	copyObjectPath := "test-file-copied.txt"

	// Save data
	err := storage.Put(objectPath, strings.NewReader(srcData), gostorage.ObjectPublicRead)
	require.NoError(t, err)

	// Copy object
	err = storage.Copy(objectPath, copyObjectPath)
	require.NoError(t, err)

	// Check copied file exists
	exist, err := storage.Exist(copyObjectPath)
	require.NoError(t, err)
	require.True(t, exist)

	// Read copied file content
	obj, err := storage.Read(copyObjectPath)
	require.NoError(t, err)

	content, err := ioutil.ReadAll(obj)
	require.NoError(t, err)
	require.Equal(t, srcData, string(content))
	_ = obj.Close()

	// Clean up
	cleanTestDir()
}

func Test_ListObjectInDirectory(t *testing.T) {
	storage := getLocalStorage()
	srcData := "Hello, this is file content 😊 😅"
	objectPaths := []string{
		"test.txt",
		"test2.txt",
		"my-dir/test.txt",
		"my-dir/test2.txt",
		"my-dir-2/test.txt",
	}

	// Save data
	for _, p := range objectPaths {
		err := storage.Put(p, strings.NewReader(srcData), gostorage.ObjectPublicRead)
		require.NoError(t, err)
	}

	// List object inside root dir
	result, err := storage.List("/")
	t.Logf("list / = %+v", result)
	require.NoError(t, err)
	require.Equal(t, 4, len(result))

	// List object inside my-dir
	result, err = storage.List("my-dir")
	t.Logf("list /my-dir = %+v", result)
	require.NoError(t, err)
	require.Equal(t, 2, len(result))

	// Clean up
	cleanTestDir()
}
