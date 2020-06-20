// Package s3 implements S3 utilities.
package s3

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-k8s-tester/pkg/fileutil"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/dustin/go-humanize"
	"go.uber.org/zap"
)

// CreateBucket creates a S3 bucket.
func CreateBucket(
	lg *zap.Logger,
	s3API s3iface.S3API,
	bucket string,
	region string,
	lifecyclePrefix string,
	lifecycleExpirationDays int64) (err error) {

	var retry bool
	for i := 0; i < 5; i++ {
		retry, err = createBucket(lg, s3API, bucket, region, lifecyclePrefix, lifecycleExpirationDays)
		if err == nil {
			break
		}
		if retry {
			lg.Warn("failed to create bucket; retrying", zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}
		return err
	}
	return err
}

func createBucket(
	lg *zap.Logger,
	s3API s3iface.S3API,
	bucket string,
	region string,
	lifecyclePrefix string,
	lifecycleExpirationDays int64) (retry bool, err error) {

	lg.Info("creating S3 bucket", zap.String("name", bucket))
	_, err = s3API.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucket),
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(region),
		},
		// https://docs.aws.amazon.com/AmazonS3/latest/dev/acl-overview.html#canned-acl
		// vs. "public-read"
		ACL: aws.String("private"),
	})
	alreadyExist := false
	if err != nil {
		// https://docs.aws.amazon.com/AWSEC2/latest/APIReference/errors-overview.html
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeBucketAlreadyExists:
				lg.Warn("bucket already exists", zap.String("s3-bucket", bucket), zap.Error(err))
				alreadyExist, err = true, nil
			case s3.ErrCodeBucketAlreadyOwnedByYou:
				lg.Warn("bucket already owned by me", zap.String("s3-bucket", bucket), zap.Error(err))
				alreadyExist, err = true, nil
			default:
				if strings.Contains(err.Error(), "OperationAborted: A conflicting conditional operation is currently in progress against this resource. Please try again.") ||
					request.IsErrorRetryable(err) ||
					request.IsErrorThrottle(err) {
					return true, err
				}
				lg.Warn("failed to create bucket", zap.String("s3-bucket", bucket), zap.String("code", aerr.Code()), zap.Error(err))
				return false, err
			}
		}
		if !alreadyExist {
			lg.Warn("failed to create bucket", zap.String("s3-bucket", bucket), zap.String("type", reflect.TypeOf(err).String()), zap.Error(err))
			return false, err
		}
	}
	if alreadyExist {
		return false, nil
	}
	lg.Info("created S3 bucket", zap.String("s3-bucket", bucket))

	_, err = s3API.PutBucketTagging(&s3.PutBucketTaggingInput{
		Bucket: aws.String(bucket),
		Tagging: &s3.Tagging{TagSet: []*s3.Tag{
			{Key: aws.String("Kind"), Value: aws.String("aws-k8s-tester")},
			{Key: aws.String("Creation"), Value: aws.String(time.Now().String())},
		}},
	})
	if err != nil {
		return true, err
	}

	if lifecyclePrefix != "" && lifecycleExpirationDays > 0 {
		_, err = s3API.PutBucketLifecycle(&s3.PutBucketLifecycleInput{
			Bucket: aws.String(bucket),
			LifecycleConfiguration: &s3.LifecycleConfiguration{
				Rules: []*s3.Rule{
					{
						Prefix: aws.String(lifecyclePrefix),
						AbortIncompleteMultipartUpload: &s3.AbortIncompleteMultipartUpload{
							DaysAfterInitiation: aws.Int64(lifecycleExpirationDays),
						},
						Expiration: &s3.LifecycleExpiration{
							Days: aws.Int64(lifecycleExpirationDays),
						},
						ID:     aws.String(fmt.Sprintf("ObjectLifecycleOf%vDays", lifecycleExpirationDays)),
						Status: aws.String("Enabled"),
					},
				},
			},
		})
		if err != nil {
			return true, err
		}
	}

	return false, nil
}

