variable "project"     { type = string }
variable "environment" { type = string }
variable "name_prefix" { type = string }

variable "vpc_cidr" {
  description = "CIDR block for the VPC."
  type        = string
}

variable "availability_zones" {
  description = "List of AZs to spread subnets across. Must have at least two entries."
  type        = list(string)
}

variable "public_subnet_cidrs" {
  description = "CIDR blocks for public subnets, one per AZ."
  type        = list(string)
}

variable "private_subnet_cidrs" {
  description = "CIDR blocks for private subnets, one per AZ."
  type        = list(string)
}

variable "single_nat_gateway" {
  description = "When true, provision one shared NAT GW (~$32/mo). When false, one per AZ (~$64/mo)."
  type        = bool
  default     = true
}
