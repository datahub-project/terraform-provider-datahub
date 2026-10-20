# A tiny bucket with one seeded CSV object. This is what the ingestion source
# actually ingests, making the example runnable end to end: the s3 source lists
# the bucket, infers the CSV schema, and emits a dataset.
resource "aws_s3_bucket" "data" {
  bucket = local.bucket_name
  # Remove any object left behind (none are expected) so terraform destroy
  # never fails on a non-empty bucket.
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "data" {
  bucket                  = aws_s3_bucket.data.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_object" "customers_csv" {
  bucket       = aws_s3_bucket.data.id
  key          = "customers.csv"
  content_type = "text/csv"

  content = <<-CSV
    id,name,region,signup_date
    1,Amara Okafor,emea,2025-11-03
    2,Jin Park,apac,2025-12-14
    3,Lucia Alvarez,amer,2026-01-22
    4,Noah Williams,amer,2026-02-05
    5,Priya Sharma,apac,2026-03-18
  CSV
}

# The identity the ingestion recipe authenticates as: a read-only IAM user whose
# access key is held in Secrets Manager and file-mounted into the worker pods.
# This is the AWS counterpart of the Azure example's storage account key -- a
# static credential that exists so the example can demonstrate the file-mounted
# secret path. A worker that only ever reads S3 could use its pod identity
# instead and need no key at all.
resource "aws_iam_user" "ingest" {
  name = "tf-example-aws-ingest-${random_string.suffix.result}"
}

resource "aws_iam_user_policy" "ingest_read_bucket" {
  name = "read-seeded-bucket"
  user = aws_iam_user.ingest.name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:ListBucket", "s3:GetBucketLocation"]
        Resource = aws_s3_bucket.data.arn
      },
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject"]
        Resource = "${aws_s3_bucket.data.arn}/*"
      },
    ]
  })
}

resource "aws_iam_access_key" "ingest" {
  user = aws_iam_user.ingest.name
}
