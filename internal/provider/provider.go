// Package provider implements the arianet Terraform provider.
package provider

import (
	"context"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

// Environment variables the provider reads.
const (
	EnvToken    = "ARIANET_TOKEN"
	EnvEndpoint = "ARIANET_API_URL"
)

var _ provider.Provider = (*arianetProvider)(nil)

type arianetProvider struct {
	version string
}

type providerModel struct {
	Endpoint     types.String `tfsdk:"endpoint"`
	Token        types.String `tfsdk:"token"`
	PollInterval types.String `tfsdk:"poll_interval"`
	MaxRetries   types.Int64  `tfsdk:"max_retries"`
}

// providerData is what resources and data sources receive.
type providerData struct {
	client *client.Client
	locks  *keyedMutex
}

// New returns a provider factory for the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &arianetProvider{version: version}
	}
}

func (p *arianetProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "arianet"
	resp.Version = p.version
}

func (p *arianetProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The arianet provider manages arianet Cloud resources (servers, volumes, networks, firewalls, floating IPs, load balancers, placement groups) and account SSH keys through the arianet API.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Root URL of the arianet API gateway. Defaults to `" + client.DefaultEndpoint + "`, or the `" + EnvEndpoint + "` environment variable. Must be https (plain http is allowed only for localhost).",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "API token with the `cloud:servers:read`/`cloud:servers:write` scopes (and `ssh_keys` scopes for `arianet_ssh_key`). May be set with the `" + EnvToken + "` environment variable.",
			},
			"poll_interval": schema.StringAttribute{
				Optional:    true,
				Description: "How long to wait between polls of a running action, as a Go duration such as `2s` (default). The wait grows up to 10s.",
			},
			"max_retries": schema.Int64Attribute{
				Optional:    true,
				Description: "How many times a request is retried on HTTP 429 (honouring `Retry-After`) and a read on 502/503/504. Default 5.",
			},
		},
	}
}

func (p *arianetProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.Endpoint.IsUnknown() || cfg.Token.IsUnknown() {
		resp.Diagnostics.AddWarning("Provider configuration is not known yet",
			"endpoint or token depends on values known only after apply; the provider cannot be configured during this plan.")
		return
	}

	endpoint := os.Getenv(EnvEndpoint)
	if !cfg.Endpoint.IsNull() && cfg.Endpoint.ValueString() != "" {
		endpoint = cfg.Endpoint.ValueString()
	}
	token := os.Getenv(EnvToken)
	if !cfg.Token.IsNull() && cfg.Token.ValueString() != "" {
		token = cfg.Token.ValueString()
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API token",
			"Set the token argument of the arianet provider or the "+EnvToken+" environment variable.")
		return
	}

	var poll time.Duration
	if !cfg.PollInterval.IsNull() && !cfg.PollInterval.IsUnknown() && cfg.PollInterval.ValueString() != "" {
		d, err := time.ParseDuration(cfg.PollInterval.ValueString())
		if err != nil || d <= 0 {
			resp.Diagnostics.AddAttributeError(path.Root("poll_interval"), "Invalid poll_interval", "poll_interval must be a positive duration such as 2s.")
			return
		}
		poll = d
	}

	c, err := client.New(client.Config{
		Endpoint:     endpoint,
		Token:        token,
		UserAgent:    "terraform-provider-arianet/" + p.version,
		MaxRetries:   int(cfg.MaxRetries.ValueInt64()),
		PollInterval: poll,
	})
	if err != nil {
		resp.Diagnostics.AddError("Invalid arianet provider configuration", err.Error())
		return
	}
	data := &providerData{client: c, locks: newKeyedMutex()}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *arianetProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newServerResource,
		newVolumeResource,
		newVolumeAttachmentResource,
		newNetworkResource,
		newNetworkSubnetResource,
		newNetworkRouteResource,
		newFirewallResource,
		newFirewallAttachmentResource,
		newFloatingIPResource,
		newFloatingIPAssignmentResource,
		newSSHKeyResource,
		newPlacementGroupResource,
		newLoadBalancerResource,
		newLoadBalancerServiceResource,
		newLoadBalancerTargetResource,
	}
}

func (p *arianetProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newImagesDataSource,
		newImageDataSource,
		newPlansDataSource,
		newLocationsDataSource,
		newPricingDataSource,
		newServerDataSource,
		newNetworkDataSource,
		newSSHKeyDataSource,
		newLoadBalancerTypesDataSource,
	}
}
