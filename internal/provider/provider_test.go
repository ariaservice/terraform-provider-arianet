package provider

import (
	"context"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testProtoV6ProviderFactories serves the provider in-process for tests.
var testProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"arianet": providerserver.NewProtocol6WithError(New("test")()),
}

// TestProviderSchemasAreValid checks every schema the way Terraform will.
func TestProviderSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	var presp provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &presp)
	if presp.Diagnostics.HasError() {
		t.Fatalf("provider schema: %v", presp.Diagnostics)
	}
	if d := presp.Schema.ValidateImplementation(ctx); d.HasError() {
		t.Fatalf("provider schema: %v", d)
	}

	seen := map[string]bool{}
	for _, f := range p.Resources(ctx) {
		r := f()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "arianet"}, &meta)
		if seen[meta.TypeName] {
			t.Errorf("duplicate resource %s", meta.TypeName)
		}
		seen[meta.TypeName] = true
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s: %v", meta.TypeName, resp.Diagnostics)
			continue
		}
		if d := resp.Schema.ValidateImplementation(ctx); d.HasError() {
			t.Errorf("%s: %v", meta.TypeName, d)
		}
		if _, ok := r.(resource.ResourceWithImportState); !ok {
			t.Errorf("%s does not support import", meta.TypeName)
		}
		if resp.Schema.Description == "" {
			t.Errorf("%s has no description", meta.TypeName)
		}
	}
	for _, f := range p.DataSources(ctx) {
		d := f()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "arianet"}, &meta)
		if seen["ds:"+meta.TypeName] {
			t.Errorf("duplicate data source %s", meta.TypeName)
		}
		seen["ds:"+meta.TypeName] = true
		var resp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s: %v", meta.TypeName, resp.Diagnostics)
			continue
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: %v", meta.TypeName, diags)
		}
	}
}

func TestProviderRefusesPlainHTTP(t *testing.T) {
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{{
			Config: `
provider "arianet" {
  endpoint = "http://api.example.com"
  token    = "x"
}
data "arianet_locations" "all" {}
`,
			ExpectError: regexp.MustCompile(`plain http`),
		}},
	})
}

func TestProviderNeedsToken(t *testing.T) {
	t.Setenv(EnvToken, "")
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{{
			Config: `
provider "arianet" {
  endpoint = "https://api.example.com"
}
data "arianet_locations" "all" {}
`,
			ExpectError: regexp.MustCompile(`Missing API token`),
		}},
	})
}

