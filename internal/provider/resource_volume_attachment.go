package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*volumeAttachmentResource)(nil)
	_ resource.ResourceWithImportState = (*volumeAttachmentResource)(nil)
)

type volumeAttachmentResource struct{ base }

func newVolumeAttachmentResource() resource.Resource { return &volumeAttachmentResource{} }

type volumeAttachmentModel struct {
	ID       types.String   `tfsdk:"id"`
	VolumeID types.Int64    `tfsdk:"volume_id"`
	ServerID types.Int64    `tfsdk:"server_id"`
	Device   types.String   `tfsdk:"device"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (r *volumeAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume_attachment"
}

func (r *volumeAttachmentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Attaches a volume to a server (`POST /volumes/{id}/actions/attach`, `detach` on destroy). The ID is the volume ID. Do not also set `server_id` on the `arianet_volume`.",
		Attributes: map[string]schema.Attribute{
			"id":        idAttribute("The volume ID."),
			"volume_id": schema.Int64Attribute{Required: true, Description: "Volume to attach.", PlanModifiers: replace},
			"server_id": schema.Int64Attribute{Required: true, Description: "Server to attach it to.", PlanModifiers: replace},
			"device":    computedString("Device path inside the server, when known."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *volumeAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := parseID(&resp.Diagnostics, req.ID)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("volume_id"), id)...)
}

func (r *volumeAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	actionID, err := r.client.AttachVolume(ctx, plan.VolumeID.ValueInt64(), plan.ServerID.ValueInt64())
	if err == nil {
		err = r.client.WaitAction(ctx, actionID)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Attaching volume failed", err)
		return
	}
	plan.ID = idString(plan.VolumeID.ValueInt64())
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Attachment not found", "The volume is not attached to the server after the attach action finished.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeAttachmentResource) refresh(ctx context.Context, m *volumeAttachmentModel, diags *diagnosticsAlias) bool {
	v, err := r.client.GetVolume(ctx, m.VolumeID.ValueInt64())
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading volume failed", err)
		return false
	}
	if v.ServerID == nil {
		return false
	}
	if !m.ServerID.IsNull() && !m.ServerID.IsUnknown() && *v.ServerID != m.ServerID.ValueInt64() {
		return false
	}
	m.ServerID = types.Int64Value(*v.ServerID)
	m.Device = types.StringValue(v.Device)
	return true
}

func (r *volumeAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeAttachmentModel
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

func (r *volumeAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every argument forces replacement; only timeouts can change in place.
	var plan volumeAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	v, err := r.client.GetVolume(ctx, state.VolumeID.ValueInt64())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading volume failed", err)
		return
	}
	if v.ServerID == nil || *v.ServerID != state.ServerID.ValueInt64() {
		return
	}
	actionID, err := r.client.DetachVolume(ctx, v.ID)
	if client.IsNotFound(err) {
		return
	}
	if err == nil {
		err = r.client.WaitAction(ctx, actionID)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Detaching volume failed", err)
	}
}
