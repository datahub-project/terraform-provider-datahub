# The CloudFormation template takes one existing subnet and assigns the task
# no public IP, so the task needs a private subnet with a route to a NAT
# gateway: it pulls its image from ECR and reaches GMS and PyPI over the
# internet. One availability zone is the honest minimum for a single-subnet
# template; the public subnet exists only to host the NAT gateway.
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 6.7"

  name = local.stack_name
  cidr = "10.0.0.0/16"

  azs             = [data.aws_availability_zones.available.names[0]]
  private_subnets = ["10.0.1.0/24"]
  public_subnets  = ["10.0.101.0/24"]

  enable_nat_gateway = true
  single_nat_gateway = true
}
