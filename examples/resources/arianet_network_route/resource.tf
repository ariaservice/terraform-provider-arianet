resource "arianet_network_route" "vpn" {
  network_id  = arianet_network.private.id
  destination = "10.100.0.0/24"
  gateway     = "10.0.1.10"
}
