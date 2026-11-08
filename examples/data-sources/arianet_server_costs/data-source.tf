data "arianet_server_costs" "web" {
  server_id = arianet_server.web.id
}

output "web_monthly_cost" {
  value = data.arianet_server_costs.web.totals
}
