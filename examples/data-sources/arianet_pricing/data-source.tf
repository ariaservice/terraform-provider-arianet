data "arianet_pricing" "fra" {
  datacenter = "eu-central-1"
}

output "volume_gb_monthly" {
  value = [for p in data.arianet_pricing.fra.prices : p.monthly_price if p.sku == "volume_gb"]
}

output "raw" {
  value = jsondecode(data.arianet_pricing.fra.raw_json)
}
