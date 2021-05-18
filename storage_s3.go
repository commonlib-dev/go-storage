package gostorage

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/sirupsen/logrus"
)

const (
	maxRetry          = 3           // maximum retry for uploading part
	s3PartSize        = 5120 * 1024 // 5MB is minimum s3 part size upload
	s3SignedURLExpire = 24 * time.Hour
)

type storageS3 struct {
	awsSession *session.Session
	s3         *s3.S3
	bucketName string
}

// NewAWSS3Storage create new storage backed by AWS S3
func NewAWSS3Storage(
	bucketName string,
	region string,
	accessKeyID string,
	secretAccessKey string,
	sessionToken string) Storage {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
		Credentials: credentials.NewStaticCredentials(
			accessKeyID,
			secretAccessKey,
			sessionToken,
		),
	})
	if err != nil {
		panic(err)
	}

	svc := s3.New(sess)
	return &storageS3{
		awsSession: sess,
		s3:         svc,
		bucketName: bucketName,
	}
}

func cleanS3ObjectPath(objectPath string) string {
	return path.Clean(filepath.ToSlash(objectPath))
}

func (s *storageS3) List(objectDir string) ([]ObjectInfo, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (s *storageS3) Read(objectPath string) (io.ReadCloser, error) {
	objectPath = cleanS3ObjectPath(objectPath)
	output, err := s.s3.GetObject(&s3.GetObjectInput{
		Bucket: &s.bucketName,
		Key:    &objectPath,
	})

	if err != nil {
		return nil, err
	}

	return output.Body, nil
}

func (s *storageS3) Put(objectPath string, source io.Reader, visibility ObjectVisibility) error {
	objectPath = cleanS3ObjectPath(objectPath)

	acl, err := getS3ACLOrError(visibility)
	if err != nil {
		return err
	}

	expireAt := time.Now().Add(time.Hour * 6)
	createdResp, err := s.s3.CreateMultipartUpload(&s3.CreateMultipartUploadInput{
		ACL:     acl,
		Bucket:  &s.bucketName,
		Key:     &objectPath,
		Expires: &expireAt,
	})

	if err != nil {
		return err
	}

	var partNumber int64 = 1
	var completedParts []*s3.CompletedPart
	buffer := make([]byte, s3PartSize)
	for {

		bytesRead, err := source.Read(buffer)

		if err != nil && err != io.EOF {
			if err := abortMultipartUpload(s.s3, createdResp); err != nil {
				logrus.Debugf("[S3] error aborting multipart upload, while reading data: %s\n", err.Error())
				return err
			}
			return err
		}

		if bytesRead <= 0 {
			break
		}

		completed, err := uploadMultipart(s.s3, createdResp, buffer[:bytesRead], partNumber)
		if err != nil {
			if err := abortMultipartUpload(s.s3, createdResp); err != nil {
				logrus.Debugf("[S3] error aborting multipart upload: %s\n", err.Error())
				return err
			}
			return err
		}

		partNumber++
		completedParts = append(completedParts, completed)
	}

	completionResp, err := s.s3.CompleteMultipartUpload(&s3.CompleteMultipartUploadInput{
		Bucket:   createdResp.Bucket,
		Key:      createdResp.Key,
		UploadId: createdResp.UploadId,
		MultipartUpload: &s3.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})

	if err != nil {
		return err
	}

	logrus.Debugf("[S3] Upload success: %s\n", completionResp.String())
	return nil
}

func uploadMultipart(service *s3.S3, resp *s3.CreateMultipartUploadOutput, data []byte, partNumber int64) (*s3.CompletedPart, error) {
	uploadInput := &s3.UploadPartInput{
		Bucket:        resp.Bucket,
		Key:           resp.Key,
		UploadId:      resp.UploadId,
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		PartNumber:    aws.Int64(partNumber),
	}

	var retry int
	for retry < maxRetry {
		logrus.Debugf("[S3] uploading (%d bytes) part %d - %s\n", len(data), partNumber, *resp.Key)
		uploadResp, err := service.UploadPart(uploadInput)

		if err != nil {
			retry++
			if retry >= maxRetry {
				return nil, err
			}
			time.Sleep(time.Second * 2)
			logrus.Debugf("[S3] retrying part %d - %s, err: %s\n", partNumber, *resp.Key, err.Error())
			continue
		}

		return &s3.CompletedPart{
			ETag:       uploadResp.ETag,
			PartNumber: &partNumber,
		}, nil
	}
	return nil, nil
}

func abortMultipartUpload(service *s3.S3, resp *s3.CreateMultipartUploadOutput) error {
	_, err := service.AbortMultipartUpload(&s3.AbortMultipartUploadInput{
		Bucket:   resp.Bucket,
		Key:      resp.Key,
		UploadId: resp.UploadId,
	})
	return err
}

func (s *storageS3) Delete(objectPaths ...string) error {
	switch len(objectPaths) {
	case 0:
		return nil
	case 1:
		objectPath := cleanS3ObjectPath(objectPaths[0])
		_, err := s.s3.DeleteObject(&s3.DeleteObjectInput{
			Bucket: &s.bucketName,
			Key:    &objectPath,
		})
		return err
	}

	var objectIdentifiers []*s3.ObjectIdentifier
	for _, objectPath := range objectPaths {
		objectIdentifiers = append(objectIdentifiers, &s3.ObjectIdentifier{
			Key: aws.String(cleanS3ObjectPath(objectPath)),
		})
	}

	_, err := s.s3.DeleteObjects(&s3.DeleteObjectsInput{
		Bucket: &s.bucketName,
		Delete: &s3.Delete{
			Objects: objectIdentifiers,
		},
	})
	return err
}

func (s *storageS3) Copy(srcObjectPath string, dstObjectPath string) error {
	srcObjectPath = cleanS3ObjectPath(srcObjectPath)
	dstObjectPath = cleanS3ObjectPath(dstObjectPath)

	out, err := s.s3.CopyObject(&s3.CopyObjectInput{
		Bucket:     &s.bucketName,
		Key:        &dstObjectPath,
		CopySource: &srcObjectPath,
	})

	if err != nil {
		return err
	}

	logrus.Debug(out)
	return nil
}

func (s *storageS3) URL(objectPath string) (string, error) {
	if objectPath == "" {
		return "", nil
	}
	objectPath = cleanS3ObjectPath(objectPath)
	return fmt.Sprintf("https://%s.s3-%s.amazonaws.com/%s", s.bucketName, *s.awsSession.Config.Region, objectPath), nil
}

func (s *storageS3) TemporaryURL(objectPath string, expireIn time.Duration) (string, error) {
	if expireIn < s3SignedURLExpire {
		expireIn = s3SignedURLExpire
	}

	req, _ := s.s3.GetObjectRequest(&s3.GetObjectInput{
		Bucket: &s.bucketName,
		Key:    &objectPath,
	})

	return req.Presign(expireIn)
}

func (s *storageS3) Size(objectPath string) (int64, error) {
	objectPath = cleanS3ObjectPath(objectPath)

	output, err := s.s3.HeadObject(&s3.HeadObjectInput{
		Bucket: &s.bucketName,
		Key:    &objectPath,
	})
	if err != nil {
		return 0, err
	}

	logrus.Debug(output)
	return *output.ContentLength, nil
}

func (s *storageS3) LastModified(objectPath string) (time.Time, error) {
	objectPath = cleanS3ObjectPath(objectPath)

	output, err := s.s3.HeadObject(&s3.HeadObjectInput{
		Bucket: &s.bucketName,
		Key:    &objectPath,
	})
	if err != nil {
		return time.Time{}, err
	}

	return *output.LastModified, nil
}

func (s *storageS3) Exist(objectPath string) (bool, error) {
	objectPath = cleanS3ObjectPath(objectPath)
	output, err := s.s3.HeadObject(&s3.HeadObjectInput{
		Bucket: &s.bucketName,
		Key:    &objectPath,
	})

	if err != nil {
		return false, err
	}

	return output.LastModified != nil, nil
}

func (s *storageS3) SetVisibility(objectPath string, visibility ObjectVisibility) error {
	objectPath = cleanS3ObjectPath(objectPath)

	if acl, err := getS3ACLOrError(visibility); err == nil {
		_, err = s.s3.PutObjectAcl(&s3.PutObjectAclInput{
			Bucket: &s.bucketName,
			Key:    &objectPath,
			ACL:    acl,
		})
		return err
	} else {
		return err
	}
}

func (s *storageS3) GetVisibility(objectPath string) (ObjectVisibility, error) {
	output, err := s.s3.GetObjectAcl(&s3.GetObjectAclInput{
		Bucket: &s.bucketName,
		Key:    &objectPath,
	})
	if err != nil {
		return "", err
	}

	fmt.Println(output)

	hasRead, hasWrite := false, false
	for _, grant := range output.Grants {
		if aws.StringValue(grant.Grantee.URI) == "http://acs.amazonaws.com/groups/global/AllUsers" {
			if aws.StringValue(grant.Permission) == s3.PermissionRead {
				hasRead = true
			} else if aws.StringValue(grant.Permission) == s3.PermissionWrite {
				hasWrite = true
			}
		}
	}

	if hasRead && hasWrite {
		return ObjectPublicReadWrite, nil
	} else if hasRead {
		return ObjectPublicRead, nil
	} else {
		return "", err
	}
}

func getS3ACLOrError(visibility ObjectVisibility) (*string, error) {
	if visibility == ObjectPublicRead {
		return aws.String(s3.BucketCannedACLPublicRead), nil
	} else if visibility == ObjectPublicReadWrite {
		return aws.String(s3.BucketCannedACLPublicReadWrite), nil
	} else if visibility == ObjectPrivate {
		return aws.String(s3.BucketCannedACLPrivate), nil
	} else {
		return nil, fmt.Errorf("err invalid object visibility: %s", visibility)
	}
}
