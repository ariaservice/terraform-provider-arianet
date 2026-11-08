data "arianet_locations" "all" {}

output "datacenters" {
  value = data.arianet_locations.all.codes
}
