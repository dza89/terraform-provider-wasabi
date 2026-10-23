package aws

import (
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

const errCodeNoSuchLifecycleConfiguration = "NoSuchLifecycleConfiguration"

func resourceAwsS3BucketLifecycleConfiguration() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the lifecycle rules of a bucket. Do not use it together with the inline `lifecycle_rule` blocks of `wasabi_bucket` on the same bucket, because the two would overwrite each other's configuration. Because `wasabi_bucket` removes lifecycle rules that are not in its own configuration, add `ignore_changes = [lifecycle_rule]` to the `lifecycle` block of the bucket. Wasabi has no storage classes, so transitions are not supported, and tag filters are not supported either.",

		Create: resourceAwsS3BucketLifecycleConfigurationPut,
		Read:   resourceAwsS3BucketLifecycleConfigurationRead,
		Update: resourceAwsS3BucketLifecycleConfigurationPut,
		Delete: resourceAwsS3BucketLifecycleConfigurationDelete,
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
			"rule": {
				Type:        schema.TypeList,
				Required:    true,
				MinItems:    1,
				Description: "The lifecycle rules. A rule needs at least one of `expiration`, `noncurrent_version_expiration` and `abort_incomplete_multipart_upload`.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"id": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringLenBetween(1, 255),
							Description:  "The unique identifier of the rule.",
						},
						"status": {
							Type:     schema.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								s3.ExpirationStatusEnabled,
								s3.ExpirationStatusDisabled,
							}, false),
							Description: "Whether the rule is applied, `Enabled` or `Disabled`.",
						},
						"filter": {
							Type:        schema.TypeList,
							Optional:    true,
							MaxItems:    1,
							Description: "Limits the rule to the objects with a key prefix. Without it, the rule applies to all objects. Tag filters are not supported: tags in a filter that was created elsewhere are ignored and removed when this resource updates the rule.",
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"prefix": {
										Type:        schema.TypeString,
										Optional:    true,
										Description: "The key prefix that objects must have.",
									},
								},
							},
						},
						"expiration": {
							Type:        schema.TypeList,
							Optional:    true,
							MaxItems:    1,
							Description: "Expires the current version of the objects.",
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"date": {
										Type:         schema.TypeString,
										Optional:     true,
										ValidateFunc: validateS3BucketLifecycleExpirationDate,
										Description:  "The date after which the objects expire, as a timestamp at midnight UTC such as `2030-01-01T00:00:00Z`. It cannot be combined with `days`.",
									},
									"days": {
										Type:         schema.TypeInt,
										Optional:     true,
										ValidateFunc: validation.IntAtLeast(1),
										Description:  "The number of days after creation after which the objects expire. It cannot be combined with `date`.",
									},
									"expired_object_delete_marker": {
										Type:        schema.TypeBool,
										Optional:    true,
										Description: "Whether to remove delete markers that have no noncurrent versions. It cannot be combined with `date` or `days`.",
									},
								},
							},
						},
						"noncurrent_version_expiration": {
							Type:        schema.TypeList,
							Optional:    true,
							MaxItems:    1,
							Description: "Expires noncurrent object versions.",
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"noncurrent_days": {
										Type:         schema.TypeInt,
										Required:     true,
										ValidateFunc: validation.IntAtLeast(1),
										Description:  "The number of days after which a version becomes noncurrent before it expires.",
									},
								},
							},
						},
						"abort_incomplete_multipart_upload": {
							Type:        schema.TypeList,
							Optional:    true,
							MaxItems:    1,
							Description: "Aborts multipart uploads that are not completed.",
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"days_after_initiation": {
										Type:         schema.TypeInt,
										Required:     true,
										ValidateFunc: validation.IntAtLeast(1),
										Description:  "The number of days after the upload was initiated after which it is aborted.",
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

func resourceAwsS3BucketLifecycleConfigurationPut(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	bucket := d.Get("bucket").(string)

	rules, err := expandS3BucketLifecycleRules(d.Get("rule").([]interface{}))
	if err != nil {
		return err
	}

	input := &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
		LifecycleConfiguration: &s3.BucketLifecycleConfiguration{
			Rules: rules,
		},
	}

	log.Printf("[DEBUG] S3 bucket: %s, put lifecycle configuration: %s", bucket, input)
	_, err = retryOnAwsCode(s3.ErrCodeNoSuchBucket, func() (interface{}, error) {
		return s3conn.PutBucketLifecycleConfiguration(input)
	})
	if err != nil {
		return fmt.Errorf("error putting S3 Bucket (%s) lifecycle configuration: %s", bucket, err)
	}

	d.SetId(bucket)

	return resourceAwsS3BucketLifecycleConfigurationRead(d, meta)
}

func resourceAwsS3BucketLifecycleConfigurationRead(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	var output *s3.GetBucketLifecycleConfigurationOutput
	err := resource.Retry(2*time.Minute, func() *resource.RetryError {
		var err error
		output, err = s3conn.GetBucketLifecycleConfiguration(&s3.GetBucketLifecycleConfigurationInput{
			Bucket: aws.String(d.Id()),
		})
		if d.IsNewResource() &&
			(isAWSErr(err, s3.ErrCodeNoSuchBucket, "") || isAWSErr(err, errCodeNoSuchLifecycleConfiguration, "")) {
			return resource.RetryableError(err)
		}
		if err != nil {
			return resource.NonRetryableError(err)
		}
		return nil
	})
	if isResourceTimeoutError(err) {
		output, err = s3conn.GetBucketLifecycleConfiguration(&s3.GetBucketLifecycleConfigurationInput{
			Bucket: aws.String(d.Id()),
		})
	}
	if !d.IsNewResource() && (isAWSErr(err, s3.ErrCodeNoSuchBucket, "") || isAWSErr(err, errCodeNoSuchLifecycleConfiguration, "")) {
		log.Printf("[WARN] S3 Bucket (%s) or its lifecycle configuration not found, removing from state", d.Id())
		d.SetId("")
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reading S3 Bucket (%s) lifecycle configuration: %s", d.Id(), err)
	}

	if err := d.Set("bucket", d.Id()); err != nil {
		return err
	}
	if err := d.Set("rule", flattenS3BucketLifecycleRules(d, output.Rules)); err != nil {
		return fmt.Errorf("error setting rule: %s", err)
	}

	return nil
}

func resourceAwsS3BucketLifecycleConfigurationDelete(d *schema.ResourceData, meta interface{}) error {
	s3conn := meta.(*AWSClient).s3conn

	log.Printf("[DEBUG] S3 bucket: %s, delete lifecycle configuration", d.Id())
	_, err := s3conn.DeleteBucketLifecycle(&s3.DeleteBucketLifecycleInput{
		Bucket: aws.String(d.Id()),
	})
	if isAWSErr(err, s3.ErrCodeNoSuchBucket, "") || isAWSErr(err, errCodeNoSuchLifecycleConfiguration, "") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error deleting S3 Bucket (%s) lifecycle configuration: %s", d.Id(), err)
	}

	return nil
}

