package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*networkResource)(nil)
	_ resource.ResourceWithImportState = (*networkResource)(nil)
)

type networkResource struct{ base }

func newNetworkResource() resource.Resource { return &networkResource{} }

type networkModel struct {
	ID               types.String   `tfsdk:"id"`
	Name             types.String   `tfsdk:"name"`
	Datacenter       types.String   `tfsdk:"datacenter"`
	IPRange          types.String   `tfsdk:"ip_range"`
	SubnetIPRange    types.String   `tfsdk:"subnet_ip_range"`
	Labels           types.Map      `tfsdk:"labels"`
	ProtectionDelete types.Bool     `tfsdk:"protection_delete"`
	ProjectID        types.Int64    `tfsdk:"project_id"`
	Status           types.String   `tfsdk:"status"`
	Subnets          types.List     `tfsdk:"subnets"`
	Routes           types.List     `tfsdk:"routes"`
	CreatedAt        types.String   `tfsdk:"created_at"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

var subnetAttrTypes = map[string]attr.Type{
	"id":       types.Int64Type,
	"ip_range": types.StringType,
	"gateway":  types.StringType,
	"status":   types.StringType,
}

var routeAttrTypes = map[string]attr.Type{
	"destination": types.StringType,
	"gateway":     types.StringType,
}

func subnetsValue(subnets []client.Subnet) types.List {
	vals := make([]attr.Value, 0, len(subnets))
	for _, s := range subnets {
		vals = append(vals, types.ObjectValueMust(subnetAttrTypes, map[string]attr.Value{
			"id": types.Int64Value(s.ID), "ip_range": types.StringValue(s.IPRange),
			"gateway": types.StringValue(s.Gateway), "status": types.StringValue(s.Status),
		}))
	}
	return types.ListValueMust(types.ObjectType{AttrTypes: subnetAttrTypes}, vals)
}

func routesValue(routes []client.Route) types.List {
	vals := make([]attr.Value, 0, len(routes))
	for _, r := range routes {
		vals = append(vals, types.ObjectValueMust(routeAttrTypes, map[string]attr.Value{
			"destination": types.StringValue(r.Destination), "gateway": types.StringValue(r.Gateway),
		}))
	}
	return types.ListValueMust(types.ObjectType{AttrTypes: routeAttrTypes}, vals)
}

func (r *networkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (r *networkResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A private network. Maps to `/cloud/v1/networks`. The API always creates a first subnet (`subnet_ip_range`, or the whole `ip_range`); add more with `arianet_network_subnet` and routes with `arianet_network_route`.",
		Attributes: map[string]schema.Attribute{
			"id":         idAttribute("Network ID."),
			"name":       schema.StringAttribute{Required: true, Description: "Network name."},
			"datacenter": schema.StringAttribute{Required: true, Description: "Datacenter code. Changing it replaces the network.", PlanModifiers: replace},
			"ip_range":   schema.StringAttribute{Required: true, Description: "IP range of the network in CIDR notation (private ranges, e.g. `10.0.0.0/16`). Changing it replaces the network.", PlanModifiers: replace},
			"subnet_ip_range": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "IP range of the first subnet, inside `ip_range`. Defaults to the whole `ip_range`. Changing it replaces the network.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
			},
			"labels":            labelsAttribute(),
			"protection_delete": protectionDeleteAttribute(),
			"project_id":        projectIDAttribute(),
			"status":            computedString("Network status."),
			"subnets": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Every subnet of the network, including the first one and those of `arianet_network_subnet`.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":       schema.Int64Attribute{Computed: true, Description: "Subnet ID."},
					"ip_range": schema.StringAttribute{Computed: true, Description: "Subnet range."},
					"gateway":  schema.StringAttribute{Computed: true, Description: "The subnet's router address."},
					"status":   schema.StringAttribute{Computed: true, Description: "Subnet status."},
				}},
			},
			"routes": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Static routes of the network (managed with `arianet_network_route`).",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"destination": schema.StringAttribute{Computed: true, Description: "Destination range."},
					"gateway":     schema.StringAttribute{Computed: true, Description: "Next hop."},
				}},
			},
			"created_at": computedStableString("Creation time (RFC 3339)."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *networkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *networkResource) fill(m *networkModel, n client.Network) {
	m.ID = idString(n.ID)
	m.Name = types.StringValue(n.Name)
	m.Datacenter = types.StringValue(n.Datacenter)
	m.IPRange = types.StringValue(n.IPRange)
	if (m.SubnetIPRange.IsNull() || m.SubnetIPRange.IsUnknown()) && len(n.Subnets) > 0 {
		m.SubnetIPRange = types.StringValue(n.Subnets[0].IPRange)
	}
	if m.SubnetIPRange.IsUnknown() {
		m.SubnetIPRange = types.StringValue(n.IPRange)
	}
	m.Labels = labelsValue(n.Labels)
	m.ProtectionDelete = types.BoolValue(n.ProtectionDelete)
	m.ProjectID = types.Int64Value(n.ProjectID)
	m.Status = types.StringValue(n.Status)
	m.Subnets = subnetsValue(n.Subnets)
	m.Routes = routesValue(n.Routes)
	m.CreatedAt = timeValue(n.CreatedAt)
}

func (r *networkResource) refresh(ctx context.Context, m *networkModel, diags *diagnosticsAlias) bool {
	id := parseID(diags, m.ID.ValueString())
	if diags.HasError() {
		return false
	}
	n, err := r.client.GetNetwork(ctx, id)
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading network failed", err)
		return false
	}
	r.fill(m, n)
	return true
}

func (r *networkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	body := client.CreateNetworkRequest{
		Name:       plan.Name.ValueString(),
		Datacenter: plan.Datacenter.ValueString(),
		IPRange:    plan.IPRange.ValueString(),
		Labels:     labelsFrom(ctx, plan.Labels, &resp.Diagnostics),
		ProjectID:  optionalInt64(plan.ProjectID),
	}
	if !plan.SubnetIPRange.IsUnknown() {
		body.SubnetIPRange = plan.SubnetIPRange.ValueString()
	}
	if resp.Diagnostics.HasError() {
		return
	}
	n, actionID, err := r.client.CreateNetwork(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating network failed", err)
		return
	}
	r.fill(&plan, n)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if err := r.client.WaitAction(ctx, actionID); err != nil {
		addAPIError(&resp.Diagnostics, "Waiting for the network to be created failed", err)
		return
	}
	if plan.ProtectionDelete.ValueBool() != n.ProtectionDelete {
		pd := plan.ProtectionDelete.ValueBool()
		if _, err := r.client.UpdateNetwork(ctx, n.ID, client.UpdateRequest{ProtectionDelete: &pd}); err != nil {
			addAPIError(&resp.Diagnostics, "Setting the network's protection failed", err)
			return
		}
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.refresh(ctx, &state, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *networkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state networkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Update(ctx, defaultUpdateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	id := parseID(&resp.Diagnostics, state.ID.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	patch, changed := networkingPatch(ctx, plan.Name, state.Name, plan.Labels, state.Labels, plan.ProtectionDelete, state.ProtectionDelete, plan.ProjectID, state.ProjectID, &resp.Diagnostics)
	if changed {
		if _, err := r.client.UpdateNetwork(ctx, id, patch); err != nil {
			addAPIError(&resp.Diagnostics, "Updating network failed", err)
			return
		}
	}
	plan.ID = state.ID
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Network disappeared", "The network was not found after the update.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	id := parseID(&resp.Diagnostics, state.ID.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	actionID, err := r.client.DeleteNetwork(ctx, id)
	if client.IsNotFound(err) {
		return
	}
	if err == nil {
		err = r.client.WaitAction(ctx, actionID)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Deleting network failed", err)
	}
}

// networkingPatch builds the shared PATCH body from what changed.
func networkingPatch(ctx context.Context, planName, stateName types.String, planLabels, stateLabels types.Map,
	planProt, stateProt types.Bool, planProject, stateProject types.Int64, diags *diagnosticsAlias,
) (client.UpdateRequest, bool) {
	var patch client.UpdateRequest
	changed := false
	if !planName.IsUnknown() && !planName.Equal(stateName) {
		v := planName.ValueString()
		patch.Name, changed = &v, true
	}
	if !planLabels.Equal(stateLabels) {
		l := labelsFrom(ctx, planLabels, diags)
		patch.Labels, changed = &l, true
	}
	if !planProt.IsNull() && !planProt.Equal(stateProt) {
		v := planProt.ValueBool()
		patch.ProtectionDelete, changed = &v, true
	}
	if !planProject.IsUnknown() && !planProject.IsNull() && !planProject.Equal(stateProject) {
		patch.ProjectID, changed = optionalInt64(planProject), true
	}
	return patch, changed
}
