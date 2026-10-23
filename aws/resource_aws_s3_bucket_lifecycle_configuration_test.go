package aws

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccWasabiBucketLifecycleConfiguration_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("terraform-provider-wasabi-test-")
	resourceName := "wasabi_bucket_lifecycle_configuration.this"

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckWasabiBucketResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccWasabiBucketLifecycleConfigurationConfig(rName, 30),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "rule.#", "2"),
					resource.TestCheckResourceAttr(resourceName, "rule.0.id", "expire-logs"),
					resource.TestCheckResourceAttr(resourceName, "rule.0.filter.0.prefix", "logs/"),
					resource.TestCheckResourceAttr(resourceName, "rule.0.expiration.0.days", "30"),
					resource.TestCheckResourceAttr(resourceName, "rule.1.noncurrent_version_expiration.0.noncurrent_days", "7"),
					resource.TestCheckResourceAttr(resourceName, "rule.1.abort_incomplete_multipart_upload.0.days_after_initiation", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccWasabiBucketLifecycleConfigurationConfig(rName, 60),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "rule.0.expiration.0.days", "60"),
				),
			},
		},
	})
}

func testAccWasabiBucketLifecycleConfigurationConfig(bucket string, days int) string {
	return fmt.Sprintf(`
resource "wasabi_bucket" "this" {
  bucket = "%[1]s"

  lifecycle {
    ignore_changes = [lifecycle_rule]
  }
}

resource "wasabi_bucket_versioning" "this" {
  bucket = wasabi_bucket.this.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "wasabi_bucket_lifecycle_configuration" "this" {
  bucket = wasabi_bucket.this.id

  rule {
    id     = "expire-logs"
    status = "Enabled"

    filter {
      prefix = "logs/"
    }

    expiration {
      days = %[2]d
    }
  }

  rule {
    id     = "clean-up"
    status = "Enabled"

    noncurrent_version_expiration {
      noncurrent_days = 7
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }

  depends_on = [wasabi_bucket_versioning.this]
}
`, bucket, days)
}