func expandS3BucketLifecycleRules(l []interface{}) ([]*s3.LifecycleRule, error) {
	rules := make([]*s3.LifecycleRule, 0, len(l))

	for _, v := range l {
		m, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		rule := &s3.LifecycleRule{
			ID:     aws.String(m["id"].(string)),
			Status: aws.String(m["status"].(string)),
			Filter: &s3.LifecycleRuleFilter{Prefix: aws.String("")},
		}

		if f, ok := m["filter"].([]interface{}); ok && len(f) > 0 && f[0] != nil {
			rule.Filter.Prefix = aws.String(f[0].(map[string]interface{})["prefix"].(string))
		}

		if e, ok := m["expiration"].([]interface{}); ok && len(e) > 0 && e[0] != nil {
			expiration, err := expandS3BucketLifecycleExpiration(e[0].(map[string]interface{}))
			if err != nil {
				return nil, err
			}
			rule.Expiration = expiration
		}

		if e, ok := m["noncurrent_version_expiration"].([]interface{}); ok && len(e) > 0 && e[0] != nil {
			rule.NoncurrentVersionExpiration = &s3.NoncurrentVersionExpiration{
				NoncurrentDays: aws.Int64(int64(e[0].(map[string]interface{})["noncurrent_days"].(int))),
			}
		}

		if a, ok := m["abort_incomplete_multipart_upload"].([]interface{}); ok && len(a) > 0 && a[0] != nil {
			rule.AbortIncompleteMultipartUpload = &s3.AbortIncompleteMultipartUpload{
				DaysAfterInitiation: aws.Int64(int64(a[0].(map[string]interface{})["days_after_initiation"].(int))),
			}
		}

		if rule.Expiration == nil && rule.NoncurrentVersionExpiration == nil && rule.AbortIncompleteMultipartUpload == nil {
			return nil, fmt.Errorf("lifecycle rule %q needs at least one of expiration, noncurrent_version_expiration and abort_incomplete_multipart_upload", aws.StringValue(rule.ID))
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

func expandS3BucketLifecycleExpiration(m map[string]interface{}) (*s3.LifecycleExpiration, error) {
	expiration := &s3.LifecycleExpiration{}

	date, _ := m["date"].(string)
	days, _ := m["days"].(int)
	marker, _ := m["expired_object_delete_marker"].(bool)

	if date != "" && days > 0 {
		return nil, fmt.Errorf("expiration cannot combine date and days")
	}
	if marker && (date != "" || days > 0) {
		return nil, fmt.Errorf("expired_object_delete_marker cannot be combined with date or days")
	}

	switch {
	case date != "":
		t, err := time.Parse(time.RFC3339, date)
		if err != nil {
			return nil, fmt.Errorf("error parsing S3 Bucket lifecycle expiration date: %s", err)
		}
		expiration.Date = aws.Time(t)
	case days > 0:
		expiration.Days = aws.Int64(int64(days))
	case marker:
		expiration.ExpiredObjectDeleteMarker = aws.Bool(true)
	default:
		return nil, fmt.Errorf("expiration needs one of date, days and expired_object_delete_marker")
	}

	return expiration, nil
}

// validateS3BucketLifecycleExpirationDate accepts only timestamps at midnight
// UTC, which is how the API stores and returns expiration dates; other offsets
// that denote the same instant would otherwise show a permanent diff.
func validateS3BucketLifecycleExpirationDate(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if _, err := time.Parse("2006-01-02T00:00:00Z", value); err != nil {
		errors = append(errors, fmt.Errorf("%q must be a timestamp at midnight UTC such as 2030-01-01T00:00:00Z, got %q", k, value))
	}

	return
}

func flattenS3BucketLifecycleRules(d *schema.ResourceData, rules []*s3.LifecycleRule) []interface{} {
	result := make([]interface{}, 0, len(rules))

	for i, rule := range rules {
		m := map[string]interface{}{
			"id":     aws.StringValue(rule.ID),
			"status": aws.StringValue(rule.Status),
		}

		// The API reports an empty filter for rules that apply to all objects.
		// Only keep a filter block that is configured or that selects a prefix.
		prefix := aws.StringValue(rule.Prefix)
		if rule.Filter != nil {
			prefix = aws.StringValue(rule.Filter.Prefix)
			if rule.Filter.And != nil {
				prefix = aws.StringValue(rule.Filter.And.Prefix)
			}
		}
		if n, ok := d.Get(fmt.Sprintf("rule.%d.filter.#", i)).(int); prefix != "" || (ok && n > 0) {
			m["filter"] = []interface{}{map[string]interface{}{"prefix": prefix}}
		}

		if e := rule.Expiration; e != nil {
			expiration := map[string]interface{}{}
			if e.Date != nil {
				expiration["date"] = aws.TimeValue(e.Date).UTC().Format(time.RFC3339)
			}
			if e.Days != nil {
				expiration["days"] = int(aws.Int64Value(e.Days))
			}
			// The API leaves the marker out, or reports false, unless it is the action.
			if aws.BoolValue(e.ExpiredObjectDeleteMarker) {
				expiration["expired_object_delete_marker"] = true
			}
			m["expiration"] = []interface{}{expiration}
		}

		if e := rule.NoncurrentVersionExpiration; e != nil {
			m["noncurrent_version_expiration"] = []interface{}{
				map[string]interface{}{"noncurrent_days": int(aws.Int64Value(e.NoncurrentDays))},
			}
		}

		if a := rule.AbortIncompleteMultipartUpload; a != nil {
			m["abort_incomplete_multipart_upload"] = []interface{}{
				map[string]interface{}{"days_after_initiation": int(aws.Int64Value(a.DaysAfterInitiation))},
			}
		}

		result = append(result, m)
	}

	return result
}
