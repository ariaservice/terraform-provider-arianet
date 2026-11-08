resource "arianet_network_subnet" "db" {
  network_id = arianet_network.private.id
  ip_range   = "10.0.2.0/24"
}
