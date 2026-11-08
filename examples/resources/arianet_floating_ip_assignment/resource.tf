resource "arianet_floating_ip_assignment" "public" {
  floating_ip_id = arianet_floating_ip.public.id
  server_id      = arianet_server.web.id
}
