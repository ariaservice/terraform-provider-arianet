# Terraform Provider for arianet

`ariaservice/arianet` manages arianet Cloud resources through the arianet API:
servers, volumes, private networks (subnets, routes), firewalls, floating IPs,
load balancers (services, targets) and the account's SSH keys.

Built with [terraform-plugin-framework](https://github.com/hashicorp/terraform-plugin-framework), protocol 6 (Terraform >= 1.0).

## Usage

```hcl
terraform {
  required_providers {
    arianet = {
      source  = "ariaservice/arianet"
      version = "~> 0.1"
    }
  }
}

provider "arianet" {}  # reads ARIANET_TOKEN (and optionally ARIANET_API_URL)

resource "arianet_ssh_key" "me" {
  name       = "laptop"
  public_key = file("~/.ssh/id_ed25519.pub")
}

data "arianet_plans" "fra" {
  datacenter = "eu-central-1"
}

resource "arianet_server" "web" {
  name        = "web-1"
  datacenter  = "eu-central-1"
  plan_id     = data.arianet_plans.fra.plans[0].id
  image       = "ubuntu-24.04"
  ssh_key_ids = [arianet_ssh_key.me.id]
  labels      = { role = "web" }
}

resource "arianet_firewall" "web" {
  name = "web"
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  apply_to = [{ label_selector = "role=web" }]
}
```

### Provider arguments

| Argument        | Env               | Default                       |
|-----------------|-------------------|-------------------------------|
| `endpoint`      | `ARIANET_API_URL` | `https://api.ariaservice.net` |
| `token`         | `ARIANET_TOKEN`   | — (required)                  |
| `poll_interval` |                   | `2s` (grows to 10s)           |
| `max_retries`   |                   | `5`                           |

The token is a personal API token with the `cloud:servers:read` and
`cloud:servers:write` scopes (plus the SSH-key scopes for `arianet_ssh_key`).
Only `https` endpoints are accepted, except `http://localhost` / loopback for
development.

### How it talks to the API

* Cloud resources: `<endpoint>/cloud/v1/...` (the gateway proxies to
  cloud-service). SSH keys: `<endpoint>/api/v1/ssh-keys`.
* Long operations return an action; the provider polls `GET /cloud/v1/actions/{id}`
  until `success` or `error`, bounded by each resource's `timeouts` block.
* HTTP 429 is retried honouring `Retry-After` (or `RateLimit-Reset` when that is the only hint); reads are also retried on
  502/503/504. Redirects to another host are refused.
* A resource that answers 404 on refresh is removed from the state.

Full documentation per resource and data source is in [`docs/`](docs/) and,
once published, on the Terraform Registry. Every resource supports
`terraform import` (see the Import section of each page).

## Resources and data sources

| Resource | API |
|---|---|
| `arianet_server` | `/servers`, actions `change_plan`, `enable_backup`/`disable_backup`; firewalls via `apply_to_resources` |
| `arianet_volume`, `arianet_volume_attachment` | `/volumes`, actions `attach`/`detach`/`resize` |
| `arianet_network`, `arianet_network_subnet`, `arianet_network_route` | `/networks`, `/networks/{id}/subnets`, `PUT /networks/{id}/routes` |
| `arianet_firewall`, `arianet_firewall_attachment` | `/firewalls`, actions `apply_to_resources`/`remove_from_resources` |
| `arianet_floating_ip`, `arianet_floating_ip_assignment` | `/floating-ips`, actions `assign`/`unassign`/`change_dns_ptr` |
| `arianet_load_balancer`, `arianet_load_balancer_service`, `arianet_load_balancer_target` | `/load-balancers`, actions `add_service`/`update_service`/`delete_service`, `add_target`/`remove_target`, `change_type`, `change_algorithm`, `attach_to_network`/`detach_from_network` |
| `arianet_ssh_key` | arianet-api `/api/v1/ssh-keys` |

Data sources: `arianet_images`, `arianet_image`, `arianet_plans`,
`arianet_locations`, `arianet_pricing` (public price list, `/cloud/v1/public/pricing`),
`arianet_server_costs` (`/servers/{id}/costs`), `arianet_load_balancer_types`,
`arianet_server`, `arianet_network`, `arianet_ssh_key`.

## Development

Requirements: Go (see `go.mod`), Terraform >= 1.0.

```sh
make build      # go build
make test       # unit tests (client + provider against an in-process mock API; needs terraform on PATH)
make testacc    # acceptance tests against the real API: TF_ACC=1, needs ARIANET_TOKEN; creates billed resources
make docs       # regenerate docs/ from the schema, examples/ and templates
make install    # install into ~/.terraform.d/plugins for a filesystem mirror
```

Acceptance-test knobs: `ARIANET_ACC_DATACENTER` (default `eu-central-1`),
`ARIANET_ACC_PLAN_ID` (server test is skipped without it), `ARIANET_ACC_IMAGE`.

### Using a local build (`dev_overrides`)

Build the binary, then point Terraform at it in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "ariaservice/arianet" = "/path/to/terraform-provider-arianet"  # the directory holding the binary
  }
  direct {}
}
```

With `dev_overrides` skip `terraform init` and run `plan`/`apply` directly;
Terraform prints a warning that overrides are in effect. Debugging: run the
binary with `-debug` and export the printed `TF_REATTACH_PROVIDERS`.

## Publishing to the Terraform Registry

The release pipeline is ready (`.goreleaser.yml`, `.github/workflows/release.yml`,
`terraform-registry-manifest.json`, protocol 6.0). One-time manual steps:

1. **Repository**: the GitHub repository must be named
   `terraform-provider-arianet`, live under the `ariaservice` organisation or
   user, and be **public** (the registry only reads public repositories).
2. **GPG key**: create a signing key (RSA, no expiry or a long one):
   `gpg --full-generate-key`, then export it:
   `gpg --armor --export <KEY_ID>` (public) and
   `gpg --armor --export-secret-keys <KEY_ID>` (private).
3. **Registry signing key**: sign in to <https://registry.terraform.io> with the
   GitHub account that owns the repository, go to **User Settings → Signing
   Keys**, add the ASCII-armored **public** key for the `ariaservice` namespace.
4. **GitHub secrets** (repository → Settings → Secrets and variables → Actions):
   `GPG_PRIVATE_KEY` = the ASCII-armored private key, `PASSPHRASE` = its passphrase.
5. **Tag a release**: `git tag v0.1.0 && git push origin v0.1.0`. The Release
   workflow builds every platform, writes the zip archives
   (`terraform-provider-arianet_0.1.0_<os>_<arch>.zip`), the `_SHA256SUMS` file,
   its detached signature `_SHA256SUMS.sig` and the `_manifest.json`, and
   creates a **draft** GitHub release. Review it and click **Publish release**.
6. **Publish the provider** (first time only): on registry.terraform.io choose
   **Publish → Provider**, pick the `ariaservice/terraform-provider-arianet`
   repository and confirm. The registry installs a webhook; later tags are picked
   up automatically once their GitHub release is published.
7. Check <https://registry.terraform.io/providers/ariaservice/arianet> shows the
   version and the documentation from `docs/`.

## License

[MPL-2.0](LICENSE)
