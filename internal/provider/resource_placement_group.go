package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*placementGroupResource)(nil)
	_ resource.ResourceWithImportState = (*placementGroupResource)(nil)
)

type placementGroupResource struct{ base }

func newPlacementGroupResource() resource.Resource { return &placementGroupResource{} }

type placementGroupModel struct {
	ID         types.String   `tfsdk:"id"`
	Name       types.String   `tfsdk:"name"`
	Datacenter types.String   `tfsdk:"datacenter"`
	Type       types.String   `tfsdk:"type"`
	Labels     types.Map      `tfsdk:"labels"`
	ProjectID  types.Int64    `tfsdk:"project_id"`
	Servers    types.List     `tfsdk:"servers"`
	MaxServers types.Int64    `tfsdk:"max_servers"`
	Status     types.String   `tfsdk:"status"`
	CreatedAt  types.String   `tfsdk:"created_at"`
	Timeouts   timeouts.Value `tfsdk:"timeouts"`
}

func (r *placementGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_placement_group"
}

func (r *placementGroupResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A placement group: servers created in it are spread over different hosts. Maps to `/cloud/v1/placement-groups`; servers join it with `arianet_server.placement_group_id`.",
		Attributes: map[string]schema.Attribute{
			"id":         idAttribute("Placement group ID."),
			"name":       schema.StringAttribute{Required: true, Description: "Name: letters, digits, dots and dashes, up to 63 characters."},
			"datacenter": schema.StringAttribute{Required: true, Description: "Datacenter code. Changing it replaces the group.", PlanModifiers: replace},
			"type": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("spread"),
				Description: "Placement type. Only `spread` exists.", PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.OneOf("spread")},
			},
			"labels":      labelsAttribute(),
			"project_id":  projectIDAttribute(),
			"servers":     schema.ListAttribute{ElementType: types.Int64Type, Computed: true, Description: "Servers in the group."},
			"max_servers": schema.Int64Attribute{Computed: true, Description: "How many servers the group can hold."},
			"status":      computedString("Status."),
			"created_at":  computedStableString("Creation time (RFC 3339)."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *placementGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *placementGroupResource) fill(m *placementGroupModel, g client.PlacementGroup) {
	m.ID = idString(g.ID)
	m.Name = types.StringValue(g.Name)
	m.Datacenter = types.StringValue(g.Datacenter)
	m.Type = types.StringValue(g.Type)
	m.Labels = labelsValue(g.Labels)
	m.ProjectID = types.Int64Value(g.ProjectID)
	m.Servers = int64ListValue(g.Servers)
	m.MaxServers = types.Int64Value(g.MaxServers)
	m.Status = types.StringValue(g.Status)
	m.CreatedAt = timeValue(g.CreatedAt)
}

func (r *placementGroupResource) refresh(ctx context.Context, m *placementGroupModel, diags *diagnosticsAlias) bool {
	id := parseID(diags, m.ID.ValueString())
	if diags.HasError() {
		return false
	}
	g, err := r.client.GetPlacementGroup(ctx, id)
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading placement group failed", err)
		return false
	}
	r.fill(m, g)
	return true
}

func (r *placementGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan placementGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	g, actionID, err := r.client.CreatePlacementGroup(ctx, client.CreatePlacementGroupRequest{
		Name:       plan.Name.ValueString(),
		Datacenter: plan.Datacenter.ValueString(),
		Type:       plan.Type.ValueString(),
		Labels:     labelsFrom(ctx, plan.Labels, &resp.Diagnostics),
		ProjectID:  optionalInt64(plan.ProjectID),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating placement group failed", err)
		return
	}
	r.fill(&plan, g)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if err := r.client.WaitAction(ctx, actionID); err != nil {
		addAPIError(&resp.Diagnostics, "Waiting for the placement group failed", err)
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *placementGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state placementGroupModel
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

func (r *placementGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state placementGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := parseID(&resp.Diagnostics, state.ID.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	patch, changed := networkingPatch(ctx, plan.Name, state.Name, plan.Labels, state.Labels, types.BoolNull(), types.BoolNull(), plan.ProjectID, state.ProjectID, &resp.Diagnostics)
	if changed {
		if _, err := r.client.UpdatePlacementGroup(ctx, id, patch); err != nil {
			addAPIError(&resp.Diagnostics, "Updating placement group failed", err)
			return
		}
	}
	plan.ID = state.ID
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Placement group disappeared", "The placement group was not found after the update.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *placementGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state placementGroupModel
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
	if err := r.wait(ctx)(r.client.DeletePlacementGroup(ctx, id)); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Deleting placement group failed", err)
	}
}
