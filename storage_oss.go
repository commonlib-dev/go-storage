package gostorage

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

const ossSignedURLExpire = 24 * time.Hour // 1 Day

type storageAlibabaOSS struct {
	client *oss.Client
	bucket *oss.Bucket
}

// NewAlibabaOSSStorage create storage backed by alibaba oss
func NewAlibabaOSSStorage(
	bucketName string,
	endpoint string,
	accessID string,
	accessSecret string) Storage {

	client, err := oss.New(endpoint, accessID, accessSecret)
	if err != nil {
		panic(err)
	}

	bucket, err := client.Bucket(bucketName)
	if err != nil {
		panic(err)
	}

	return &storageAlibabaOSS{
		client: client,
		bucket: bucket,
	}
}

func cleanOSSObjectPath(objectPath string) string {
	return path.Clean(filepath.ToSlash(objectPath))
}

func (s *storageAlibabaOSS) Read(objectPath string) (io.ReadCloser, error) {
	return s.bucket.GetObject(cleanOSSObjectPath(objectPath))
}

func (s *storageAlibabaOSS) Put(objectPath string, source io.Reader, visibility ObjectVisibility) error {
	var ossOptions []oss.Option
	if acl, err := getACLOSSOrError(visibility); err == nil {
		ossOptions = append(ossOptions, oss.ObjectACL(acl))
	} else {
		return err
	}

	return s.bucket.PutObject(cleanOSSObjectPath(objectPath), source, ossOptions...)
}

func (s *storageAlibabaOSS) Delete(objectPaths ...string) error {
	switch len(objectPaths) {
	case 0:
		return nil
	case 1:
		return s.bucket.DeleteObject(cleanOSSObjectPath(objectPaths[0]))
	}

	var cleanedPaths []string
	for _, objectPath := range objectPaths {
		cleanedPaths = append(cleanedPaths, cleanOSSObjectPath(objectPath))
	}
	_, err := s.bucket.DeleteObjects(objectPaths)
	return err
}

func (s *storageAlibabaOSS) Copy(srcObjectPath string, dstObjectPath string) error {
	_, err := s.bucket.CopyObject(cleanOSSObjectPath(srcObjectPath), cleanOSSObjectPath(dstObjectPath))
	return err
}

func (s *storageAlibabaOSS) URL(objectPath string) (string, error) {
	if objectPath == "" {
		return "", nil
	}

	objectPath = cleanOSSObjectPath(objectPath)
	return fmt.Sprintf("https://%s.%s/%s", s.bucket.BucketName, s.bucket.GetConfig().Endpoint, objectPath), nil
}

func (s *storageAlibabaOSS) TemporaryURL(objectPath string, expireIn time.Duration) (string, error) {
	if expireIn < ossSignedURLExpire {
		expireIn = ossSignedURLExpire
	}

	return s.bucket.SignURL(objectPath, oss.HTTPGet, int64(expireIn))
}

func (s *storageAlibabaOSS) Size(objectPath string) (int64, error) {
	r, err := s.bucket.GetObjectMeta(cleanOSSObjectPath(objectPath))
	if err != nil {
		return 0, err
	}

	sizeStr := r.Get("Content-Length")
	return strconv.ParseInt(sizeStr, 10, 64)
}

func (s *storageAlibabaOSS) LastModified(objectPath string) (time.Time, error) {
	r, err := s.bucket.GetObjectMeta(cleanOSSObjectPath(objectPath))
	if err != nil {
		return time.Time{}, err
	}

	LastModified, err := http.ParseTime(r.Get("Last-Modified"))
	if err != nil {
		return time.Time{}, err
	}

	return LastModified, nil
}

func (s *storageAlibabaOSS) Exist(objectPath string) (bool, error) {
	return s.bucket.IsObjectExist(cleanOSSObjectPath(objectPath))
}

func (s *storageAlibabaOSS) SetVisibility(objectPath string, visibility ObjectVisibility) error {
	if acl, err := getACLOSSOrError(visibility); err == nil {
		return s.bucket.SetObjectACL(cleanOSSObjectPath(objectPath), acl)
	} else {
		return err
	}
}

func (s *storageAlibabaOSS) GetVisibility(objectPath string) (ObjectVisibility, error) {
	result, err := s.bucket.GetObjectACL(cleanOSSObjectPath(objectPath))
	if err != nil {
		return "", err
	}

	aclType := oss.ACLType(result.ACL)
	if aclType == oss.ACLPrivate {
		return ObjectPrivate, nil
	} else if aclType == oss.ACLPublicRead {
		return ObjectPublicRead, nil
	} else if aclType == oss.ACLPublicReadWrite {
		return ObjectPublicReadWrite, nil
	}

	return "", fmt.Errorf("invalid returned ACL value")
}

func getACLOSSOrError(visibility ObjectVisibility) (oss.ACLType, error) {
	if visibility == ObjectPublicRead {
		return oss.ACLPublicRead, nil
	} else if visibility == ObjectPublicReadWrite {
		return oss.ACLPublicReadWrite, nil
	} else if visibility == ObjectPrivate {
		return oss.ACLPrivate, nil
	} else {
		return "", fmt.Errorf("err invalid object visibility: %s", visibility)
	}
}