func TestUnitDataSources(t *testing.T) {
	m := newMockAPI(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{{
			Config: m.providerConfig() + `
data "arianet_locations" "all" {}
data "arianet_images" "system" {
  type       = "system"
  datacenter = "eu-central-1"
}
data "arianet_image" "ubuntu" {
  name = "Ubuntu 24.04"
}
data "arianet_plans" "fra" {
  datacenter = "eu-central-1"
}
data "arianet_pricing" "all" {}
data "arianet_pricing" "fra" {
  datacenter = "eu-central-1"
}
data "arianet_load_balancer_types" "all" {}
`,
			Check: tfresource.ComposeAggregateTestCheckFunc(
				tfresource.TestCheckResourceAttr("data.arianet_locations.all", "locations.#", "2"),
				tfresource.TestCheckResourceAttr("data.arianet_locations.all", "codes.0", "eu-central-1"),
				tfresource.TestCheckResourceAttr("data.arianet_images.system", "images.#", "2"),
				tfresource.TestCheckResourceAttr("data.arianet_image.ubuntu", "image", "ubuntu-24.04"),
				tfresource.TestCheckResourceAttr("data.arianet_image.ubuntu", "datacenters.#", "2"),
				tfresource.TestCheckResourceAttr("data.arianet_plans.fra", "plans.#", "2"),
				tfresource.TestCheckNoResourceAttr("data.arianet_plans.fra", "plans.0.traffic_gb"),
				tfresource.TestCheckResourceAttr("data.arianet_plans.fra", "plans.1.traffic_gb", "20000"),
				tfresource.TestCheckResourceAttr("data.arianet_plans.fra", "plans.1.available", "false"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.all", "hours_per_month", "730"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.all", "vat_included", "false"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.all", "server_types.#", "2"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.all", "server_types.0.prices.#", "2"),
				tfresource.TestCheckNoResourceAttr("data.arianet_pricing.all", "server_types.0.included_traffic_gb"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.all", "server_types.1.included_traffic_gb", "20000"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.all", "resources.#", "3"),
				tfresource.TestCheckResourceAttrSet("data.arianet_pricing.all", "raw_json"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.fra", "server_types.#", "1"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.fra", "server_types.0.prices.0.monthly_price", "7300"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.fra", "server_types.0.prices.0.currency", "IRR"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.fra", "resources.#", "2"),
				tfresource.TestCheckResourceAttr("data.arianet_pricing.fra", "resources.1.percent", "20"),
				tfresource.TestCheckNoResourceAttr("data.arianet_pricing.fra", "resources.0.percent"),
				tfresource.TestCheckResourceAttr("data.arianet_load_balancer_types.all", "load_balancer_types.0.code", "lb11"),
			),
		}},
	})
}

func TestUnitSSHKey(t *testing.T) {
	m := newMockAPI(t)
	cfg := func(name string) string {
		return m.providerConfig() + `
resource "arianet_ssh_key" "k" {
  name       = "` + name + `"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIK0 me@laptop\n"
}
data "arianet_ssh_key" "byname" {
  name       = arianet_ssh_key.k.name
}
`
	}
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if len(m.sshKeys) != 0 {
				t.Errorf("ssh keys left: %d", len(m.sshKeys))
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: cfg("laptop"),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("arianet_ssh_key.k", "fingerprint", "SHA256:mock"),
					tfresource.TestCheckResourceAttr("arianet_ssh_key.k", "is_default", "false"),
					tfresource.TestCheckResourceAttrPair("data.arianet_ssh_key.byname", "id", "arianet_ssh_key.k", "id"),
				),
			},
			{
				Config: cfg("desktop"),
				Check:  tfresource.TestCheckResourceAttr("arianet_ssh_key.k", "name", "desktop"),
			},
			{
				ResourceName:            "arianet_ssh_key.k",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_key"},
			},
		},
	})
}

