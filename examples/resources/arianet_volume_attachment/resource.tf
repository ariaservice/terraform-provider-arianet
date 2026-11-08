resource "arianet_volume_attachment" "data" {
  volume_id = arianet_volume.data.id
  server_id = arianet_server.web.id
}
