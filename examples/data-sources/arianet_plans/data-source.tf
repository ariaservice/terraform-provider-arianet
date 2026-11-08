data "arianet_plans" "fra" {
  datacenter = "eu-central-1"
}

locals {
  small = [for p in data.arianet_plans.fra.plans : p if p.vcpu == 2 && p.available][0]
}
