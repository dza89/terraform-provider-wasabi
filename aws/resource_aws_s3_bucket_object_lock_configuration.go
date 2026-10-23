package aws

import (
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

func resourceAwsS3BucketObjectLockConfiguration() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the Object Lock default retention of a bucket. Object Lock must be enabled when the bucket is created (`object_lock_enabled` of `wasabi_bucket`) and cannot be disabled afterwards, so destroying this resource only removes the default retention rule.",

		Create: resourceAwsS3BucketObjectLockConfigurationPut,
		Read:   resourceAwsS3BucketObjectLockConfigurationRead,
		Update: resourceAwsS3BucketObjectLockConfigurationPut,
		Delete: resourceAwsS3BucketObjectLockConfigurationDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"bucket": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the bucket. Object Lock must have been enabled when the bucket was created, see the `object_lock_enabled` argument of `wasabi_bucket`.",
			},
			"rule": {
				Type:        schema.TypeList,
				Optional:    true,
				MaxItems:    1,
				Description: "The Object Lock rule. Without a rule, new objects get no default retention.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"default_retention": {
							Type:        schema.TypeList,
							Required:    true,
							MaxItems:    1,
							Description: "The default retention applied to new objects.",
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"mode": {
										Type:     schema.TypeString,
										Required: true,
										ValidateFunc: validation.StringInSlice([]string{
											s3.ObjectLockRetentionModeCompliance,
											s3.ObjectLockRetentionModeGovernance,
										}, false),
										Description: "The retention mode, `COMPLIANCE` or `GOVERNANCE`.",
									},
									"days": {
										Type:         schema.TypeInt,
										Optional:     true,
										ValidateFunc: validation.IntAtLeast(1),
										ExactlyOneOf: []string{"rule.0.default_retention.0.days", "rule.0.default_retention.0.years"},
										Description:  "The default retention period in days. Exactly one of `days` and `years` is required.",
									},
									"years": {
										Type:         schema.TypeInt,
										Optional:     true,
										ValidateFunc: validation.IntAtLeast(1),
										ExactlyOneOf: []string{"rule.0.default_retention.0.days", "rule.0.default_retention.0.years"},
										Description:  "The default retention period in years. Exactly one of `days` and `years` is required.",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func resourceAwsS3BucketObjectLockConfigurationPut(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	bucket := d.Get("bucket").(string)

	input := &s3.PutObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
		ObjectLockConfiguration: &s3.ObjectLockConfiguration{
			ObjectLockEnabled: aws.String(s3.ObjectLockEnabledEnabled),
			Rule:              expandS3ObjectLockRule(d.Get("rule").([]interface{})),
		},
	}

	log.Printf("[DEBUG] S3 bucket: %s, put Object Lock configuration: %s", bucket, input)
	_, err := retryOnAwsCode(s3.ErrCodeNoSuchBucket, func() (interface{}, error) {
		return s3conn.PutObjectLockConfiguration(input)
	})
	if err != nil {
		return fmt.Errorf("error putting S3 Bucket (%s) Object Lock configuration: %s", bucket, err)
	}

	d.SetId(bucket)

	return resourceAwsS3BucketObjectLockConfigurationRead(d, meta)
}

func resourceAwsS3BucketObjectLockConfigurationRead(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	var output *s3.GetObjectLockConfigurationOutput
	var err error
	input := &s3.GetObjectLockConfigurationInput{Bucket: aws.String(d.Id())}
	if d.IsNewResource() {
		var v interface{}
		v, err = retryOnAwsCode(s3.ErrCodeNoSuchBucket, func() (interface{}, error) {
			return s3conn.GetObjectLockConfiguration(input)
		})
		if err == nil {
			output = v.(*s3.GetObjectLockConfigurationOutput)
		}
	} else {
		output, err = s3conn.GetObjectLockConfiguration(input)
	}
	if !d.IsNewResource() && (isAWSErr(err, s3.ErrCodeNoSuchBucket, "") || isAWSErr(err, "ObjectLockConfigurationNotFoundError", "")) {
		log.Printf("[WARN] S3 Bucket (%s) or its Object Lock configuration not found, removing from state", d.Id())
		d.SetId("")
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reading S3 Bucket (%s) Object Lock configuration: %s", d.Id(), err)
	}

	config := output.ObjectLockConfiguration
	if config == nil || aws.StringValue(config.ObjectLockEnabled) != s3.ObjectLockEnabledEnabled {
		if d.IsNewResource() {
			return fmt.Errorf("error reading S3 Bucket (%s) Object Lock configuration: Object Lock is not enabled", d.Id())
		}
		log.Printf("[WARN] S3 Bucket (%s) has Object Lock disabled, removing from state", d.Id())
		d.SetId("")
		return nil
	}

	if err := d.Set("bucket", d.Id()); err != nil {
		return err
	}
	if err := d.Set("rule", flattenS3ObjectLockRule(config.Rule)); err != nil {
		return fmt.Errorf("error setting rule: %s", err)
	}

	return nil
}

// Object Lock cannot be disabled once it is enabled for a bucket, so deleting
// the resource removes the default retention rule and leaves Object Lock on.
func resourceAwsS3BucketObjectLockConfigurationDelete(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	log.Printf("[DEBUG] S3 bucket: %s, remove Object Lock default retention", d.Id())
	_, err := s3conn.PutObjectLockConfiguration(&s3.PutObjectLockConfigurationInput{
		Bucket: aws.String(d.Id()),
		ObjectLockConfiguration: &s3.ObjectLockConfiguration{
			ObjectLockEnabled: aws.String(s3.ObjectLockEnabledEnabled),
		},
	})
	if isAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error removing S3 Bucket (%s) Object Lock default retention: %s", d.Id(), err)
	}

	return nil
}

