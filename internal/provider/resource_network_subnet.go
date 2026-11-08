package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*networkSubnetResource)(nil)
	_ resource.ResourceWithImportState = (*networkSubnetResource)(nil)
)

type networkSubnetResource struct{ base }

func newNetworkSubnetResource() resource.Resource { return &networkSubnetResource{} }

type networkSubnetModel struct {
	ID        types.String   `tfsdk:"id"`
	NetworkID types.Int64    `tfsdk:"network_id"`
	IPRange   types.String   `tfsdk:"ip_range"`
	SubnetID  types.Int64    `tfsdk:"subnet_id"`
	Gateway   types.String   `tfsdk:"gateway"`
	Status    types.String   `tfsdk:"status"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func (r *networkSubnetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network_subnet"
}

func (r *networkSubnetResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An additional subnet of a private network (`POST /networks/{id}/subnets`, `DELETE /networks/{id}/subnets/{subnet}`). Import ID: `<network_id>/<subnet_id>`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("`<network_id>/<subnet_id>`."),
			"network_id": schema.Int64Attribute{
				Required: true, Description: "Network to add the subnet to.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"ip_range": schema.StringAttribute{
				Required: true, Description: "Subnet range in CIDR notation, inside the network's `ip_range` and not overlapping its other subnets.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"subnet_id": schema.Int64Attribute{Computed: true, Description: "Subnet ID.", PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"gateway":   computedStableString("The subnet's router address (its first host)."),
			"status":    computedString("Subnet status."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *networkSubnetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	a, b := splitID(&resp.Diagnostics, req.ID, "<network_id>/<subnet_id>")
	if resp.Diagnostics.HasError() {
		return
	}
	nid, sid := parseID(&resp.Diagnostics, a), parseID(&resp.Diagnostics, b)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), nid)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("subnet_id"), sid)...)
}

func (r *networkSubnetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkSubnetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	nid := plan.NetworkID.ValueInt64()
	sn, actionID, err := r.client.AddSubnet(ctx, nid, plan.IPRange.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Adding subnet failed", err)
		return
	}
	plan.ID = types.StringValue(compositeID(strconv.FormatInt(nid, 10), strconv.FormatInt(sn.ID, 10)))
	plan.SubnetID = types.Int64Value(sn.ID)
	plan.IPRange = types.StringValue(sn.IPRange)
	plan.Gateway = types.StringValue(sn.Gateway)
	plan.Status = types.StringValue(sn.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if err := r.client.WaitAction(ctx, actionID); err != nil {
		addAPIError(&resp.Diagnostics, "Waiting for the subnet failed", err)
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkSubnetResource) refresh(ctx context.Context, m *networkSubnetModel, diags *diagnosticsAlias) bool {
	n, err := r.client.GetNetwork(ctx, m.NetworkID.ValueInt64())
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading network failed", err)
		return false
	}
	for _, sn := range n.Subnets {
		if sn.ID == m.SubnetID.ValueInt64() {
			m.IPRange = types.StringValue(sn.IPRange)
			m.Gateway = types.StringValue(sn.Gateway)
			m.Status = types.StringValue(sn.Status)
			return true
		}
	}
	return false
}

func (r *networkSubnetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkSubnetModel
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

func (r *networkSubnetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan networkSubnetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkSubnetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkSubnetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	actionID, err := r.client.DeleteSubnet(ctx, state.NetworkID.ValueInt64(), state.SubnetID.ValueInt64())
	if client.IsNotFound(err) {
		return
	}
	if err == nil {
		err = r.client.WaitAction(ctx, actionID)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Deleting subnet failed", err)
	}
}
