package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

// pickOne narrows candidates to exactly one, or explains why not.
func pickOne[T any](diags *diagnosticsAlias, kind string, items []T) (T, bool) {
	var zero T
	switch len(items) {
	case 1:
		return items[0], true
	case 0:
		diags.AddError("No "+kind+" found", "No "+kind+" matches the given arguments.")
	default:
		diags.AddError("Several "+kind+"s found", fmt.Sprintf("%d %ss match; narrow the lookup.", len(items), kind))
	}
	return zero, false
}

// ---------------------------------------------------------------- server

type serverDataSource struct{ dsBase }

func newServerDataSource() datasource.DataSource { return &serverDataSource{} }

type serverDataModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	LabelSelector    types.String `tfsdk:"label_selector"`
	Datacenter       types.String `tfsdk:"datacenter"`
	PlanID           types.Int64  `tfsdk:"plan_id"`
	Image            types.String `tfsdk:"image"`
	Status           types.String `tfsdk:"status"`
	IPv4             types.String `tfsdk:"ipv4_address"`
	IPv6             types.String `tfsdk:"ipv6_address"`
	DiskGB           types.Int64  `tfsdk:"disk_gb"`
	Labels           types.Map    `tfsdk:"labels"`
	ProtectionDelete types.Bool   `tfsdk:"protection_delete"`
	ProjectID        types.Int64  `tfsdk:"project_id"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (d *serverDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (d *serverDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "One existing server, by `id`, `name` or `label_selector`.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Optional: true, Computed: true, Description: "Server ID."},
			"name":              schema.StringAttribute{Optional: true, Computed: true, Description: "Server name."},
			"label_selector":    schema.StringAttribute{Optional: true, Description: "Label selector that must match exactly one server."},
			"datacenter":        schema.StringAttribute{Computed: true, Description: "Datacenter."},
			"plan_id":           schema.Int64Attribute{Computed: true, Description: "Plan ID."},
			"image":             schema.StringAttribute{Computed: true, Description: "Image it was created from."},
			"status":            schema.StringAttribute{Computed: true, Description: "Status."},
			"ipv4_address":      schema.StringAttribute{Computed: true, Description: "Public IPv4."},
			"ipv6_address":      schema.StringAttribute{Computed: true, Description: "Public IPv6."},
			"disk_gb":           schema.Int64Attribute{Computed: true, Description: "Disk size."},
			"labels":            schema.MapAttribute{ElementType: types.StringType, Computed: true, Description: "Labels."},
			"protection_delete": schema.BoolAttribute{Computed: true, Description: "Delete protection."},
			"project_id":        schema.Int64Attribute{Computed: true, Description: "Project."},
			"created_at":        schema.StringAttribute{Computed: true, Description: "Creation time."},
		},
	}
}

func (d *serverDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.AtLeastOneOf(path.MatchRoot("id"), path.MatchRoot("name"), path.MatchRoot("label_selector")),
	}
}

func (d *serverDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m serverDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var s client.Server
	if !m.ID.IsNull() {
		id := parseID(&resp.Diagnostics, m.ID.ValueString())
		if resp.Diagnostics.HasError() {
			return
		}
		var err error
		if s, err = d.client.GetServer(ctx, id); err != nil {
			addAPIError(&resp.Diagnostics, "Reading server failed", err)
			return
		}
	} else {
		all, err := d.client.ListServers(ctx, m.LabelSelector.ValueString())
		if err != nil {
			addAPIError(&resp.Diagnostics, "Listing servers failed", err)
			return
		}
		var found []client.Server
		for _, srv := range all {
			if m.Name.IsNull() || srv.Name == m.Name.ValueString() {
				found = append(found, srv)
			}
		}
		var ok bool
		if s, ok = pickOne(&resp.Diagnostics, "server", found); !ok {
			return
		}
	}
	m.ID, m.Name = idString(s.ID), types.StringValue(s.Name)
	m.Datacenter, m.PlanID, m.Image = types.StringValue(s.Datacenter), types.Int64Value(s.PlanID), types.StringValue(s.Image)
	m.Status, m.IPv4, m.IPv6 = types.StringValue(s.Status), types.StringValue(s.IPv4), types.StringValue(s.IPv6)
	m.DiskGB, m.Labels = types.Int64Value(s.DiskGB), labelsValue(s.Labels)
	m.ProtectionDelete, m.ProjectID, m.CreatedAt = types.BoolValue(s.ProtectionDelete), types.Int64Value(s.ProjectID), timeValue(s.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- network

type networkDataSource struct{ dsBase }

func newNetworkDataSource() datasource.DataSource { return &networkDataSource{} }

type networkDataModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	LabelSelector types.String `tfsdk:"label_selector"`
	Datacenter    types.String `tfsdk:"datacenter"`
	IPRange       types.String `tfsdk:"ip_range"`
	Status        types.String `tfsdk:"status"`
	Labels        types.Map    `tfsdk:"labels"`
	Subnets       types.List   `tfsdk:"subnets"`
	Routes        types.List   `tfsdk:"routes"`
	ProjectID     types.Int64  `tfsdk:"project_id"`
}

func (d *networkDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (d *networkDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "One existing private network, by `id`, `name` or `label_selector`.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Optional: true, Computed: true, Description: "Network ID."},
			"name":           schema.StringAttribute{Optional: true, Computed: true, Description: "Network name."},
			"label_selector": schema.StringAttribute{Optional: true, Description: "Label selector that must match exactly one network."},
			"datacenter":     schema.StringAttribute{Computed: true, Description: "Datacenter."},
			"ip_range":       schema.StringAttribute{Computed: true, Description: "IP range."},
			"status":         schema.StringAttribute{Computed: true, Description: "Status."},
			"labels":         schema.MapAttribute{ElementType: types.StringType, Computed: true, Description: "Labels."},
			"project_id":     schema.Int64Attribute{Computed: true, Description: "Project."},
			"subnets": schema.ListNestedAttribute{Computed: true, Description: "Subnets.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":       schema.Int64Attribute{Computed: true, Description: "Subnet ID."},
					"ip_range": schema.StringAttribute{Computed: true, Description: "Range."},
					"gateway":  schema.StringAttribute{Computed: true, Description: "Router address."},
					"status":   schema.StringAttribute{Computed: true, Description: "Status."},
				}}},
			"routes": schema.ListNestedAttribute{Computed: true, Description: "Routes.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"destination": schema.StringAttribute{Computed: true, Description: "Destination."},
					"gateway":     schema.StringAttribute{Computed: true, Description: "Next hop."},
				}}},
		},
	}
}

func (d *networkDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.AtLeastOneOf(path.MatchRoot("id"), path.MatchRoot("name"), path.MatchRoot("label_selector")),
	}
}

func (d *networkDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m networkDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var n client.Network
	if !m.ID.IsNull() {
		id := parseID(&resp.Diagnostics, m.ID.ValueString())
		if resp.Diagnostics.HasError() {
			return
		}
		var err error
		if n, err = d.client.GetNetwork(ctx, id); err != nil {
			addAPIError(&resp.Diagnostics, "Reading network failed", err)
			return
		}
	} else {
		all, err := d.client.ListNetworks(ctx, m.LabelSelector.ValueString())
		if err != nil {
			addAPIError(&resp.Diagnostics, "Listing networks failed", err)
			return
		}
		var found []client.Network
		for _, nw := range all {
			if m.Name.IsNull() || nw.Name == m.Name.ValueString() {
				found = append(found, nw)
			}
		}
		var ok bool
		if n, ok = pickOne(&resp.Diagnostics, "network", found); !ok {
			return
		}
	}
	m.ID, m.Name, m.Datacenter, m.IPRange = idString(n.ID), types.StringValue(n.Name), types.StringValue(n.Datacenter), types.StringValue(n.IPRange)
	m.Status, m.Labels, m.ProjectID = types.StringValue(n.Status), labelsValue(n.Labels), types.Int64Value(n.ProjectID)
	m.Subnets, m.Routes = subnetsValue(n.Subnets), routesValue(n.Routes)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- ssh key

type sshKeyDataSource struct{ dsBase }

func newSSHKeyDataSource() datasource.DataSource { return &sshKeyDataSource{} }

type sshKeyDataModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	PublicKey   types.String `tfsdk:"public_key"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
}

