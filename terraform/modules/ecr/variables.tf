variable "project"     { type = string }
variable "environment" { type = string }
variable "name_prefix" { type = string }

variable "image_count" {
  description = "Maximum number of images to retain per repository. Older images are expired by the lifecycle policy."
  type        = number
  default     = 10
}
