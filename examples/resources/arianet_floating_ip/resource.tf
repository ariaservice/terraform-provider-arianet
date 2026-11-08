resource "arianet_floating_ip" "public" {
  datacenter = "eu-central-1"
  type       = "ipv4"
  name       = "public"
  dns_ptr    = "www.example.com"
}
