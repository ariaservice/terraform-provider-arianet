package provider

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Acceptance tests run against the real API and create billed resources.
// They run only with TF_ACC=1 and need:
//
//	ARIANET_TOKEN            API token (cloud:servers:read/write, ssh_keys read/write)
//	ARIANET_API_URL          optional, defaults to https://api.ariaservice.net
//	ARIANET_ACC_DATACENTER   optional, defaults to eu-central-1
//	ARIANET_ACC_PLAN_ID      plan for the server test (skipped when unset)
//	ARIANET_ACC_IMAGE        image for the server test, defaults to ubuntu-24.04

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv(EnvToken) == "" {
		t.Fatal(EnvToken + " must be set for acceptance tests")
	}
}

func accDatacenter() string {
	if v := os.Getenv("ARIANET_ACC_DATACENTER"); v != "" {
		return v
	}
	return "eu-central-1"
}

func accName(prefix string) string {
	return fmt.Sprintf("tfacc-%s-%d", prefix, time.Now().UnixNano()%1_000_000)
}

func TestAccDataSources(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
data "arianet_locations" "all" {}
data "arianet_images" "system" {
  type       = "system"
  datacenter = %[1]q
}
data "arianet_plans" "dc" {
  datacenter = %[1]q
}
data "arianet_pricing" "dc" {
  datacenter = %[1]q
}
`, accDatacenter()),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("data.arianet_locations.all", "locations.0.code"),
				resource.TestCheckResourceAttrSet("data.arianet_images.system", "images.0.image"),
				resource.TestCheckResourceAttrSet("data.arianet_plans.dc", "plans.0.id"),
				resource.TestCheckResourceAttrSet("data.arianet_pricing.dc", "raw_json"),
			),
		}},
	})
}

func TestAccSSHKey(t *testing.T) {
	name := accName("key")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "arianet_ssh_key" "k" {
  name       = %q
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGq1n1mXpzmYxTm1q0mCw3o6ZbJ5nJm8Q0kq3dYw1x2Q tfacc"
}
`, name),
				Check: resource.TestCheckResourceAttrSet("arianet_ssh_key.k", "fingerprint"),
			},
			{ResourceName: "arianet_ssh_key.k", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"public_key"}},
		},
	})
}

func TestAccNetworkFirewall(t *testing.T) {
	name := accName("net")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "arianet_network" "n" {
  name       = %[1]q
  datacenter = %[2]q
  ip_range   = "10.77.0.0/16"
  subnet_ip_range = "10.77.1.0/24"
  labels     = { tfacc = "true" }
}
resource "arianet_network_subnet" "s" {
  network_id = arianet_network.n.id
  ip_range   = "10.77.2.0/24"
}
resource "arianet_network_route" "r" {
  network_id  = arianet_network.n.id
  destination = "10.99.0.0/24"
  gateway     = "10.77.1.10"
  depends_on  = [arianet_network_subnet.s]
}
resource "arianet_firewall" "fw" {
  name = %[1]q
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = ["0.0.0.0/0"]
  }
  apply_to = [{ label_selector = "tfacc=%[1]s" }]
}
`, name, accDatacenter()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("arianet_network.n", "subnets.#", "1"),
					resource.TestCheckResourceAttrSet("arianet_network_subnet.s", "subnet_id"),
					resource.TestCheckResourceAttr("arianet_firewall.fw", "apply_to.#", "1"),
				),
			},
			{ResourceName: "arianet_network.n", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts", "subnets", "routes", "status"}},
			{ResourceName: "arianet_firewall.fw", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
		},
	})
}

func TestAccServerVolume(t *testing.T) {
	plan := os.Getenv("ARIANET_ACC_PLAN_ID")
	if os.Getenv("TF_ACC") != "" && plan == "" {
		t.Skip("ARIANET_ACC_PLAN_ID is not set; skipping the billed server test")
	}
	image := os.Getenv("ARIANET_ACC_IMAGE")
	if image == "" {
		image = "ubuntu-24.04"
	}
	name := accName("srv")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "arianet_server" "s" {
  name       = %[1]q
  datacenter = %[2]q
  plan_id    = %[3]s
  image      = %[4]q
  labels     = { tfacc = "true" }
}
resource "arianet_volume" "v" {
  name       = %[1]q
  datacenter = %[2]q
  size       = 10
}
resource "arianet_volume_attachment" "a" {
  volume_id = arianet_volume.v.id
  server_id = arianet_server.s.id
}
`, name, accDatacenter(), plan, image),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("arianet_server.s", "ipv4_address"),
					resource.TestCheckResourceAttrPair("arianet_volume_attachment.a", "server_id", "arianet_server.s", "id"),
				),
			},
			{
				ResourceName:            "arianet_server.s",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"root_password", "start_after_create", "keep_backups", "delete_snapshots", "timeouts", "status"},
			},
		},
	})
}
