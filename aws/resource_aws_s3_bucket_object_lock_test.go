package aws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func newObjectLockTestClient(t *testing.T, status int, body string) (*s3.S3, func()) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))

	sess := session.Must(session.NewSession(&aws.Config{
		Credentials:      credentials.NewStaticCredentials("AKIATEST", "secret", ""),
		Endpoint:         aws.String(server.URL),
		Region:           aws.String("us-east-1"),
		S3ForcePathStyle: aws.Bool(true),
		DisableSSL:       aws.Bool(true),
		MaxRetries:       aws.Int(0),
	}))

	return s3.New(sess), server.Close
}

func s3ErrorBody(code string) string {
	return `<Error><Code>` + code + `</Code><Message>test</Message><RequestId>1</RequestId></Error>`
}

func TestS3BucketObjectLockEnabled(t *testing.T) {
	testCases := []struct {
		name     string
		status   int
		body     string
		enabled  bool
		known    bool
		hasError bool
	}{
		{
			name:    "enabled",
			status:  http.StatusOK,
			body:    `<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled></ObjectLockConfiguration>`,
			enabled: true,
			known:   true,
		},
		{
			name:   "no configuration",
			status: http.StatusNotFound,
			body:   s3ErrorBody("ObjectLockConfigurationNotFoundError"),
			known:  true,
		},
		{name: "access denied", status: http.StatusForbidden, body: s3ErrorBody("AccessDenied")},
		{name: "method not allowed", status: http.StatusMethodNotAllowed, body: s3ErrorBody("MethodNotAllowed")},
		{name: "not implemented", status: http.StatusNotImplemented, body: s3ErrorBody("NotImplemented")},
		{name: "other error", status: http.StatusConflict, body: s3ErrorBody("OperationAborted"), hasError: true},
		{name: "no such bucket", status: http.StatusNotFound, body: s3ErrorBody("NoSuchBucket"), hasError: true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			conn, closeServer := newObjectLockTestClient(t, tc.status, tc.body)
			defer closeServer()

			enabled, known, err := s3BucketObjectLockEnabled(conn, "example", false)
			if (err != nil) != tc.hasError {
				t.Fatalf("unexpected error state: %v", err)
			}
			if enabled != tc.enabled || known != tc.known {
				t.Errorf("expected enabled=%t known=%t, got enabled=%t known=%t", tc.enabled, tc.known, enabled, known)
			}
		})
	}
}

// Buckets that already have Object Lock, or whose state was written before the
// argument existed, must not be planned for replacement when it is omitted.
func TestResourceAwsS3Bucket_objectLockEnabledDiff(t *testing.T) {
	testCases := []struct {
		name            string
		stateAttributes map[string]string
		config          map[string]interface{}
		requiresNew     bool
	}{
		{
			name:            "enabled in state, omitted in configuration",
			stateAttributes: map[string]string{"object_lock_enabled": "true"},
			config:          map[string]interface{}{},
		},
		{
			name:            "missing from state written by an older provider, omitted in configuration",
			stateAttributes: map[string]string{},
			config:          map[string]interface{}{},
		},
		{
			name:            "enabled in state and configuration",
			stateAttributes: map[string]string{"object_lock_enabled": "true"},
			config:          map[string]interface{}{"object_lock_enabled": true},
		},
		{
			name:            "enabled in state, disabled in configuration",
			stateAttributes: map[string]string{"object_lock_enabled": "true"},
			config:          map[string]interface{}{"object_lock_enabled": false},
			requiresNew:     true,
		},
		{
			name:            "disabled in state, enabled in configuration",
			stateAttributes: map[string]string{"object_lock_enabled": "false"},
			config:          map[string]interface{}{"object_lock_enabled": true},
			requiresNew:     true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			attributes := map[string]string{
				"id":            "example",
				"bucket":        "example",
				"acl":           "private",
				"force_destroy": "false",
			}
			for k, v := range tc.stateAttributes {
				attributes[k] = v
			}
			config := map[string]interface{}{"bucket": "example"}
			for k, v := range tc.config {
				config[k] = v
			}

			diff, err := resourceAwsS3Bucket().Diff(
				context.Background(),
				&terraform.InstanceState{ID: "example", Attributes: attributes},
				terraform.NewResourceConfigRaw(config),
				nil,
			)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if tc.requiresNew {
				if diff == nil || !diff.RequiresNew() {
					t.Fatalf("expected the bucket to be replaced, got %#v", diff)
				}
				return
			}
			if diff != nil && (diff.RequiresNew() || diff.Attributes["object_lock_enabled"] != nil) {
				t.Fatalf("expected no object_lock_enabled change, got %#v", diff)
			}
		})
	}
}
