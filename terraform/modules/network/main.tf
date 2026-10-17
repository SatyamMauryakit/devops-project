# modules/network/main.tf
#
# Creates:
#   - VPC
#   - Public subnets (one per AZ) with a shared Internet Gateway
#   - Private subnets (one per AZ) — EKS nodes run here
#   - NAT Gateway(s): one shared (single_nat_gateway = true) or one per AZ
#   - Route tables wiring everything together
#
# NAT Gateway cost note
# ---------------------
# A NAT Gateway costs ~$0.045/hour + $0.045/GB processed.
#   single_nat_gateway = true  ->  ~$32/month (one NAT GW, one EIP)
#   single_nat_gateway = false ->  ~$64/month (two NAT GWs, two EIPs)
#
# For production, use false so that a single-AZ NAT failure does not take
# down nodes in other AZs. For dev/demo, true is sufficient and halves cost.
# Destroy the NAT GW(s) with the EKS cluster when a demo is over:
#   terraform destroy -target=module.network

locals {
  # When single_nat_gateway is true, all private route tables point to one NAT GW.
  # When false, each private subnet gets its own NAT GW in the same AZ.
  nat_gateway_count = var.single_nat_gateway ? 1 : length(var.availability_zones)
}

# ---------------------------------------------------------------------------
# VPC
# ---------------------------------------------------------------------------
resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "${var.name_prefix}-vpc"
  }
}

# ---------------------------------------------------------------------------
# Internet Gateway (required for public subnets)
# ---------------------------------------------------------------------------
resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${var.name_prefix}-igw"
  }
}

# ---------------------------------------------------------------------------
# Public subnets
# ---------------------------------------------------------------------------
resource "aws_subnet" "public" {
  count             = length(var.availability_zones)
  vpc_id            = aws_vpc.main.id
  cidr_block        = var.public_subnet_cidrs[count.index]
  availability_zone = var.availability_zones[count.index]

  # Instances launched in public subnets receive a public IP automatically.
  # This is required for NAT GW to have an outbound path and for public
  # load balancers.
  map_public_ip_on_launch = true

  tags = {
    Name = "${var.name_prefix}-public-${var.availability_zones[count.index]}"
    # EKS uses these tags to discover subnets for public load balancers.
    "kubernetes.io/role/elb" = "1"
  }
}

# ---------------------------------------------------------------------------
# Private subnets
# ---------------------------------------------------------------------------
resource "aws_subnet" "private" {
  count             = length(var.availability_zones)
  vpc_id            = aws_vpc.main.id
  cidr_block        = var.private_subnet_cidrs[count.index]
  availability_zone = var.availability_zones[count.index]

  tags = {
    Name = "${var.name_prefix}-private-${var.availability_zones[count.index]}"
    # EKS uses this tag to discover subnets for internal load balancers.
    "kubernetes.io/role/internal-elb" = "1"
  }
}

# ---------------------------------------------------------------------------
# Elastic IPs for NAT Gateways
# ---------------------------------------------------------------------------
resource "aws_eip" "nat" {
  count  = local.nat_gateway_count
  domain = "vpc"

  tags = {
    Name = "${var.name_prefix}-nat-eip-${count.index}"
  }

  # The EIP must be created after the IGW is attached.
  depends_on = [aws_internet_gateway.main]
}

# ---------------------------------------------------------------------------
# NAT Gateways (in public subnets)
# ---------------------------------------------------------------------------
resource "aws_nat_gateway" "main" {
  count         = local.nat_gateway_count
  allocation_id = aws_eip.nat[count.index].id
  # Always place in a public subnet so it has an internet route.
  subnet_id     = aws_subnet.public[count.index].id

  tags = {
    Name = "${var.name_prefix}-nat-${count.index}"
  }

  depends_on = [aws_internet_gateway.main]
}

# ---------------------------------------------------------------------------
# Public route table
# ---------------------------------------------------------------------------
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = {
    Name = "${var.name_prefix}-rt-public"
  }
}

resource "aws_route_table_association" "public" {
  count          = length(var.availability_zones)
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# ---------------------------------------------------------------------------
# Private route tables (one per AZ when HA; one shared when single_nat_gateway)
# ---------------------------------------------------------------------------
resource "aws_route_table" "private" {
  count  = length(var.availability_zones)
  vpc_id = aws_vpc.main.id

  route {
    cidr_block     = "0.0.0.0/0"
    # When single_nat_gateway, all private tables use NAT GW index 0.
    # When multi-NAT, each table uses the NAT GW in the same AZ.
    nat_gateway_id = aws_nat_gateway.main[var.single_nat_gateway ? 0 : count.index].id
  }

  tags = {
    Name = "${var.name_prefix}-rt-private-${var.availability_zones[count.index]}"
  }
}

resource "aws_route_table_association" "private" {
  count          = length(var.availability_zones)
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[count.index].id
}
