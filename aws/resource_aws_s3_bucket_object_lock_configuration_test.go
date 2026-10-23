package aws

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccWasabiBucketObjectLockConfiguration_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("terraform-provider-wasabi-test-")
	resourceName := "wasabi_bucket_object_lock_configuration.this"

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckWasabiBucketObjectLockConfigurationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccWasabiBucketObjectLockConfigurationConfig(rName, "GOVERNANCE", "days = 1"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("wasabi_bucket.this", "object_lock_enabled", "true"),
					resource.TestCheckResourceAttr(resourceName, "rule.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "rule.0.default_retention.0.mode", "GOVERNANCE"),
					resource.TestCheckResourceAttr(resourceName, "rule.0.default_retention.0.days", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccWasabiBucketObjectLockConfigurationConfig(rName, "COMPLIANCE", "years = 1"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "rule.0.default_retention.0.mode", "COMPLIANCE"),
					resource.TestCheckResourceAttr(resourceName, "rule.0.default_retention.0.years", "1"),
				),
			},
		},
	})
}

func TestAccWasabiBucket_objectLockDisabled(t *testing.T) {
	rName := acctest.RandomWithPrefix("terraform-provider-wasabi-test-")

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckWasabiBucketResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "wasabi_bucket" "this" {
  bucket = "%s"
}
`, rName),
				Check: resource.TestCheckResourceAttr("wasabi_bucket.this", "object_lock_enabled", "false"),
			},
		},
	})
}

// testAccCheckWasabiBucketObjectLockConfigurationDestroy verifies that no
// Object Lock default retention is left behind and that the bucket is gone.
func testAccCheckWasabiBucketObjectLockConfigurationDestroy(s *terraform.State) error {
	conn := testAccProvider.Meta().(*AWSClient).s3conn

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "wasabi_bucket_object_lock_configuration" {
			continue
		}

		output, err := conn.GetObjectLockConfiguration(&s3.GetObjectLockConfigurationInput{
			Bucket: aws.String(rs.Primary.ID),
		})
		if isAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
			continue
		}
		if err != nil {
			return fmt.Errorf("error reading Object Lock configuration of bucket %s: %s", rs.Primary.ID, err)
		}
		if config := output.ObjectLockConfiguration; config != nil && config.Rule != nil {
			return fmt.Errorf("bucket %s still has an Object Lock default retention rule", rs.Primary.ID)
		}
	}

	return testAccCheckWasabiBucketResourceDestroy(s)
}

func testAccWasabiBucketObjectLockConfigurationConfig(bucket, mode, period string) string {
	return fmt.Sprintf(`
resource "wasabi_bucket" "this" {
  bucket              = "%[1]s"
  object_lock_enabled = true
}

resource "wasabi_bucket_object_lock_configuration" "this" {
  bucket = wasabi_bucket.this.id

  rule {
    default_retention {
      mode = "%[2]s"
      %[3]s
    }
  }
}
`, bucket, mode, period)
}

func TestExpandFlattenS3ObjectLockRule(t *testing.T) {
	rule := &s3.ObjectLockRule{
		DefaultRetention: &s3.DefaultRetention{
			Mode: aws.String(s3.ObjectLockRetentionModeGovernance),
			Days: aws.Int64(7),
		},
	}

	flattened := flattenS3ObjectLockRule(rule)
	expected := []interface{}{
		map[string]interface{}{
			"default_retention": []interface{}{
				map[string]interface{}{"mode": "GOVERNANCE", "days": 7, "years": 0},
			},
		},
	}
	if !reflect.DeepEqual(flattened, expected) {
		t.Fatalf("unexpected flattened rule: %#v", flattened)
	}

	if got := expandS3ObjectLockRule(flattened); !reflect.DeepEqual(got, rule) {
		t.Fatalf("unexpected expanded rule: %s", got)
	}

	if got := flattenS3ObjectLockRule(nil); len(got) != 0 {
		t.Fatalf("expected no rule, got %#v", got)
	}
	if got := expandS3ObjectLockRule(nil); got != nil {
		t.Fatalf("expected no rule, got %s", got)
	}
}
