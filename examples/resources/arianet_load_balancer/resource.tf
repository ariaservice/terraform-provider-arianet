resource "arianet_load_balancer" "lb" {
  name       = "web"
  datacenter = "eu-central-1"
  type       = "lb11"
  algorithm  = "round_robin"
  network_id = arianet_network.private.id
}