func TestUnitServerLifecycle(t *testing.T) {
	m := newMockAPI(t)
	cfg := func(name, labels string, plan int, backups bool) string {
		b := "false"
		if backups {
			b = "true"
		}
		return m.providerConfig() + `
resource "arianet_firewall" "web" {
  name = "web"
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = ["0.0.0.0/0"]
  }
}
resource "arianet_server" "s" {
  name         = "` + name + `"
  datacenter   = "eu-central-1"
  plan_id      = ` + strconv.Itoa(plan) + `
  image        = "ubuntu-24.04"
  ssh_keys     = ["ssh-ed25519 AAAA me"]
  firewall_ids = [arianet_firewall.web.id]
  backups      = ` + b + `
  keep_backups = true
  labels       = { ` + labels + ` }
}
`
	}
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if len(m.servers) != 0 || len(m.firewalls) != 0 {
				t.Errorf("left behind: %d servers, %d firewalls", len(m.servers), len(m.firewalls))
			}
			for _, q := range m.deletes {
				if q != "keep_backups=true" {
					t.Errorf("DELETE /servers query = %q, want keep_backups=true", q)
				}
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: cfg("web-1", `env = "prod"`, 1, false) + `
data "arianet_server_costs" "s" {
  server_id = arianet_server.s.id
}
`,
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("arianet_server.s", "status", "running"),
					tfresource.TestCheckResourceAttr("arianet_server.s", "labels.env", "prod"),
					tfresource.TestCheckResourceAttr("arianet_server.s", "root_password", "s3cret"),
					tfresource.TestCheckResourceAttr("arianet_server.s", "firewall_ids.#", "1"),
					tfresource.TestCheckResourceAttr("arianet_server.s", "backups", "false"),
					tfresource.TestCheckResourceAttrSet("arianet_server.s", "ipv4_address"),
					tfresource.TestCheckResourceAttr("data.arianet_server_costs.s", "lines.#", "2"),
					tfresource.TestCheckResourceAttr("data.arianet_server_costs.s", "totals.0.monthly_price", "8030"),
					tfresource.TestCheckResourceAttr("data.arianet_server_costs.s", "snapshots.size_gb", "12.5"),
					tfresource.TestCheckResourceAttr("data.arianet_server_costs.s", "hours_per_month", "730"),
				),
			},
			{
				Config: cfg("web-2", `env = "staging", team = "a"`, 2, true),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("arianet_server.s", "name", "web-2"),
					tfresource.TestCheckResourceAttr("arianet_server.s", "plan_id", "2"),
					tfresource.TestCheckResourceAttr("arianet_server.s", "labels.%", "2"),
					tfresource.TestCheckResourceAttr("arianet_server.s", "backups", "true"),
					// Refreshed before this step: the firewall now lists the server.
					tfresource.TestCheckResourceAttr("arianet_firewall.web", "server_ids.#", "1"),
				),
			},
			{
				ResourceName:      "arianet_server.s",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"ssh_keys", "root_password", "start_after_create", "keep_backups", "delete_snapshots", "timeouts",
				},
			},
			{
				// A server deleted outside Terraform is removed from state and planned again.
				PreConfig: func() {
					m.mu.Lock()
					for id := range m.servers {
						delete(m.servers, id)
					}
					m.mu.Unlock()
				},
				Config:             cfg("web-2", `env = "staging", team = "a"`, 2, true),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestUnitNetworkSubnetRoute(t *testing.T) {
	m := newMockAPI(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: m.providerConfig() + `
resource "arianet_network" "n" {
  name            = "private"
  datacenter      = "eu-central-1"
  ip_range        = "10.0.0.0/16"
  subnet_ip_range = "10.0.1.0/24"
}
resource "arianet_network_subnet" "extra" {
  network_id = arianet_network.n.id
  ip_range   = "10.0.9.0/24"
}
resource "arianet_network_route" "a" {
  network_id  = arianet_network.n.id
  destination = "10.100.0.0/24"
  gateway     = "10.0.1.10"
}
resource "arianet_network_route" "b" {
  network_id  = arianet_network.n.id
  destination = "10.200.0.0/24"
  gateway     = "10.0.1.11"
}
`,
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("arianet_network_subnet.extra", "gateway", "10.0.9.1"),
					func(*terraform.State) error {
						m.mu.Lock()
						defer m.mu.Unlock()
						for _, n := range m.networks {
							if len(n.Routes) != 2 || len(n.Subnets) != 2 {
								t.Errorf("network has %d routes, %d subnets", len(n.Routes), len(n.Subnets))
							}
						}
						return nil
					},
				),
			},
			{
				ResourceName:            "arianet_network_route.a",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				ResourceName:            "arianet_network.n",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "routes", "subnets"},
			},
		},
	})
}

func TestUnitFirewallSelectors(t *testing.T) {
	m := newMockAPI(t)
	cfg := func(port, selector string) string {
		return m.providerConfig() + `
resource "arianet_firewall" "fw" {
  name   = "edge"
  labels = { tier = "edge" }
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "` + port + `"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  rule {
    direction = "in"
    protocol  = "icmp"
  }
  apply_to = [{ label_selector = "` + selector + `" }]
}
`
	}
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: cfg("443", "env=prod"),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("arianet_firewall.fw", "rule.#", "2"),
					tfresource.TestCheckResourceAttr("arianet_firewall.fw", "apply_to.#", "1"),
					tfresource.TestCheckResourceAttr("arianet_firewall.fw", "apply_to.0.label_selector", "env=prod"),
				),
			},
			{
				Config: cfg("8443", "env=staging"),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("arianet_firewall.fw", "rule.0.port", "8443"),
					tfresource.TestCheckResourceAttr("arianet_firewall.fw", "apply_to.0.label_selector", "env=staging"),
				),
			},
			{
				ResourceName:            "arianet_firewall.fw",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}
