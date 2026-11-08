terraform {
  required_providers {
    arianet = {
      source  = "ariaservice/arianet"
      version = "~> 0.1"
    }
  }
}

# The token can also come from ARIANET_TOKEN, the endpoint from ARIANET_API_URL.
provider "arianet" {
  token = var.arianet_token
}

variable "arianet_token" {
  type      = string
  sensitive = true
}