// Upload uploads a file to S3 bucket.
func Upload(
	lg *zap.Logger,
	s3API s3iface.S3API,
	bucket string,
	s3Key string,
	fpath string) error {

	if !fileutil.Exist(fpath) {
		return fmt.Errorf("file %q does not exist; failed to upload to %s/%s", fpath, bucket, s3Key)
	}
	stat, err := os.Stat(fpath)
	if err != nil {
		return err
	}
	size := humanize.Bytes(uint64(stat.Size()))

	lg.Info("uploading",
		zap.String("s3-bucket", bucket),
		zap.String("remote-path", s3Key),
		zap.String("file-size", size),
	)

	rf, err := os.OpenFile(fpath, os.O_RDONLY, 0444)
	if err != nil {
		lg.Warn("failed to read a file", zap.String("file-path", fpath), zap.Error(err))
		return err
	}
	defer rf.Close()

	_, err = s3API.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(s3Key),

		Body: rf,

		// https://docs.aws.amazon.com/AmazonS3/latest/dev/acl-overview.html#canned-acl
		// vs. "public-read"
		ACL: aws.String("private"),

		Metadata: map[string]*string{
			"Kind": aws.String("aws-k8s-tester"),
		},
	})
	if err == nil {
		lg.Info("uploaded",
			zap.String("s3-bucket", bucket),
			zap.String("remote-path", s3Key),
			zap.String("file-size", size),
		)
	} else {
		lg.Warn("failed to upload",
			zap.String("s3-bucket", bucket),
			zap.String("remote-path", s3Key),
			zap.String("file-size", size),
			zap.Error(err),
		)
	}
	return err
}

// UploadBody uploads the body reader to S3.
func UploadBody(
	lg *zap.Logger,
	s3API s3iface.S3API,
	bucket string,
	s3Key string,
	body io.ReadSeeker) (err error) {

	lg.Info("uploading",
		zap.String("s3-bucket", bucket),
		zap.String("remote-path", s3Key),
	)
	_, err = s3API.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(s3Key),

		Body: body,

		// https://docs.aws.amazon.com/AmazonS3/latest/dev/acl-overview.html#canned-acl
		// vs. "public-read"
		ACL: aws.String("private"),

		Metadata: map[string]*string{
			"Kind": aws.String("aws-k8s-tester"),
		},
	})
	if err == nil {
		lg.Info("uploaded",
			zap.String("s3-bucket", bucket),
			zap.String("remote-path", s3Key),
		)
	} else {
		lg.Warn("failed to upload",
			zap.String("s3-bucket", bucket),
			zap.String("remote-path", s3Key),
			zap.Error(err),
		)
	}
	return err
}

// EmptyBucket empties S3 bucket, by deleting all files in the bucket.
func EmptyBucket(lg *zap.Logger, s3API s3iface.S3API, bucket string) error {
	lg.Info("emptying bucket", zap.String("s3-bucket", bucket))
	batcher := s3manager.NewBatchDeleteWithClient(s3API)
	iter := &s3manager.DeleteListIterator{
		Bucket: aws.String(bucket),
		Paginator: request.Pagination{
			NewRequest: func() (*request.Request, error) {
				req, _ := s3API.ListObjectsRequest(&s3.ListObjectsInput{
					Bucket: aws.String(bucket),
				})
				return req, nil
			},
		},
	}
	err := batcher.Delete(aws.BackgroundContext(), iter)
	if err != nil { // https://docs.aws.amazon.com/AWSEC2/latest/APIReference/errors-overview.html
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket:
				lg.Info("no such bucket", zap.String("s3-bucket", bucket), zap.Error(err))
				return nil
			}
		}
		lg.Warn("failed to empty bucket", zap.String("s3-bucket", bucket), zap.Error(err))
		return err
	}
	lg.Info("emptied bucket", zap.String("s3-bucket", bucket))
	return nil
}

