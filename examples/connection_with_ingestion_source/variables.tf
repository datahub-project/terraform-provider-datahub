variable "postgres_host" {
  description = "Postgres hostname or IP (private network, not public internet)."
  type        = string
}

variable "postgres_db" {
  description = "Postgres database name."
  type        = string
}

variable "postgres_user" {
  description = "Postgres username."
  type        = string
}

variable "postgres_password" {
  description = "Postgres password."
  type        = string
  sensitive   = true
}
