data "arianet_image" "ubuntu" {
  name       = "Ubuntu 24.04"
  datacenter = "eu-central-1"
}

data "arianet_image" "latest_snapshot" {
  type           = "snapshot"
  label_selector = "app=web"
  most_recent    = true
}
