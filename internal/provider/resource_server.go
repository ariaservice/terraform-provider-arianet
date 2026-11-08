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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*serverResource)(nil)
	_ resource.ResourceWithImportState = (*serverResource)(nil)
)

type serverResource struct{ base }

func newServerResource() resource.Resource { return &serverResource{} }

type serverModel struct {
	ID                types.String   `tfsdk:"id"`
	Name              types.String   `tfsdk:"name"`
	Datacenter        types.String   `tfsdk:"datacenter"`
	PlanID            types.Int64    `tfsdk:"plan_id"`
	Image             types.String   `tfsdk:"image"`
	SSHKeyIDs         types.Set      `tfsdk:"ssh_key_ids"`
	SSHKeys           types.List     `tfsdk:"ssh_keys"`
	UserData          types.String   `tfsdk:"user_data"`
	StartAfterCreate  types.Bool     `tfsdk:"start_after_create"`
	FirewallIDs       types.Set      `tfsdk:"firewall_ids"`
	PlacementGroupID  types.Int64    `tfsdk:"placement_group_id"`
	Backups           types.Bool     `tfsdk:"backups"`
	EnableIPv4        types.Bool     `tfsdk:"enable_ipv4"`
	EnableIPv6        types.Bool     `tfsdk:"enable_ipv6"`
	NetworkIDs        types.Set      `tfsdk:"network_ids"`
	Labels            types.Map      `tfsdk:"labels"`
	ProtectionDelete  types.Bool     `tfsdk:"protection_delete"`
	ProtectionRebuild types.Bool     `tfsdk:"protection_rebuild"`
	ProjectID         types.Int64    `tfsdk:"project_id"`
	KeepBackups       types.Bool     `tfsdk:"keep_backups"`
	DeleteSnapshots   types.Bool     `tfsdk:"delete_snapshots"`
	Status            types.String   `tfsdk:"status"`
	IPv4              types.String   `tfsdk:"ipv4_address"`
	IPv6              types.String   `tfsdk:"ipv6_address"`
	DiskGB            types.Int64    `tfsdk:"disk_gb"`
	RescueEnabled     types.Bool     `tfsdk:"rescue_enabled"`
	Locked            types.Bool     `tfsdk:"locked"`
	CreatedAt         types.String   `tfsdk:"created_at"`
	RootPassword      types.String   `tfsdk:"root_password"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *serverResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A cloud server. Maps to `/cloud/v1/servers`. Creating, resizing (`plan_id`) and deleting wait for the server's action to finish.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("Server ID."),
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Server name: letters, digits, dots and dashes, up to 63 characters.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 63)},
			},
			"datacenter": schema.StringAttribute{
				Required:      true,
				Description:   "Datacenter (location) code, see the `arianet_locations` data source. Changing it replaces the server.",
				PlanModifiers: replaceStr,
			},
			"plan_id": schema.Int64Attribute{
				Required:    true,
				Description: "Plan ID, see the `arianet_plans` data source. Changing it resizes the server (`change_plan`).",
			},
			"image": schema.StringAttribute{
				Required:      true,
				Description:   "Image to create the server from: the `image` of an `arianet_image(s)` data source (a system/app image slug, or a snapshot/backup reference). Changing it replaces the server.",
				PlanModifiers: replaceStr,
			},
			"ssh_key_ids": schema.SetAttribute{
				ElementType:   types.Int64Type,
				Optional:      true,
				Description:   "IDs of the account's saved SSH keys (`arianet_ssh_key`) installed at creation. Changing it replaces the server.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
			},
			"ssh_keys": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Public keys (OpenSSH lines) installed at creation in addition to `ssh_key_ids`. Changing it replaces the server.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"user_data": schema.StringAttribute{
				Optional:      true,
				Description:   "Cloud-init user data. Changing it replaces the server.",
				PlanModifiers: replaceStr,
			},
			"start_after_create": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the server is started once created. Only used at creation.",
			},
			"firewall_ids": schema.SetAttribute{
				ElementType:   types.Int64Type,
				Optional:      true,
				Computed:      true,
				Description:   "Firewalls applied directly to this server. Changes apply/remove the firewalls. Do not combine with `apply_to` of `arianet_firewall` or `arianet_firewall_attachment` for the same server.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"placement_group_id": schema.Int64Attribute{
				Optional:      true,
				Description:   "Placement group to create the server in. Changing it replaces the server.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"backups": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether daily backups are enabled (billed extra).",
			},
			"enable_ipv4": schema.BoolAttribute{
				Optional:      true,
				Description:   "Whether the server gets a public IPv4 address (API default: true). Changing it replaces the server.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"enable_ipv6": schema.BoolAttribute{
				Optional:      true,
				Description:   "Whether the server gets a public IPv6 address where the datacenter has IPv6. Changing it replaces the server.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"network_ids": schema.SetAttribute{
				ElementType:   types.Int64Type,
				Optional:      true,
				Description:   "Private networks the server is attached to at creation. Changing it replaces the server.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
			},
			"labels":            labelsAttribute(),
			"protection_delete": protectionDeleteAttribute(),
			"protection_rebuild": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the server is protected from being rebuilt.",
			},
			"project_id": projectIDAttribute(),
			"keep_backups": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "On destroy: keep the server's backups (they stay billed per GB, see `arianet_server_costs.kept_backups`). Sent as `?keep_backups=true`; set it and apply before destroying.",
			},
			"delete_snapshots": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "On destroy: also delete the server's snapshots, which otherwise stay billed. Sent as `?delete_snapshots=true`; set it and apply before destroying.",
			},
			"status":       computedString("Server status, e.g. `running` or `off`."),
			"ipv4_address": computedStableString("Public IPv4 address."),
			"ipv6_address": computedStableString("Public IPv6 address."),
			"disk_gb": schema.Int64Attribute{
				Computed:    true,
				Description: "Root disk size in GB.",
			},
			"rescue_enabled": schema.BoolAttribute{Computed: true, Description: "Whether rescue mode is on."},
			"locked":         schema.BoolAttribute{Computed: true, Description: "Whether the server is locked (e.g. by billing)."},
			"created_at":     computedStableString("Creation time (RFC 3339)."),
			"root_password": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "Root password returned once at creation (empty when SSH keys are used and the API returns none).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	body := client.CreateServerRequest{
		Datacenter: plan.Datacenter.ValueString(),
		Name:       plan.Name.ValueString(),
		PlanID:     plan.PlanID.ValueInt64(),
		Image:      plan.Image.ValueString(),
		SSHKeyIDs:  int64sFrom(ctx, plan.SSHKeyIDs, &resp.Diagnostics),
		SSHKeys:    stringsFromList(ctx, plan.SSHKeys, &resp.Diagnostics),
		UserData:   plan.UserData.ValueString(),
		Backups:    plan.Backups.ValueBool(),
		Labels:     labelsFrom(ctx, plan.Labels, &resp.Diagnostics),
		ProjectID:  optionalInt64(plan.ProjectID),
	}
	start := plan.StartAfterCreate.ValueBool()
	body.StartAfterCreate = &start
	body.FirewallIDs = int64sFrom(ctx, plan.FirewallIDs, &resp.Diagnostics)
	body.PlacementGroupID = optionalInt64(plan.PlacementGroupID)
	if !plan.EnableIPv4.IsNull() || !plan.EnableIPv6.IsNull() || !plan.NetworkIDs.IsNull() {
		n := &client.ServerNetworks{NetworkIDs: int64sFrom(ctx, plan.NetworkIDs, &resp.Diagnostics)}
		if !plan.EnableIPv4.IsNull() {
			v := plan.EnableIPv4.ValueBool()
			n.EnableIPv4 = &v
		}
		if !plan.EnableIPv6.IsNull() {
			v := plan.EnableIPv6.ValueBool()
			n.EnableIPv6 = &v
		}
		body.Networks = n
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.CreateServer(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating server failed", err)
		return
	}
	plan.ID = idString(res.Server.ID)
	plan.RootPassword = types.StringValue(res.RootPassword)
	r.fill(&plan, res.Server)
	plan.Backups = types.BoolValue(body.Backups)
	if plan.FirewallIDs.IsUnknown() {
		plan.FirewallIDs = int64SetValue(body.FirewallIDs)
	}
	// Saved before waiting so a failed or timed-out create is tainted, not lost.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	if err := r.client.WaitAction(ctx, res.ActionID); err != nil {
		addAPIError(&resp.Diagnostics, "Waiting for the server to be created failed", err)
		return
	}
	if plan.ProtectionDelete.ValueBool() || plan.ProtectionRebuild.ValueBool() {
		pd, pr := plan.ProtectionDelete.ValueBool(), plan.ProtectionRebuild.ValueBool()
		if _, err := r.client.UpdateServer(ctx, res.Server.ID, client.UpdateServerRequest{ProtectionDelete: &pd, ProtectionRebuild: &pr}); err != nil {
			addAPIError(&resp.Diagnostics, "Setting the server's protection failed", err)
			return
		}
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// fill copies the API view into the model.
func (r *serverResource) fill(m *serverModel, s client.Server) {
	m.ID = idString(s.ID)
	m.Name = types.StringValue(s.Name)
	m.Datacenter = types.StringValue(s.Datacenter)
	m.PlanID = types.Int64Value(s.PlanID)
	if m.Image.IsNull() || m.Image.IsUnknown() || m.Image.ValueString() == "" {
		m.Image = types.StringValue(s.Image)
	}
	m.Labels = labelsValue(s.Labels)
	m.ProtectionDelete = types.BoolValue(s.ProtectionDelete)
	m.ProtectionRebuild = types.BoolValue(s.ProtectionRebuild)
	m.ProjectID = types.Int64Value(s.ProjectID)
	m.Status = types.StringValue(s.Status)
	m.IPv4 = types.StringValue(s.IPv4)
	m.IPv6 = types.StringValue(s.IPv6)
	m.DiskGB = types.Int64Value(s.DiskGB)
	m.RescueEnabled = types.BoolValue(s.RescueEnabled)
	m.Locked = types.BoolValue(s.Locked)
	m.CreatedAt = timeValue(s.CreatedAt)
	if m.RootPassword.IsNull() || m.RootPassword.IsUnknown() {
		m.RootPassword = types.StringValue("")
	}
	if m.StartAfterCreate.IsNull() || m.StartAfterCreate.IsUnknown() {
		m.StartAfterCreate = types.BoolValue(true)
	}
	if m.KeepBackups.IsNull() || m.KeepBackups.IsUnknown() {
		m.KeepBackups = types.BoolValue(false)
	}
	if m.DeleteSnapshots.IsNull() || m.DeleteSnapshots.IsUnknown() {
		m.DeleteSnapshots = types.BoolValue(false)
	}
}

// refresh reads the server, its backup plan and its direct firewalls.
// It returns false when the server is gone.
func (r *serverResource) refresh(ctx context.Context, m *serverModel, diags *diagnosticsAlias) bool {
	id := parseID(diags, m.ID.ValueString())
	if diags.HasError() {
		return false
	}
	s, err := r.client.GetServer(ctx, id)
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading server failed", err)
		return false
	}
	r.fill(m, s)

	plan, err := r.client.ServerBackupPlan(ctx, id)
	if err != nil && !client.IsNotFound(err) {
		addAPIError(diags, "Reading the server's backup plan failed", err)
		return false
	}
	m.Backups = types.BoolValue(plan != nil)

	fws, err := r.client.ListFirewalls(ctx, "")
	if err != nil {
		addAPIError(diags, "Reading firewalls failed", err)
		return false
	}
	direct := []int64{}
	for _, f := range fws {
		for _, sid := range f.DirectServerIDs() {
			if sid == id {
				direct = append(direct, f.ID)
			}
		}
	}
	m.FirewallIDs = int64SetValue(direct)
	return true
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverModel
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

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serverModel
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

	// Settings: name, labels, protection, project.
	var patch client.UpdateServerRequest
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
	if !plan.ProtectionRebuild.Equal(state.ProtectionRebuild) {
		v := plan.ProtectionRebuild.ValueBool()
		patch.ProtectionRebuild, changed = &v, true
	}
	if !plan.ProjectID.IsUnknown() && !plan.ProjectID.IsNull() && !plan.ProjectID.Equal(state.ProjectID) {
		patch.ProjectID, changed = optionalInt64(plan.ProjectID), true
	}
	if changed {
		if _, err := r.client.UpdateServer(ctx, id, patch); err != nil {
			addAPIError(&resp.Diagnostics, "Updating server failed", err)
			return
		}
	}

	if !plan.PlanID.Equal(state.PlanID) {
		actionID, err := r.client.ChangeServerPlan(ctx, id, plan.PlanID.ValueInt64())
		if err == nil {
			err = r.client.WaitAction(ctx, actionID)
		}
		if err != nil {
			addAPIError(&resp.Diagnostics, "Changing the server's plan failed", err)
			return
		}
	}

	if !plan.Backups.Equal(state.Backups) {
		var actionID int64
		var err error
		if plan.Backups.ValueBool() {
			actionID, err = r.client.EnableServerBackup(ctx, id)
		} else {
			actionID, err = r.client.DisableServerBackup(ctx, id)
		}
		if err == nil {
			err = r.client.WaitAction(ctx, actionID)
		}
		if err != nil {
			addAPIError(&resp.Diagnostics, "Changing the server's backups failed", err)
			return
		}
	}

	if !plan.FirewallIDs.IsUnknown() && !plan.FirewallIDs.Equal(state.FirewallIDs) {
		add, remove := diffInt64(int64sFrom(ctx, state.FirewallIDs, &resp.Diagnostics), int64sFrom(ctx, plan.FirewallIDs, &resp.Diagnostics))
		for _, fid := range remove {
			actionID, err := r.client.RemoveFirewall(ctx, fid, []client.FirewallTarget{client.ServerTarget(id)})
			if err == nil {
				err = r.client.WaitAction(ctx, actionID)
			}
			if err != nil && !client.IsNotFound(err) {
				addAPIError(&resp.Diagnostics, "Removing a firewall from the server failed", err)
				return
			}
		}
		for _, fid := range add {
			actionID, err := r.client.ApplyFirewall(ctx, fid, []client.FirewallTarget{client.ServerTarget(id)})
			if err == nil {
				err = r.client.WaitAction(ctx, actionID)
			}
			if err != nil {
				addAPIError(&resp.Diagnostics, "Applying a firewall to the server failed", err)
				return
			}
		}
	}

	plan.ID = state.ID
	plan.RootPassword = state.RootPassword
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Server disappeared", "The server was not found after the update.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverModel
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
	actionID, err := r.client.DeleteServer(ctx, id, client.DeleteServerOptions{
		KeepBackups:     state.KeepBackups.ValueBool(),
		DeleteSnapshots: state.DeleteSnapshots.ValueBool(),
	})
	if client.IsNotFound(err) {
		return
	}
	if err == nil {
		err = r.client.WaitAction(ctx, actionID)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Deleting server failed", err)
	}
}
