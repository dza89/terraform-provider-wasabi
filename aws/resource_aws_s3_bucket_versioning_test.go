package aws

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestAccWasabiBucketVersioning_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("terraform-provider-wasabi-test-")
	resourceName := "wasabi_bucket_versioning.this"

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckWasabiBucketResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccWasabiBucketVersioningConfig(rName, "Enabled"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "versioning_configuration.0.status", "Enabled"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccWasabiBucketVersioningConfig(rName, "Suspended"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "versioning_configuration.0.status", "Suspended"),
				),
			},
		},
	})
}

func testAccWasabiBucketVersioningConfig(bucket, status string) string {
	return fmt.Sprintf(`
resource "wasabi_bucket" "this" {
  bucket = "%[1]s"
}

resource "wasabi_bucket_versioning" "this" {
  bucket = wasabi_bucket.this.id

  versioning_configuration {
    status = "%[2]s"
  }
}
`, bucket, status)
}

func TestResourceAwsS3BucketVersioningDelete(t *testing.T) {
	testCases := []struct {
		name         string
		lockBody     string
		lockStatus   int
		versioning   string
		putStatus    int
		expectPut    bool
		expectFailed bool
	}{
		{
			name:       "object lock bucket keeps versioning without a request",
			lockStatus: http.StatusOK,
			lockBody:   `<ObjectLockConfiguration><ObjectLockEnabled>Enabled</ObjectLockEnabled></ObjectLockConfiguration>`,
		},
		{
			name:       "bucket without object lock is suspended",
			lockStatus: http.StatusNotFound,
			lockBody:   s3VersioningTestErrorBody("ObjectLockConfigurationNotFoundError"),
			versioning: `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`,
			putStatus:  http.StatusOK,
			expectPut:  true,
		},
		{
			name:       "unreadable object lock falls back to the rejected suspend",
			lockStatus: http.StatusForbidden,
			lockBody:   s3VersioningTestErrorBody("AccessDenied"),
			versioning: `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`,
			putStatus:  http.StatusBadRequest,
			expectPut:  true,
		},
		{
			name:         "other suspend failure is reported",
			lockStatus:   http.StatusNotFound,
			lockBody:     s3VersioningTestErrorBody("ObjectLockConfigurationNotFoundError"),
			versioning:   `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`,
			putStatus:    http.StatusConflict,
			expectPut:    true,
			expectFailed: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			put := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				query := r.URL.Query()
				_, isLock := query["object-lock"]
				_, isVersioning := query["versioning"]
				switch {
				case isLock:
					w.WriteHeader(tc.lockStatus)
					_, _ = w.Write([]byte(tc.lockBody))
				case isVersioning && r.Method == http.MethodGet:
					_, _ = w.Write([]byte(tc.versioning))
				case isVersioning && r.Method == http.MethodPut:
					put = true
					if tc.putStatus == http.StatusBadRequest {
						w.WriteHeader(tc.putStatus)
						_, _ = w.Write([]byte(s3VersioningTestErrorBody("IllegalVersioningConfigurationException")))
					} else if tc.putStatus == http.StatusConflict {
						w.WriteHeader(tc.putStatus)
						_, _ = w.Write([]byte(s3VersioningTestErrorBody("OperationAborted")))
					}
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
			}))
			defer server.Close()

			sess := session.Must(session.NewSession(&aws.Config{
				Credentials:      credentials.NewStaticCredentials("AKIATEST", "secret", ""),
				Endpoint:         aws.String(server.URL),
				Region:           aws.String("us-east-1"),
				S3ForcePathStyle: aws.Bool(true),
				DisableSSL:       aws.Bool(true),
				MaxRetries:       aws.Int(0),
			}))

			d := schema.TestResourceDataRaw(t, resourceAwsS3BucketVersioning().Schema, map[string]interface{}{
				"bucket":                   "example",
				"versioning_configuration": []interface{}{map[string]interface{}{"status": "Enabled"}},
			})
			d.SetId("example")

			err := resourceAwsS3BucketVersioningDelete(d, &AWSClient{s3conn: s3.New(sess)})
			if (err != nil) != tc.expectFailed {
				t.Errorf("unexpected error state: %v", err)
			}
			if put != tc.expectPut {
				t.Errorf("expected suspend request = %t, got %t", tc.expectPut, put)
			}
		})
	}
}

func TestFlattenS3BucketVersioningConfiguration(t *testing.T) {
	testCases := []struct {
		status   *string
		expected string
	}{
		{aws.String(s3.BucketVersioningStatusEnabled), "Enabled"},
		{aws.String(s3.BucketVersioningStatusSuspended), "Suspended"},
		{nil, "Disabled"},
	}

	for _, tc := range testCases {
		flattened := flattenS3BucketVersioningConfiguration(&s3.GetBucketVersioningOutput{Status: tc.status})
		got := flattened[0].(map[string]interface{})["status"]
		if got != tc.expected {
			t.Errorf("expected status %q for %q, got %q", tc.expected, aws.StringValue(tc.status), got)
		}
	}
}

func s3VersioningTestErrorBody(code string) string {
	return `<Error><Code>` + code + `</Code><Message>test</Message><RequestId>1</RequestId></Error>`
}
