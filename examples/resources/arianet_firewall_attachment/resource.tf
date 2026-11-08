resource "arianet_firewall_attachment" "web" {
  firewall_id     = arianet_firewall.web.id
  server_ids      = [arianet_server.web.id]
  label_selectors = ["env=prod"]
}