func TestExpandS3BucketLifecycleRules(t *testing.T) {
	rules, err := expandS3BucketLifecycleRules([]interface{}{
		map[string]interface{}{
			"id":     "expire-logs",
			"status": "Enabled",
			"filter": []interface{}{map[string]interface{}{"prefix": "logs/"}},
			"expiration": []interface{}{map[string]interface{}{
				"date": "", "days": 30, "expired_object_delete_marker": false,
			}},
			"noncurrent_version_expiration": []interface{}{},
		},
		map[string]interface{}{
			"id":     "clean-up",
			"status": "Disabled",
			"expiration": []interface{}{map[string]interface{}{
				"date": "2030-01-02T00:00:00Z", "days": 0, "expired_object_delete_marker": false,
			}},
			"noncurrent_version_expiration":     []interface{}{map[string]interface{}{"noncurrent_days": 7}},
			"abort_incomplete_multipart_upload": []interface{}{map[string]interface{}{"days_after_initiation": 1}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	expected := []*s3.LifecycleRule{
		{
			ID:         aws.String("expire-logs"),
			Status:     aws.String("Enabled"),
			Filter:     &s3.LifecycleRuleFilter{Prefix: aws.String("logs/")},
			Expiration: &s3.LifecycleExpiration{Days: aws.Int64(30)},
		},
		{
			ID:                             aws.String("clean-up"),
			Status:                         aws.String("Disabled"),
			Filter:                         &s3.LifecycleRuleFilter{Prefix: aws.String("")},
			Expiration:                     &s3.LifecycleExpiration{Date: aws.Time(time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC))},
			NoncurrentVersionExpiration:    &s3.NoncurrentVersionExpiration{NoncurrentDays: aws.Int64(7)},
			AbortIncompleteMultipartUpload: &s3.AbortIncompleteMultipartUpload{DaysAfterInitiation: aws.Int64(1)},
		},
	}
	if !reflect.DeepEqual(rules, expected) {
		t.Fatalf("unexpected rules:\n%s\nexpected:\n%s", rules, expected)
	}
}

func TestExpandS3BucketLifecycleExpiration(t *testing.T) {
	expiration, err := expandS3BucketLifecycleExpiration(map[string]interface{}{
		"date": "", "days": 0, "expired_object_delete_marker": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !aws.BoolValue(expiration.ExpiredObjectDeleteMarker) || expiration.Days != nil || expiration.Date != nil {
		t.Fatalf("expected only expired_object_delete_marker, got %s", expiration)
	}

	// Days with an explicit false marker, as left behind by a switch from the marker to days.
	expiration, err = expandS3BucketLifecycleExpiration(map[string]interface{}{
		"date": "", "days": 5, "expired_object_delete_marker": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if aws.Int64Value(expiration.Days) != 5 || expiration.ExpiredObjectDeleteMarker != nil {
		t.Fatalf("expected only days, got %s", expiration)
	}

	invalid := []map[string]interface{}{
		{"date": "", "days": 5, "expired_object_delete_marker": true},
		{"date": "2030-01-02T00:00:00Z", "days": 0, "expired_object_delete_marker": true},
		{"date": "2030-01-02T00:00:00Z", "days": 5, "expired_object_delete_marker": false},
		{"date": "", "days": 0, "expired_object_delete_marker": false},
	}
	for _, m := range invalid {
		if _, err := expandS3BucketLifecycleExpiration(m); err == nil {
			t.Errorf("expected an error for %v", m)
		}
	}
}

func TestExpandS3BucketLifecycleRules_noAction(t *testing.T) {
	_, err := expandS3BucketLifecycleRules([]interface{}{
		map[string]interface{}{"id": "empty", "status": "Enabled"},
	})
	if err == nil {
		t.Fatal("expected an error for a rule without an action")
	}
}

func TestValidateS3BucketLifecycleExpirationDate(t *testing.T) {
	for _, value := range []string{"2030-01-02T00:00:00Z"} {
		if _, errs := validateS3BucketLifecycleExpirationDate(value, "date"); len(errs) != 0 {
			t.Errorf("expected %q to be valid, got %v", value, errs)
		}
	}
	for _, value := range []string{"2030-01-02", "2030-01-02T01:00:00Z", "2030-01-02T00:00:00+02:00", "2030-01-02T00:00:00-00:00", "tomorrow"} {
		if _, errs := validateS3BucketLifecycleExpirationDate(value, "date"); len(errs) == 0 {
			t.Errorf("expected %q to be invalid", value)
		}
	}
}

// Switching an expiration from the delete marker to days must not leave the
// marker behind in the plan or in the request.
func TestResourceAwsS3BucketLifecycleConfiguration_markerToDaysDiff(t *testing.T) {
	state := &terraform.InstanceState{
		ID: "example",
		Attributes: map[string]string{
			"id":                       "example",
			"bucket":                   "example",
			"rule.#":                   "1",
			"rule.0.id":                "markers",
			"rule.0.status":            "Enabled",
			"rule.0.expiration.#":      "1",
			"rule.0.expiration.0.date": "",
			"rule.0.expiration.0.days": "0",
			"rule.0.expiration.0.expired_object_delete_marker": "true",
		},
	}
	config := terraform.NewResourceConfigRaw(map[string]interface{}{
		"bucket": "example",
		"rule": []interface{}{
			map[string]interface{}{
				"id":         "markers",
				"status":     "Enabled",
				"expiration": []interface{}{map[string]interface{}{"days": 30}},
			},
		},
	})

	diff, err := resourceAwsS3BucketLifecycleConfiguration().Diff(context.Background(), state, config, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if diff == nil {
		t.Fatal("expected a diff")
	}
	if a := diff.Attributes["rule.0.expiration.0.days"]; a == nil || a.New != "30" {
		t.Errorf("expected days to change to 30, got %#v", a)
	}
	if a := diff.Attributes["rule.0.expiration.0.expired_object_delete_marker"]; a == nil || !(a.NewRemoved || a.New == "false" || a.New == "") {
		t.Errorf("expected the delete marker to be cleared, got %#v", a)
	}

	d := schema.TestResourceDataRaw(t, resourceAwsS3BucketLifecycleConfiguration().Schema, map[string]interface{}{
		"bucket": "example",
		"rule": []interface{}{
			map[string]interface{}{
				"id":         "markers",
				"status":     "Enabled",
				"expiration": []interface{}{map[string]interface{}{"days": 30}},
			},
		},
	})
	rules, err := expandS3BucketLifecycleRules(d.Get("rule").([]interface{}))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if e := rules[0].Expiration; aws.Int64Value(e.Days) != 30 || e.ExpiredObjectDeleteMarker != nil {
		t.Errorf("expected only days in the request, got %s", e)
	}
}

func TestFlattenS3BucketLifecycleRules(t *testing.T) {
	resourceSchema := resourceAwsS3BucketLifecycleConfiguration().Schema

	// A rule without a configured filter drops the empty filter that the API reports.
	d := schema.TestResourceDataRaw(t, resourceSchema, map[string]interface{}{
		"bucket": "example",
		"rule": []interface{}{
			map[string]interface{}{"id": "a", "status": "Enabled"},
			map[string]interface{}{"id": "b", "status": "Enabled", "filter": []interface{}{map[string]interface{}{"prefix": ""}}},
		},
	})

	flattened := flattenS3BucketLifecycleRules(d, []*s3.LifecycleRule{
		{
			ID:         aws.String("a"),
			Status:     aws.String("Enabled"),
			Filter:     &s3.LifecycleRuleFilter{Prefix: aws.String("")},
			Expiration: &s3.LifecycleExpiration{Days: aws.Int64(3)},
		},
		{
			ID:     aws.String("b"),
			Status: aws.String("Enabled"),
			Filter: &s3.LifecycleRuleFilter{Prefix: aws.String("")},
			Expiration: &s3.LifecycleExpiration{
				Date: aws.Time(time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)),
			},
		},
		{
			ID:         aws.String("c"),
			Status:     aws.String("Disabled"),
			Prefix:     aws.String("old/"),
			Expiration: &s3.LifecycleExpiration{ExpiredObjectDeleteMarker: aws.Bool(true)},
		},
		{
			ID:         aws.String("d"),
			Status:     aws.String("Enabled"),
			Filter:     &s3.LifecycleRuleFilter{Prefix: aws.String("")},
			Expiration: &s3.LifecycleExpiration{Days: aws.Int64(9), ExpiredObjectDeleteMarker: aws.Bool(false)},
		},
	})

	expected := []interface{}{
		map[string]interface{}{
			"id": "a", "status": "Enabled",
			"expiration": []interface{}{map[string]interface{}{"days": 3}},
		},
		map[string]interface{}{
			"id": "b", "status": "Enabled",
			"filter":     []interface{}{map[string]interface{}{"prefix": ""}},
			"expiration": []interface{}{map[string]interface{}{"date": "2030-01-02T00:00:00Z"}},
		},
		map[string]interface{}{
			"id": "c", "status": "Disabled",
			"filter":     []interface{}{map[string]interface{}{"prefix": "old/"}},
			"expiration": []interface{}{map[string]interface{}{"expired_object_delete_marker": true}},
		},
		map[string]interface{}{
			"id": "d", "status": "Enabled",
			"expiration": []interface{}{map[string]interface{}{"days": 9}},
		},
	}
	if !reflect.DeepEqual(flattened, expected) {
		t.Fatalf("unexpected rules:\n%#v\nexpected:\n%#v", flattened, expected)
	}
}