func expandS3ObjectLockRule(l []interface{}) *s3.ObjectLockRule {
	if len(l) == 0 || l[0] == nil {
		return nil
	}
	m := l[0].(map[string]interface{})

	dr, ok := m["default_retention"].([]interface{})
	if !ok || len(dr) == 0 || dr[0] == nil {
		return nil
	}
	retention := dr[0].(map[string]interface{})

	defaultRetention := &s3.DefaultRetention{
		Mode: aws.String(retention["mode"].(string)),
	}
	if v, ok := retention["days"].(int); ok && v > 0 {
		defaultRetention.Days = aws.Int64(int64(v))
	}
	if v, ok := retention["years"].(int); ok && v > 0 {
		defaultRetention.Years = aws.Int64(int64(v))
	}

	return &s3.ObjectLockRule{DefaultRetention: defaultRetention}
}

func flattenS3ObjectLockRule(rule *s3.ObjectLockRule) []interface{} {
	if rule == nil || rule.DefaultRetention == nil {
		return []interface{}{}
	}

	retention := map[string]interface{}{
		"mode":  aws.StringValue(rule.DefaultRetention.Mode),
		"days":  int(aws.Int64Value(rule.DefaultRetention.Days)),
		"years": int(aws.Int64Value(rule.DefaultRetention.Years)),
	}

	return []interface{}{
		map[string]interface{}{
			"default_retention": []interface{}{retention},
		},
	}
}

// s3BucketObjectLockEnabled reports whether Object Lock is enabled for a bucket.
// known is false when the status cannot be determined because the credentials
// or the endpoint do not allow reading the Object Lock configuration; callers
// should then leave their recorded value alone. NoSuchBucket is retried only
// when retryNoSuchBucket is set, for buckets that were created just now.
func s3BucketObjectLockEnabled(s3conn *s3.S3, bucket string, retryNoSuchBucket bool) (enabled, known bool, err error) {
	input := &s3.GetObjectLockConfigurationInput{Bucket: aws.String(bucket)}

	var output *s3.GetObjectLockConfigurationOutput
	if retryNoSuchBucket {
		var v interface{}
		v, err = retryOnAwsCode(s3.ErrCodeNoSuchBucket, func() (interface{}, error) {
			return s3conn.GetObjectLockConfiguration(input)
		})
		if err == nil {
			output = v.(*s3.GetObjectLockConfigurationOutput)
		}
	} else {
		output, err = s3conn.GetObjectLockConfiguration(input)
	}

	if isAWSErr(err, "ObjectLockConfigurationNotFoundError", "") {
		return false, true, nil
	}
	for _, code := range []string{"AccessDenied", "MethodNotAllowed", "NotImplemented"} {
		if isAWSErr(err, code, "") {
			log.Printf("[WARN] S3 Bucket (%s) Object Lock status is unknown, reading the Object Lock configuration failed: %s", bucket, err)
			return false, false, nil
		}
	}
	if err != nil {
		return false, false, err
	}

	config := output.ObjectLockConfiguration
	return config != nil && aws.StringValue(config.ObjectLockEnabled) == s3.ObjectLockEnabledEnabled, true, nil
}
