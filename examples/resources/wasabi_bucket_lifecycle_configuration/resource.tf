resource "wasabi_bucket" "example" {
  bucket = "example-bucket"

  # The bucket resource removes lifecycle rules that are not in its own
  # configuration, so leave them to wasabi_bucket_lifecycle_configuration.
  lifecycle {
    ignore_changes = [lifecycle_rule]
  }
}

resource "wasabi_bucket_versioning" "example" {
  bucket = wasabi_bucket.example.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "wasabi_bucket_lifecycle_configuration" "example" {
  bucket = wasabi_bucket.example.id

  rule {
    id     = "expire-logs"
    status = "Enabled"

    filter {
      prefix = "logs/"
    }

    expiration {
      days = 30
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

  depends_on = [wasabi_bucket_versioning.example]
}
