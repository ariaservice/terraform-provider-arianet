resource "arianet_firewall" "web" {
  name = "web"

  rule {
    direction   = "in"
    protocol    = "tcp"
    port        = "22"
    source_ips  = ["203.0.113.0/24"]
    description = "SSH from the office"
  }

  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  apply_to = [
    { label_selector = "role=web" },
    { server = arianet_server.web.id },
  ]
}
