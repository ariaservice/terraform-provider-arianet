resource "arianet_load_balancer_target" "web" {
  load_balancer_id = arianet_load_balancer.lb.id
  type             = "label_selector"
  label_selector   = "role=web"
  use_private_ip   = true
}

resource "arianet_load_balancer_target" "one" {
  load_balancer_id = arianet_load_balancer.lb.id
  type             = "server"
  server_id        = arianet_server.web.id
}
