resource "arianet_load_balancer_service" "http" {
  load_balancer_id = arianet_load_balancer.lb.id
  protocol         = "http"
  listen_port      = 80
  destination_port = 8080

  health_check = {
    protocol = "http"
    port     = 8080
    interval = 10
    timeout  = 5
    retries  = 3
    http = {
      path         = "/healthz"
      status_codes = ["2??"]
    }
  }

  http = {
    sticky_sessions = true
  }
}
