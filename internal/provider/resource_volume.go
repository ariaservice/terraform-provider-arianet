package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*volumeResource)(nil)
	_ resource.ResourceWithImportState = (*volumeResource)(nil)
)

type volumeResource struct{ base }

func newVolumeResource() resource.Resource { return &volumeResource{} }

type volumeModel struct {
	ID               types.String   `tfsdk:"id"`
	Name             types.String   `tfsdk:"name"`
	Datacenter       types.String   `tfsdk:"datacenter"`
	Size             types.Int64    `tfsdk:"size"`
	ServerID         types.Int64    `tfsdk:"server_id"`
	Filesystem       types.String   `tfsdk:"filesystem"`
	Labels           types.Map      `tfsdk:"labels"`
	ProtectionDelete types.Bool     `tfsdk:"protection_delete"`
	ProjectID        types.Int64    `tfsdk:"project_id"`
	Status           types.String   `tfsdk:"status"`
	Device           types.String   `tfsdk:"device"`
	DiskBus          types.String   `tfsdk:"disk_bus"`
	DeviceIDPrefix   types.String   `tfsdk:"device_id_prefix"`
	CreatedAt        types.String   `tfsdk:"created_at"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

func (r *volumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (r *volumeResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A block storage volume. Maps to `/cloud/v1/volumes` and its `attach`/`detach`/`resize` actions.",
		Attributes: map[string]schema.Attribute{
			"id":   idAttribute("Volume ID."),
			"name": schema.StringAttribute{Required: true, Description: "Volume name."},
			"datacenter": schema.StringAttribute{
				Required:      true,
				Description:   "Datacenter code. Changing it replaces the volume.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"size": schema.Int64Attribute{
				Required:    true,
				Description: "Size in GB. Growing resizes the volume in place; shrinking replaces it (data is lost).",
				Validators:  []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplaceIf(
					func(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.IsUnknown() && req.PlanValue.ValueInt64() < req.StateValue.ValueInt64()
					}, "Shrinking a volume replaces it.", "Shrinking a volume replaces it.")},
			},
			"server_id": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Server the volume is attached to. Do not combine with `arianet_volume_attachment` for the same volume.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"filesystem": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Filesystem the volume is meant for: `ext4`, `xfs`, or empty.",
				Validators:    []validator.String{stringvalidator.OneOf("", "ext4", "xfs")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"labels":            labelsAttribute(),
			"protection_delete": protectionDeleteAttribute(),
			"project_id":        projectIDAttribute(),
			"status":            computedString("Volume status."),
			"device":            computedString("Stable device path inside the server, when attached (virtio)."),
			"disk_bus":          computedStableString("Disk bus of the datacenter: `virtio` or `scsi`."),
			"device_id_prefix":  computedStableString("Part of the volume's ID in the disk serial / by-id name."),
			"created_at":        computedStableString("Creation time (RFC 3339)."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *volumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *volumeResource) fill(m *volumeModel, v client.Volume) {
	m.ID = idString(v.ID)
	m.Name = types.StringValue(v.Name)
	m.Datacenter = types.StringValue(v.Datacenter)
	m.Size = types.Int64Value(v.SizeGB)
	m.ServerID = ptrInt64Value(v.ServerID)
	m.Filesystem = types.StringValue(v.Filesystem)
	m.Labels = labelsValue(v.Labels)
	m.ProtectionDelete = types.BoolValue(v.ProtectionDelete)
	m.ProjectID = types.Int64Value(v.ProjectID)
	m.Status = types.StringValue(v.Status)
	m.Device = types.StringValue(v.Device)
	m.DiskBus = types.StringValue(v.DiskBus)
	m.DeviceIDPrefix = types.StringValue(v.DeviceIDPrefix)
	m.CreatedAt = timeValue(v.CreatedAt)
}

func (r *volumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	body := client.CreateVolumeRequest{
		Datacenter: plan.Datacenter.ValueString(),
		Name:       plan.Name.ValueString(),
		SizeGB:     plan.Size.ValueInt64(),
		Filesystem: plan.Filesystem.ValueString(),
		Labels:     labelsFrom(ctx, plan.Labels, &resp.Diagnostics),
		ProjectID:  optionalInt64(plan.ProjectID),
	}
	if !plan.ServerID.IsNull() && !plan.ServerID.IsUnknown() {
		body.ServerID = plan.ServerID.ValueInt64()
	}
	if resp.Diagnostics.HasError() {
		return
	}
	v, actionID, err := r.client.CreateVolume(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating volume failed", err)
		return
	}
	r.fill(&plan, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if err := r.client.WaitAction(ctx, actionID); err != nil {
		addAPIError(&resp.Diagnostics, "Waiting for the volume to be created failed", err)
		return
	}
	if plan.ProtectionDelete.ValueBool() != v.ProtectionDelete {
		pd := plan.ProtectionDelete.ValueBool()
		if _, err := r.client.UpdateVolume(ctx, v.ID, client.UpdateVolumeRequest{ProtectionDelete: &pd}); err != nil {
			addAPIError(&resp.Diagnostics, "Setting the volume's protection failed", err)
			return
		}
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// refresh reads the volume into m; false when it is gone or on error.
func (r *volumeResource) refresh(ctx context.Context, m *volumeModel, diags *diagnosticsAlias) bool {
	id := parseID(diags, m.ID.ValueString())
	if diags.HasError() {
		return false
	}
	v, err := r.client.GetVolume(ctx, id)
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading volume failed", err)
		return false
	}
	r.fill(m, v)
	return true
}

func (r *volumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeModel
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

func (r *volumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state volumeModel
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

	var patch client.UpdateVolumeRequest
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
	if !plan.Filesystem.IsUnknown() && !plan.Filesystem.Equal(state.Filesystem) {
		v := plan.Filesystem.ValueString()
		patch.Filesystem, changed = &v, true
	}
	if !plan.ProjectID.IsUnknown() && !plan.ProjectID.IsNull() && !plan.ProjectID.Equal(state.ProjectID) {
		patch.ProjectID, changed = optionalInt64(plan.ProjectID), true
	}
	if changed {
		if _, err := r.client.UpdateVolume(ctx, id, patch); err != nil {
			addAPIError(&resp.Diagnostics, "Updating volume failed", err)
			return
		}
	}

	if plan.Size.ValueInt64() > state.Size.ValueInt64() {
		actionID, err := r.client.ResizeVolume(ctx, id, plan.Size.ValueInt64())
		if err == nil {
			err = r.client.WaitAction(ctx, actionID)
		}
		if err != nil {
			addAPIError(&resp.Diagnostics, "Resizing volume failed", err)
			return
		}
	}

	if !plan.ServerID.IsUnknown() && !plan.ServerID.Equal(state.ServerID) {
		if !state.ServerID.IsNull() {
			actionID, err := r.client.DetachVolume(ctx, id)
			if err == nil {
				err = r.client.WaitAction(ctx, actionID)
			}
			if err != nil {
				addAPIError(&resp.Diagnostics, "Detaching volume failed", err)
				return
			}
		}
		if !plan.ServerID.IsNull() {
			actionID, err := r.client.AttachVolume(ctx, id, plan.ServerID.ValueInt64())
			if err == nil {
				err = r.client.WaitAction(ctx, actionID)
			}
			if err != nil {
				addAPIError(&resp.Diagnostics, "Attaching volume failed", err)
				return
			}
		}
	}

	plan.ID = state.ID
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Volume disappeared", "The volume was not found after the update.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeModel
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
	v, err := r.client.GetVolume(ctx, id)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading volume failed", err)
		return
	}
	if v.ServerID != nil {
		actionID, err := r.client.DetachVolume(ctx, id)
		if err == nil {
			err = r.client.WaitAction(ctx, actionID)
		}
		if err != nil && !client.IsNotFound(err) {
			addAPIError(&resp.Diagnostics, "Detaching volume before deletion failed", err)
			return
		}
	}
	actionID, err := r.client.DeleteVolume(ctx, id)
	if client.IsNotFound(err) {
		return
	}
	if err == nil {
		err = r.client.WaitAction(ctx, actionID)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Deleting volume failed", err)
	}
}
