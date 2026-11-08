data "arianet_pricing" "fra" {
  datacenter = "eu-central-1"
}

# Monthly price of every plan in the datacenter.
output "plan_monthly_prices" {
  value = { for p in data.arianet_pricing.fra.server_types : p.name => p.prices[0].monthly_price }
}

output "volume_gb_monthly" {
  value = [for r in data.arianet_pricing.fra.resources : r.monthly_price if r.sku == "volume_gb"]
}
