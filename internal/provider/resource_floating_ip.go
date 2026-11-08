package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*floatingIPResource)(nil)
	_ resource.ResourceWithImportState = (*floatingIPResource)(nil)
)

type floatingIPResource struct{ base }

func newFloatingIPResource() resource.Resource { return &floatingIPResource{} }

type floatingIPModel struct {
	ID               types.String   `tfsdk:"id"`
	Datacenter       types.String   `tfsdk:"datacenter"`
	Type             types.String   `tfsdk:"type"`
	Name             types.String   `tfsdk:"name"`
	Description      types.String   `tfsdk:"description"`
	ServerID         types.Int64    `tfsdk:"server_id"`
	DNSPtr           types.String   `tfsdk:"dns_ptr"`
	Labels           types.Map      `tfsdk:"labels"`
	ProtectionDelete types.Bool     `tfsdk:"protection_delete"`
	ProjectID        types.Int64    `tfsdk:"project_id"`
	IPAddress        types.String   `tfsdk:"ip_address"`
	Status           types.String   `tfsdk:"status"`
	CreatedAt        types.String   `tfsdk:"created_at"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

func (r *floatingIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_floating_ip"
}

func (r *floatingIPResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A floating IP. Maps to `/cloud/v1/floating-ips` and its `assign`/`unassign`/`change_dns_ptr` actions.",
		Attributes: map[string]schema.Attribute{
			"id":         idAttribute("Floating IP ID."),
			"datacenter": schema.StringAttribute{Required: true, Description: "Datacenter code. Changing it replaces the IP.", PlanModifiers: replace},
			"type": schema.StringAttribute{
				Required: true, Description: "`ipv4` or `ipv6`. Changing it replaces the IP.", PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.OneOf("ipv4", "ipv6")},
			},
			"name": schema.StringAttribute{Optional: true, Computed: true, Description: "Name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "Description.",
			},
			"server_id": schema.Int64Attribute{
				Optional: true, Computed: true,
				Description:   "Server the IP is assigned to. Do not combine with `arianet_floating_ip_assignment`.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"dns_ptr": schema.StringAttribute{
				Optional: true, Computed: true, Description: "Reverse DNS (PTR) hostname of the IP.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"labels":            labelsAttribute(),
			"protection_delete": protectionDeleteAttribute(),
			"project_id":        projectIDAttribute(),
			"ip_address":        computedStableString("The IP address."),
			"status":            computedString("Status."),
			"created_at":        computedStableString("Creation time (RFC 3339)."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *floatingIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *floatingIPResource) fill(m *floatingIPModel, f client.FloatingIP) {
	m.ID = idString(f.ID)
	m.Datacenter = types.StringValue(f.Datacenter)
	m.Type = types.StringValue(f.Type)
	m.Name = types.StringValue(f.Name)
	m.Description = types.StringValue(f.Description)
	m.ServerID = ptrInt64Value(f.ServerID)
	m.DNSPtr = types.StringValue(f.DNSPtr)
	m.Labels = labelsValue(f.Labels)
	m.ProtectionDelete = types.BoolValue(f.ProtectionDelete)
	m.ProjectID = types.Int64Value(f.ProjectID)
	m.IPAddress = types.StringValue(f.IP)
	m.Status = types.StringValue(f.Status)
	m.CreatedAt = timeValue(f.CreatedAt)
}

func (r *floatingIPResource) refresh(ctx context.Context, m *floatingIPModel, diags *diagnosticsAlias) bool {
	id := parseID(diags, m.ID.ValueString())
	if diags.HasError() {
		return false
	}
	f, err := r.client.GetFloatingIP(ctx, id)
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading floating IP failed", err)
		return false
	}
	r.fill(m, f)
	return true
}

func (r *floatingIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan floatingIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	body := client.CreateFloatingIPRequest{
		Datacenter:  plan.Datacenter.ValueString(),
		Type:        plan.Type.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      labelsFrom(ctx, plan.Labels, &resp.Diagnostics),
		ProjectID:   optionalInt64(plan.ProjectID),
	}
	if !plan.Name.IsUnknown() {
		body.Name = plan.Name.ValueString()
	}
	if !plan.ServerID.IsUnknown() && !plan.ServerID.IsNull() {
		body.ServerID = plan.ServerID.ValueInt64()
	}
	if resp.Diagnostics.HasError() {
		return
	}
	f, actionID, err := r.client.CreateFloatingIP(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating floating IP failed", err)
		return
	}
	wantPtr := plan.DNSPtr
	r.fill(&plan, f)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if err := r.client.WaitAction(ctx, actionID); err != nil {
		addAPIError(&resp.Diagnostics, "Waiting for the floating IP failed", err)
		return
	}
	if !wantPtr.IsUnknown() && !wantPtr.IsNull() && wantPtr.ValueString() != f.DNSPtr {
		if err := r.wait(ctx)(r.client.ChangeFloatingIPDNSPtr(ctx, f.ID, wantPtr.ValueString())); err != nil {
			addAPIError(&resp.Diagnostics, "Setting reverse DNS failed", err)
			return
		}
	}
	if plan.ProtectionDelete.ValueBool() != f.ProtectionDelete {
		pd := plan.ProtectionDelete.ValueBool()
		if _, err := r.client.UpdateFloatingIP(ctx, f.ID, client.UpdateRequest{ProtectionDelete: &pd}); err != nil {
			addAPIError(&resp.Diagnostics, "Setting the floating IP's protection failed", err)
			return
		}
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state floatingIPModel
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

func (r *floatingIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state floatingIPModel
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
	if !plan.Description.Equal(state.Description) {
		v := plan.Description.ValueString()
		patch.Description, changed = &v, true
	}
	if changed {
		if _, err := r.client.UpdateFloatingIP(ctx, id, patch); err != nil {
			addAPIError(&resp.Diagnostics, "Updating floating IP failed", err)
			return
		}
	}
	if !plan.ServerID.IsUnknown() && !plan.ServerID.Equal(state.ServerID) {
		var err error
		if plan.ServerID.IsNull() {
			err = r.wait(ctx)(r.client.UnassignFloatingIP(ctx, id))
		} else {
			err = r.wait(ctx)(r.client.AssignFloatingIP(ctx, id, plan.ServerID.ValueInt64()))
		}
		if err != nil {
			addAPIError(&resp.Diagnostics, "Changing the floating IP's server failed", err)
			return
		}
	}
	if !plan.DNSPtr.IsUnknown() && !plan.DNSPtr.Equal(state.DNSPtr) {
		if err := r.wait(ctx)(r.client.ChangeFloatingIPDNSPtr(ctx, id, plan.DNSPtr.ValueString())); err != nil {
			addAPIError(&resp.Diagnostics, "Setting reverse DNS failed", err)
			return
		}
	}
	plan.ID = state.ID
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Floating IP disappeared", "The floating IP was not found after the update.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state floatingIPModel
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
	err := r.wait(ctx)(r.client.DeleteFloatingIP(ctx, id))
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Deleting floating IP failed", err)
	}
}

// ---------------------------------------------------------------- assignment

var (
	_ resource.Resource                = (*floatingIPAssignmentResource)(nil)
	_ resource.ResourceWithImportState = (*floatingIPAssignmentResource)(nil)
)

type floatingIPAssignmentResource struct{ base }

func newFloatingIPAssignmentResource() resource.Resource { return &floatingIPAssignmentResource{} }

type floatingIPAssignmentModel struct {
	ID           types.String   `tfsdk:"id"`
	FloatingIPID types.Int64    `tfsdk:"floating_ip_id"`
	ServerID     types.Int64    `tfsdk:"server_id"`
	Timeouts     timeouts.Value `tfsdk:"timeouts"`
}

func (r *floatingIPAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_floating_ip_assignment"
}

func (r *floatingIPAssignmentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Assigns a floating IP to a server (`assign`, `unassign` on destroy). The ID is the floating IP ID. Do not also set `server_id` on the `arianet_floating_ip`.",
		Attributes: map[string]schema.Attribute{
			"id":             idAttribute("The floating IP ID."),
			"floating_ip_id": schema.Int64Attribute{Required: true, Description: "Floating IP to assign.", PlanModifiers: replace},
			"server_id":      schema.Int64Attribute{Required: true, Description: "Server to assign it to.", PlanModifiers: replace},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *floatingIPAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := parseID(&resp.Diagnostics, req.ID)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("floating_ip_id"), id)...)
}

func (r *floatingIPAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan floatingIPAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	if err := r.wait(ctx)(r.client.AssignFloatingIP(ctx, plan.FloatingIPID.ValueInt64(), plan.ServerID.ValueInt64())); err != nil {
		addAPIError(&resp.Diagnostics, "Assigning floating IP failed", err)
		return
	}
	plan.ID = idString(plan.FloatingIPID.ValueInt64())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state floatingIPAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.client.GetFloatingIP(ctx, state.FloatingIPID.ValueInt64())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading floating IP failed", err)
		return
	}
	if f.ServerID == nil || (!state.ServerID.IsNull() && *f.ServerID != state.ServerID.ValueInt64()) {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ServerID = types.Int64Value(*f.ServerID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *floatingIPAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan floatingIPAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state floatingIPAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	f, err := r.client.GetFloatingIP(ctx, state.FloatingIPID.ValueInt64())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading floating IP failed", err)
		return
	}
	if f.ServerID == nil || *f.ServerID != state.ServerID.ValueInt64() {
		return
	}
	if err := r.wait(ctx)(r.client.UnassignFloatingIP(ctx, f.ID)); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Unassigning floating IP failed", err)
	}
}
