package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*loadBalancerResource)(nil)
	_ resource.ResourceWithImportState = (*loadBalancerResource)(nil)
)

type loadBalancerResource struct{ base }

func newLoadBalancerResource() resource.Resource { return &loadBalancerResource{} }

type loadBalancerModel struct {
	ID               types.String   `tfsdk:"id"`
	Name             types.String   `tfsdk:"name"`
	Datacenter       types.String   `tfsdk:"datacenter"`
	Type             types.String   `tfsdk:"type"`
	Algorithm        types.String   `tfsdk:"algorithm"`
	PublicInterface  types.Bool     `tfsdk:"public_interface"`
	NetworkID        types.Int64    `tfsdk:"network_id"`
	Labels           types.Map      `tfsdk:"labels"`
	ProtectionDelete types.Bool     `tfsdk:"protection_delete"`
	ProjectID        types.Int64    `tfsdk:"project_id"`
	IPv4             types.String   `tfsdk:"ipv4"`
	IPv6             types.String   `tfsdk:"ipv6"`
	PrivateIP        types.String   `tfsdk:"private_ip"`
	Status           types.String   `tfsdk:"status"`
	CreatedAt        types.String   `tfsdk:"created_at"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

func (r *loadBalancerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer"
}

func (r *loadBalancerResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A load balancer. Maps to `/cloud/v1/load-balancers` (`change_type`, `change_algorithm`, `attach_to_network`/`detach_from_network` actions). Listeners and targets are separate resources: `arianet_load_balancer_service` and `arianet_load_balancer_target`.",
		Attributes: map[string]schema.Attribute{
			"id":   idAttribute("Load balancer ID."),
			"name": schema.StringAttribute{Required: true, Description: "Name."},
			"datacenter": schema.StringAttribute{
				Required: true, Description: "Datacenter code. Changing it replaces the load balancer.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{Required: true, Description: "Load balancer type code, e.g. `lb11` (see `arianet_load_balancer_types`). Changing it resizes in place."},
			"algorithm": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("round_robin"),
				Description: "`round_robin` or `least_connections`.",
				Validators:  []validator.String{stringvalidator.OneOf("round_robin", "least_connections")},
			},
			"public_interface": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description:   "Whether the load balancer has public addresses. Changing it replaces the load balancer.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"network_id": schema.Int64Attribute{
				Optional:    true,
				Description: "Private network the load balancer is attached to (needed for targets with `use_private_ip`).",
			},
			"labels":            labelsAttribute(),
			"protection_delete": protectionDeleteAttribute(),
			"project_id":        projectIDAttribute(),
			"ipv4":              computedString("Public IPv4 address."),
			"ipv6":              computedString("Public IPv6 address."),
			"private_ip":        computedString("Address in the private network."),
			"status":            computedString("Status."),
			"created_at":        computedStableString("Creation time (RFC 3339)."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *loadBalancerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func strPtrOrEmpty(p *string) types.String {
	if p == nil {
		return types.StringValue("")
	}
	return types.StringValue(*p)
}

func (r *loadBalancerResource) fill(m *loadBalancerModel, lb client.LoadBalancer) {
	m.ID = idString(lb.ID)
	m.Name = types.StringValue(lb.Name)
	m.Datacenter = types.StringValue(lb.Datacenter)
	m.Type = types.StringValue(lb.Type)
	m.Algorithm = types.StringValue(lb.Algorithm)
	m.PublicInterface = types.BoolValue(lb.PublicInterface)
	if lb.Network != nil {
		m.NetworkID = types.Int64Value(lb.Network.ID)
	} else {
		m.NetworkID = types.Int64Null()
	}
	m.Labels = labelsValue(lb.Labels)
	m.ProtectionDelete = types.BoolValue(lb.ProtectionDelete)
	m.ProjectID = types.Int64Value(lb.ProjectID)
	m.IPv4 = strPtrOrEmpty(lb.IPv4)
	m.IPv6 = strPtrOrEmpty(lb.IPv6)
	m.PrivateIP = strPtrOrEmpty(lb.PrivateIP)
	m.Status = types.StringValue(lb.Status)
	m.CreatedAt = timeValue(lb.CreatedAt)
}

func (r *loadBalancerResource) refresh(ctx context.Context, m *loadBalancerModel, diags *diagnosticsAlias) bool {
	id := parseID(diags, m.ID.ValueString())
	if diags.HasError() {
		return false
	}
	lb, err := r.client.GetLoadBalancer(ctx, id)
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading load balancer failed", err)
		return false
	}
	r.fill(m, lb)
	return true
}

func (r *loadBalancerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan loadBalancerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	public := plan.PublicInterface.ValueBool()
	body := client.CreateLoadBalancerRequest{
		Datacenter:      plan.Datacenter.ValueString(),
		Name:            plan.Name.ValueString(),
		Type:            plan.Type.ValueString(),
		Algorithm:       plan.Algorithm.ValueString(),
		PublicInterface: &public,
		Labels:          labelsFrom(ctx, plan.Labels, &resp.Diagnostics),
		ProjectID:       optionalInt64(plan.ProjectID),
	}
	if !plan.NetworkID.IsNull() && !plan.NetworkID.IsUnknown() {
		body.NetworkID = plan.NetworkID.ValueInt64()
	}
	if resp.Diagnostics.HasError() {
		return
	}
	lb, actionID, err := r.client.CreateLoadBalancer(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating load balancer failed", err)
		return
	}
	r.fill(&plan, lb)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if err := r.client.WaitAction(ctx, actionID); err != nil {
		addAPIError(&resp.Diagnostics, "Waiting for the load balancer failed", err)
		return
	}
	if plan.ProtectionDelete.ValueBool() != lb.ProtectionDelete {
		pd := plan.ProtectionDelete.ValueBool()
		if _, err := r.client.UpdateLoadBalancer(ctx, lb.ID, client.UpdateLoadBalancerRequest{ProtectionDelete: &pd}); err != nil {
			addAPIError(&resp.Diagnostics, "Setting the load balancer's protection failed", err)
			return
		}
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *loadBalancerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state loadBalancerModel
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

func (r *loadBalancerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state loadBalancerModel
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

	var patch client.UpdateLoadBalancerRequest
	changed := false
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		patch.Name, changed = &v, true
	}
	if !plan.Labels.Equal(state.Labels) {
		l := labelsFrom(ctx, plan.Labels, &resp.Diagnostics)
		patch.Labels, changed = &l, true
	}
	if !plan.ProtectionDelete.Equal(state.ProtectionDelete) {
		v := plan.ProtectionDelete.ValueBool()
		patch.ProtectionDelete, changed = &v, true
	}
	if !plan.ProjectID.IsUnknown() && !plan.ProjectID.IsNull() && !plan.ProjectID.Equal(state.ProjectID) {
		patch.ProjectID, changed = optionalInt64(plan.ProjectID), true
	}
	if changed {
		if _, err := r.client.UpdateLoadBalancer(ctx, id, patch); err != nil {
			addAPIError(&resp.Diagnostics, "Updating load balancer failed", err)
			return
		}
	}
	if !plan.Type.Equal(state.Type) {
		if err := r.wait(ctx)(r.client.ChangeLBType(ctx, id, plan.Type.ValueString())); err != nil {
			addAPIError(&resp.Diagnostics, "Changing the load balancer type failed", err)
			return
		}
	}
	if !plan.Algorithm.Equal(state.Algorithm) {
		if err := r.wait(ctx)(r.client.ChangeLBAlgorithm(ctx, id, plan.Algorithm.ValueString())); err != nil {
			addAPIError(&resp.Diagnostics, "Changing the load balancer algorithm failed", err)
			return
		}
	}
	if !plan.NetworkID.Equal(state.NetworkID) {
		if !state.NetworkID.IsNull() {
			if err := r.wait(ctx)(r.client.DetachLBFromNetwork(ctx, id)); err != nil {
				addAPIError(&resp.Diagnostics, "Detaching the load balancer from its network failed", err)
				return
			}
		}
		if !plan.NetworkID.IsNull() {
			if err := r.wait(ctx)(r.client.AttachLBToNetwork(ctx, id, plan.NetworkID.ValueInt64())); err != nil {
				addAPIError(&resp.Diagnostics, "Attaching the load balancer to the network failed", err)
				return
			}
		}
	}
	plan.ID = state.ID
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Load balancer disappeared", "The load balancer was not found after the update.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *loadBalancerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state loadBalancerModel
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
	if err := r.wait(ctx)(r.client.DeleteLoadBalancer(ctx, id)); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Deleting load balancer failed", err)
	}
}
