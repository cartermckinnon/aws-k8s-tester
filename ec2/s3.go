package ec2

import (
	"errors"
	"path"
	"path/filepath"

	aws_s3 "github.com/aws/aws-k8s-tester/pkg/aws/s3"
	"github.com/aws/aws-k8s-tester/pkg/fileutil"
	"go.uber.org/zap"
)

func (ts *Tester) createS3() (err error) {
	if ts.cfg.S3BucketCreate {
		if ts.cfg.S3BucketName == "" {
			return errors.New("empty S3 bucket name")
		}
		if err = aws_s3.CreateBucket(ts.lg, ts.s3API, ts.cfg.S3BucketName, ts.cfg.Region, ts.cfg.Name, ts.cfg.S3BucketLifecycleExpirationDays); err != nil {
			return err
		}
	} else {
		ts.lg.Info("skipping S3 bucket creation")
	}
	if ts.cfg.S3BucketName == "" {
		ts.lg.Info("skipping s3 bucket creation")
		return nil
	}
	return ts.cfg.Sync()
}

func (ts *Tester) deleteS3() error {
	if !ts.cfg.S3BucketCreate {
		ts.lg.Info("skipping S3 bucket deletion", zap.String("s3-bucket-name", ts.cfg.S3BucketName))
		return nil
	}
	if ts.cfg.S3BucketCreateKeep {
		ts.lg.Info("skipping S3 bucket deletion", zap.String("s3-bucket-name", ts.cfg.S3BucketName), zap.Bool("s3-bucket-create-keep", ts.cfg.S3BucketCreateKeep))
		return nil
	}
	if err := aws_s3.EmptyBucket(ts.lg, ts.s3API, ts.cfg.S3BucketName); err != nil {
		return err
	}
	return aws_s3.DeleteBucket(ts.lg, ts.s3API, ts.cfg.S3BucketName)
}

func (ts *Tester) uploadToS3() (err error) {
	if ts.cfg.S3BucketName == "" {
		ts.lg.Info("skipping s3 uploads; s3 bucket name is empty")
		return nil
	}

	if err = aws_s3.Upload(
		ts.lg,
		ts.s3API,
		ts.cfg.S3BucketName,
		path.Join(ts.cfg.Name, "aws-k8s-tester-ec2.config.yaml"),
		ts.cfg.ConfigPath,
	); err != nil {
		return err
	}

	logFilePath := ""
	for _, fpath := range ts.cfg.LogOutputs {
		if filepath.Ext(fpath) == ".log" {
			logFilePath = fpath
			break
		}
	}
	if fileutil.Exist(logFilePath) {
		if err = aws_s3.Upload(
			ts.lg,
			ts.s3API,
			ts.cfg.S3BucketName,
			path.Join(ts.cfg.Name, "aws-k8s-tester-ec2.log"),
			logFilePath,
		); err != nil {
			return err
		}
	}

	if fileutil.Exist(ts.cfg.RoleCFNStackYAMLFilePath) {
		if err = aws_s3.Upload(
			ts.lg,
			ts.s3API,
			ts.cfg.S3BucketName,
			path.Join(ts.cfg.Name, "cfn", "aws-k8s-tester-ec2.role.cfn.yaml"),
			ts.cfg.RoleCFNStackYAMLFilePath,
		); err != nil {
			return err
		}
	}

	if fileutil.Exist(ts.cfg.VPCCFNStackYAMLFilePath) {
		if err = aws_s3.Upload(
			ts.lg,
			ts.s3API,
			ts.cfg.S3BucketName,
			path.Join(ts.cfg.Name, "cfn", "aws-k8s-tester-ec2.vpc.cfn.yaml"),
			ts.cfg.VPCCFNStackYAMLFilePath,
		); err != nil {
			return err
		}
	}

	for _, cur := range ts.cfg.ASGs {
		if fileutil.Exist(cur.SSMDocumentCFNStackYAMLFilePath) {
			if err = aws_s3.Upload(
				ts.lg,
				ts.s3API,
				ts.cfg.S3BucketName,
				path.Join(ts.cfg.Name, "cfn", filepath.Base(cur.SSMDocumentCFNStackYAMLFilePath)),
				cur.SSMDocumentCFNStackYAMLFilePath,
			); err != nil {
				return err
			}
		}
		if fileutil.Exist(cur.ASGCFNStackYAMLFilePath) {
			if err = aws_s3.Upload(
				ts.lg,
				ts.s3API,
				ts.cfg.S3BucketName,
				path.Join(ts.cfg.Name, "cfn", filepath.Base(cur.ASGCFNStackYAMLFilePath)),
				cur.ASGCFNStackYAMLFilePath,
			); err != nil {
				return err
			}
		}
	}

	return nil
}
