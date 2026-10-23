resource "wasabi_bucket" "example" {
  bucket = "example-bucket"
}

resource "wasabi_bucket_versioning" "example" {
  bucket = wasabi_bucket.example.id

  versioning_configuration {
    status = "Enabled"
  }
}
