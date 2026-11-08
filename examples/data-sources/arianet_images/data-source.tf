data "arianet_images" "system" {
  type       = "system"
  datacenter = "eu-central-1"
}

output "image_slugs" {
  value = [for i in data.arianet_images.system.images : i.image]
}
