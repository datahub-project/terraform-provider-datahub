# Tags are flat labels applied to data assets. This resource manages the tag
# itself; applying it to datasets or columns is per-asset enrichment and is out
# of scope for this provider.
resource "datahub_tag" "pii" {
  tag_id      = "tf-example-pii"
  name        = "TF Example - PII"
  description = "Data asset contains personally identifiable information and requires special handling."
  color_hex   = "#E74C3C"
}
