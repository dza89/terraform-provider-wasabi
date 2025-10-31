package aws

import (
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func dataSourceAwsS3Bucket() *schema.Resource {
	return &schema.Resource{
		Read: dataSourceAwsS3BucketRead,

		Schema: map[string]*schema.Schema{
			"bucket": {
				Type:     schema.TypeString,
				Required: true,
			},
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"bucket_domain_name": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"bucket_regional_domain_name": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"region": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func dataSourceAwsS3BucketRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).s3conn

	bucket := d.Get("bucket").(string)

	input := &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}

	log.Printf("[DEBUG] Reading S3 bucket: %s", input)
	_, err := conn.HeadBucket(input)

	if err != nil {
		return fmt.Errorf("Failed getting S3 bucket: %s Bucket: %q", err, bucket)
	}

	d.SetId(bucket)
	arn := arn.ARN{
		Partition: meta.(*AWSClient).partition,
		Service:   "s3",
		Resource:  bucket,
	}.String()
	d.Set("arn", arn)
	d.Set("bucket_domain_name", meta.(*AWSClient).PartitionHostname(fmt.Sprintf("%s.s3", bucket)))

	err = bucketLocation(meta.(*AWSClient), d, bucket)
	if err != nil {
		return fmt.Errorf("error getting S3 Bucket location: %s", err)
	}

	regionalDomainName, err := BucketRegionalDomainName(bucket, d.Get("region").(string))
	if err != nil {
		return err
	}
	d.Set("bucket_regional_domain_name", regionalDomainName)

	return nil
}

func bucketLocation(client *AWSClient, d *schema.ResourceData, bucket string) error {
	res, err := client.s3conn.GetBucketLocation(&s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return err
	}

	// Handle the special case where us-east-1 returns nil for LocationConstraint
	location := "us-east-1"
	if res.LocationConstraint != nil {
		location = *res.LocationConstraint
	}

	region := s3.NormalizeBucketLocation(location)
	if err := d.Set("region", region); err != nil {
		return err
	}

	return nil
}
