package aws

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// bucketVersioningStatusDisabled is how this resource reports a bucket that
// has never had versioning enabled, for which S3 returns no status.
const bucketVersioningStatusDisabled = "Disabled"

func resourceAwsS3BucketVersioning() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the versioning state of a bucket. Do not use it together with the inline `versioning` block of `wasabi_bucket` on the same bucket, because the two would overwrite each other's configuration.",

		Create: resourceAwsS3BucketVersioningPut,
		Read:   resourceAwsS3BucketVersioningRead,
		Update: resourceAwsS3BucketVersioningPut,
		Delete: resourceAwsS3BucketVersioningDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"bucket": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the bucket.",
			},
			"versioning_configuration": {
				Type:        schema.TypeList,
				Required:    true,
				MaxItems:    1,
				Description: "The versioning configuration.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"status": {
							Type:     schema.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								s3.BucketVersioningStatusEnabled,
								s3.BucketVersioningStatusSuspended,
							}, false),
							Description: "The versioning state, `Enabled` or `Suspended`. A bucket that has never had versioning enabled is reported as `Disabled`.",
						},
					},
				},
			},
		},
	}
}

func resourceAwsS3BucketVersioningPut(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	bucket := d.Get("bucket").(string)

	input := &s3.PutBucketVersioningInput{
		Bucket:                  aws.String(bucket),
		VersioningConfiguration: expandS3BucketVersioningConfiguration(d.Get("versioning_configuration").([]interface{})),
	}

	log.Printf("[DEBUG] S3 bucket: %s, put versioning: %s", bucket, input)
	_, err := retryOnAwsCode(s3.ErrCodeNoSuchBucket, func() (interface{}, error) {
		return s3conn.PutBucketVersioning(input)
	})
	if err != nil {
		return fmt.Errorf("error putting S3 Bucket (%s) versioning: %s", bucket, err)
	}

	d.SetId(bucket)

	status := aws.StringValue(input.VersioningConfiguration.Status)
	if err := waitForS3BucketVersioningStatus(s3conn, bucket, status); err != nil {
		var timeoutErr *resource.TimeoutError
		if !errors.As(err, &timeoutErr) {
			return fmt.Errorf("error waiting for S3 Bucket (%s) versioning to be %s: %s", bucket, status, err)
		}
		log.Printf("[WARN] S3 Bucket (%s) versioning is not %s everywhere yet, a later refresh may report the previous status", bucket, status)
	}

	return resourceAwsS3BucketVersioningRead(d, meta)
}

func resourceAwsS3BucketVersioningRead(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	input := &s3.GetBucketVersioningInput{Bucket: aws.String(d.Id())}

	var output *s3.GetBucketVersioningOutput
	var err error
	if d.IsNewResource() {
		var v interface{}
		v, err = retryOnAwsCode(s3.ErrCodeNoSuchBucket, func() (interface{}, error) {
			return s3conn.GetBucketVersioning(input)
		})
		if err == nil {
			output = v.(*s3.GetBucketVersioningOutput)
		}
	} else {
		output, err = s3conn.GetBucketVersioning(input)
	}
	if !d.IsNewResource() && isAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
		log.Printf("[WARN] S3 Bucket (%s) not found, removing versioning from state", d.Id())
		d.SetId("")
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reading S3 Bucket (%s) versioning: %s", d.Id(), err)
	}

	if err := d.Set("bucket", d.Id()); err != nil {
		return err
	}
	if err := d.Set("versioning_configuration", flattenS3BucketVersioningConfiguration(output)); err != nil {
		return fmt.Errorf("error setting versioning_configuration: %s", err)
	}

	return nil
}

// Versioning cannot be removed from a bucket once it was enabled, so deleting
// the resource suspends it. Buckets with Object Lock must keep versioning
// enabled, so they are left unchanged.
func resourceAwsS3BucketVersioningDelete(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	lock, err := s3conn.GetObjectLockConfiguration(&s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(d.Id()),
	})
	if isAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
		return nil
	}
	if err == nil && lock.ObjectLockConfiguration != nil &&
		aws.StringValue(lock.ObjectLockConfiguration.ObjectLockEnabled) == s3.ObjectLockEnabledEnabled {
		log.Printf("[WARN] S3 Bucket (%s) has Object Lock, leaving versioning enabled", d.Id())
		return nil
	}
	// Other errors, such as a missing Object Lock configuration or missing
	// permission to read it, fall through to the attempt to suspend below.

	output, err := s3conn.GetBucketVersioning(&s3.GetBucketVersioningInput{
		Bucket: aws.String(d.Id()),
	})
	if isAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reading S3 Bucket (%s) versioning: %s", d.Id(), err)
	}
	if aws.StringValue(output.Status) != s3.BucketVersioningStatusEnabled {
		return nil
	}

	log.Printf("[DEBUG] S3 bucket: %s, suspend versioning", d.Id())
	_, err = s3conn.PutBucketVersioning(&s3.PutBucketVersioningInput{
		Bucket: aws.String(d.Id()),
		VersioningConfiguration: &s3.VersioningConfiguration{
			Status: aws.String(s3.BucketVersioningStatusSuspended),
		},
	})
	if isAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
		return nil
	}
	if isAWSErr(err, "InvalidBucketState", "") || isAWSErr(err, "IllegalVersioningConfigurationException", "") {
		log.Printf("[WARN] S3 Bucket (%s) cannot suspend versioning, leaving it enabled: %s", d.Id(), err)
		return nil
	}
	if err != nil {
		return fmt.Errorf("error suspending S3 Bucket (%s) versioning: %s", d.Id(), err)
	}

	return nil
}

// waitForS3BucketVersioningStatus waits until reads return the new status.
// Wasabi propagates versioning changes between its front ends with a delay, so
// a read right after a change can return the previous status. It requires
// several matching reads in a row because consecutive reads can be served by
// different front ends.
func waitForS3BucketVersioningStatus(s3conn *s3.S3, bucket, status string) error {
	conf := &resource.StateChangeConf{
		Pending:                   []string{"pending"},
		Target:                    []string{"propagated"},
		Timeout:                   3 * time.Minute,
		MinTimeout:                2 * time.Second,
		ContinuousTargetOccurence: 3,
		Refresh: func() (interface{}, string, error) {
			output, err := s3conn.GetBucketVersioning(&s3.GetBucketVersioningInput{
				Bucket: aws.String(bucket),
			})
			if err != nil {
				return nil, "", err
			}
			if aws.StringValue(output.Status) != status {
				return output, "pending", nil
			}
			return output, "propagated", nil
		},
	}

	_, err := conf.WaitForState()
	return err
}

func expandS3BucketVersioningConfiguration(l []interface{}) *s3.VersioningConfiguration {
	if len(l) == 0 || l[0] == nil {
		return nil
	}
	m := l[0].(map[string]interface{})

	return &s3.VersioningConfiguration{
		Status: aws.String(m["status"].(string)),
	}
}

func flattenS3BucketVersioningConfiguration(output *s3.GetBucketVersioningOutput) []interface{} {
	status := bucketVersioningStatusDisabled
	if v := aws.StringValue(output.Status); v != "" {
		status = v
	}

	return []interface{}{
		map[string]interface{}{
			"status": status,
		},
	}
}
