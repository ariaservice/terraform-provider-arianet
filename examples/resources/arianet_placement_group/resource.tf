resource "arianet_placement_group" "spread" {
  name       = "web-spread"
  datacenter = "eu-central-1"
}

resource "arianet_server" "node" {
  count              = 3
  name               = "node-${count.index}"
  datacenter         = "eu-central-1"
  plan_id            = 1
  image              = "ubuntu-24.04"
  placement_group_id = arianet_placement_group.spread.id
}