// DeleteBucket deletes S3 bucket.
func DeleteBucket(lg *zap.Logger, s3API s3iface.S3API, bucket string) error {
	lg.Info("deleting bucket", zap.String("s3-bucket", bucket))
	_, err := s3API.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket:
				lg.Info("no such bucket", zap.String("s3-bucket", bucket), zap.Error(err))
				return nil
			}
		}
		lg.Warn("failed to delete bucket", zap.String("s3-bucket", bucket), zap.Error(err))
	}

	lg.Info("deleted bucket", zap.String("s3-bucket", bucket))
	return nil
}

// DownloadDir downloads all files from the directory in the S3 bucket.
func DownloadDir(lg *zap.Logger, s3API s3iface.S3API, bucket string, s3Dir string) (targetDir string, err error) {
	if s3Dir[len(s3Dir)-1] == '/' {
		s3Dir = s3Dir[:len(s3Dir)-1]
	}
	dirPfx := "download-s3-bucket-dir-" + bucket + s3Dir
	dirPfx = strings.Replace(dirPfx, "/", "", -1)
	lg.Info("creating temp dir", zap.String("dir-prefix", dirPfx))
	targetDir = fileutil.MkTmpDir(os.TempDir(), dirPfx)

	lg.Info("downloading directory from bucket",
		zap.String("s3-bucket", bucket),
		zap.String("s3-dir", s3Dir),
		zap.String("target-dir", targetDir),
	)
	objects := make([]*s3.Object, 0, 100)
	pageNum := 0
	err = s3API.ListObjectsPages(
		&s3.ListObjectsInput{
			Bucket: aws.String(bucket),
			Prefix: aws.String(s3Dir),
		},
		func(page *s3.ListObjectsOutput, lastPage bool) bool {
			objects = append(objects, page.Contents...)
			pageNum++
			lg.Info("listing",
				zap.String("s3-bucket", bucket),
				zap.Int("page-num", pageNum),
				zap.Bool("last-page", lastPage),
				zap.Int("returned-objects", len(page.Contents)),
				zap.Int("total-objects", len(objects)),
			)
			return true
		},
	)
	if err != nil {
		os.RemoveAll(targetDir)
		return "", err
	}
	for _, obj := range objects {
		time.Sleep(300 * time.Millisecond)

		key := aws.StringValue(obj.Key)
		lg.Info("downloading object",
			zap.String("key", key),
			zap.String("size", humanize.Bytes(uint64(aws.Int64Value(obj.Size)))),
		)
		resp, err := s3API.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    obj.Key,
		})
		if err != nil {
			lg.Warn("failed to get object", zap.String("key", key), zap.Error(err))
			continue
		}
		fpath := filepath.Join(targetDir, key)
		if err = os.MkdirAll(filepath.Dir(fpath), 0700); err != nil {
			lg.Warn("failed to mkdir", zap.String("key", key), zap.Error(err))
			continue
		}
		f, err := os.OpenFile(fpath, os.O_RDWR|os.O_TRUNC, 0777)
		if err != nil {
			f, err = os.Create(fpath)
			if err != nil {
				lg.Warn("failed to write file", zap.String("key", key), zap.Error(err))
				continue
			}
		}
		n, err := io.Copy(f, resp.Body)
		f.Close()
		resp.Body.Close()
		lg.Info("downloaded object",
			zap.String("key", key),
			zap.String("size", humanize.Bytes(uint64(aws.Int64Value(obj.Size)))),
			zap.String("copied-size", humanize.Bytes(uint64(n))),
			zap.Error(err),
		)
	}
	lg.Info("downloaded directory from bucket",
		zap.String("s3-bucket", bucket),
		zap.String("s3-dir", s3Dir),
		zap.String("target-dir", targetDir),
	)
	return targetDir, nil
}
