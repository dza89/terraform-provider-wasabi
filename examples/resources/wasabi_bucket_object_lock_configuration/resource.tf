resource "wasabi_bucket" "example" {
  bucket = "example-bucket"

  # Object Lock can only be enabled when the bucket is created.
  object_lock_enabled = true
}

resource "wasabi_bucket_object_lock_configuration" "example" {
  bucket = wasabi_bucket.example.id

  rule {
    default_retention {
      mode = "GOVERNANCE"
      days = 30
    }
  }
}
