resource "arianet_ssh_key" "me" {
  name       = "laptop"
  public_key = file("~/.ssh/id_ed25519.pub")
}