func (d *sshKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (d *sshKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "One of the account's SSH keys (`/api/v1/ssh-keys`), by `id`, `name` or `fingerprint`.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Optional: true, Computed: true, Description: "Key ID."},
			"name":        schema.StringAttribute{Optional: true, Computed: true, Description: "Key name."},
			"fingerprint": schema.StringAttribute{Optional: true, Computed: true, Description: "SHA256 fingerprint."},
			"public_key":  schema.StringAttribute{Computed: true, Description: "Public key."},
			"is_default":  schema.BoolAttribute{Computed: true, Description: "Whether it is the default key."},
		},
	}
}

func (d *sshKeyDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.AtLeastOneOf(path.MatchRoot("id"), path.MatchRoot("name"), path.MatchRoot("fingerprint")),
	}
}

func (d *sshKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m sshKeyDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var k client.SSHKey
	if !m.ID.IsNull() {
		id := parseID(&resp.Diagnostics, m.ID.ValueString())
		if resp.Diagnostics.HasError() {
			return
		}
		var err error
		if k, err = d.client.GetSSHKey(ctx, id); err != nil {
			addAPIError(&resp.Diagnostics, "Reading SSH key failed", err)
			return
		}
	} else {
		all, err := d.client.ListSSHKeys(ctx)
		if err != nil {
			addAPIError(&resp.Diagnostics, "Listing SSH keys failed", err)
			return
		}
		var found []client.SSHKey
		for _, key := range all {
			if (m.Name.IsNull() || key.NameOrEmpty() == m.Name.ValueString()) &&
				(m.Fingerprint.IsNull() || key.Fingerprint == m.Fingerprint.ValueString()) {
				found = append(found, key)
			}
		}
		var ok bool
		if k, ok = pickOne(&resp.Diagnostics, "SSH key", found); !ok {
			return
		}
	}
	m.ID, m.Name, m.Fingerprint = idString(k.ID), types.StringValue(k.NameOrEmpty()), types.StringValue(k.Fingerprint)
	m.PublicKey, m.IsDefault = types.StringValue(k.Key), types.BoolValue(k.IsDefault)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
