resource "arianet_network" "private" {
  name            = "private"
  datacenter      = "eu-central-1"
  ip_range        = "10.0.0.0/16"
  subnet_ip_range = "10.0.1.0/24"
  labels          = { env = "prod" }
}
