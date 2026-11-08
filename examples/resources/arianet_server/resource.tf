data "arianet_plans" "fra" {
  datacenter = "eu-central-1"
}

resource "arianet_ssh_key" "me" {
  name       = "laptop"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "arianet_server" "web" {
  name        = "web-1"
  datacenter  = "eu-central-1"
  plan_id     = data.arianet_plans.fra.plans[0].id
  image       = "ubuntu-24.04"
  ssh_key_ids = [arianet_ssh_key.me.id]
  backups     = true

  labels = {
    env  = "prod"
    role = "web"
  }

  user_data = <<-EOT
    #cloud-config
    packages: [nginx]
  EOT

  timeouts {
    create = "20m"
  }
}
