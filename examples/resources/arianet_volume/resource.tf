resource "arianet_volume" "data" {
  name       = "data"
  datacenter = "eu-central-1"
  size       = 50
  filesystem = "ext4"
  labels     = { env = "prod" }
}
