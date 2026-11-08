package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                     = (*firewallAttachmentResource)(nil)
	_ resource.ResourceWithImportState      = (*firewallAttachmentResource)(nil)
	_ resource.ResourceWithConfigValidators = (*firewallAttachmentResource)(nil)
)

type firewallAttachmentResource struct{ base }

func newFirewallAttachmentResource() resource.Resource { return &firewallAttachmentResource{} }

type firewallAttachmentModel struct {
	ID             types.String   `tfsdk:"id"`
	FirewallID     types.Int64    `tfsdk:"firewall_id"`
	ServerIDs      types.Set      `tfsdk:"server_ids"`
	LabelSelectors types.Set      `tfsdk:"label_selectors"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *firewallAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_attachment"
}

func (r *firewallAttachmentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Applies a firewall to servers and/or label selectors (`apply_to_resources` / `remove_from_resources` actions). Only the targets listed here are managed. Do not combine with `apply_to` of the same `arianet_firewall`. The ID is the firewall ID.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("The firewall ID."),
			"firewall_id": schema.Int64Attribute{
				Required: true, Description: "Firewall to apply.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"server_ids": schema.SetAttribute{
				ElementType: types.Int64Type, Optional: true,
				Description: "Servers to apply the firewall to.",
			},
			"label_selectors": schema.SetAttribute{
				ElementType: types.StringType, Optional: true,
				Description: "Label selectors whose servers get the firewall, e.g. `env=prod`.",
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *firewallAttachmentResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.AtLeastOneOf(path.MatchRoot("server_ids"), path.MatchRoot("label_selectors")),
	}
}

func (r *firewallAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := parseID(&resp.Diagnostics, req.ID)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("firewall_id"), id)...)
}

func (r *firewallAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan firewallAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	fid := plan.FirewallID.ValueInt64()
	servers := int64sFrom(ctx, plan.ServerIDs, &resp.Diagnostics)
	selectors := stringsFromSet(ctx, plan.LabelSelectors, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.wait(ctx)(r.client.ApplyFirewall(ctx, fid, targetsList(servers, selectors))); err != nil {
		addAPIError(&resp.Diagnostics, "Applying firewall failed", err)
		return
	}
	plan.ID = idString(fid)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state firewallAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.client.GetFirewall(ctx, state.FirewallID.ValueInt64())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading firewall failed", err)
		return
	}
	direct, selectors := f.DirectServerIDs(), f.Selectors()
	if state.ServerIDs.IsNull() && state.LabelSelectors.IsNull() {
		// Import: adopt every target.
		if len(direct) > 0 {
			state.ServerIDs = int64SetValue(direct)
		}
		if len(selectors) > 0 {
			state.LabelSelectors = stringSetValue(selectors)
		}
	} else {
		// Keep only managed targets that are still applied.
		if !state.ServerIDs.IsNull() {
			have := map[int64]bool{}
			for _, id := range direct {
				have[id] = true
			}
			kept := []int64{}
			for _, id := range int64sFrom(ctx, state.ServerIDs, &resp.Diagnostics) {
				if have[id] {
					kept = append(kept, id)
				}
			}
			state.ServerIDs = int64SetValue(kept)
		}
		if !state.LabelSelectors.IsNull() {
			have := map[string]bool{}
			for _, s := range selectors {
				have[s] = true
			}
			kept := []string{}
			for _, s := range stringsFromSet(ctx, state.LabelSelectors, &resp.Diagnostics) {
				if have[s] {
					kept = append(kept, s)
				}
			}
			state.LabelSelectors = stringSetValue(kept)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *firewallAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state firewallAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Update(ctx, defaultUpdateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	fid := state.FirewallID.ValueInt64()
	addS, remS := diffInt64(int64sFrom(ctx, state.ServerIDs, &resp.Diagnostics), int64sFrom(ctx, plan.ServerIDs, &resp.Diagnostics))
	addL, remL := diffStrings(stringsFromSet(ctx, state.LabelSelectors, &resp.Diagnostics), stringsFromSet(ctx, plan.LabelSelectors, &resp.Diagnostics))
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.wait(ctx)(r.client.RemoveFirewall(ctx, fid, targetsList(remS, remL))); err != nil {
		addAPIError(&resp.Diagnostics, "Removing firewall targets failed", err)
		return
	}
	if err := r.wait(ctx)(r.client.ApplyFirewall(ctx, fid, targetsList(addS, addL))); err != nil {
		addAPIError(&resp.Diagnostics, "Applying firewall targets failed", err)
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state firewallAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	servers := int64sFrom(ctx, state.ServerIDs, &resp.Diagnostics)
	selectors := stringsFromSet(ctx, state.LabelSelectors, &resp.Diagnostics)
	err := r.wait(ctx)(r.client.RemoveFirewall(ctx, state.FirewallID.ValueInt64(), targetsList(servers, selectors)))
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Removing firewall targets failed", err)
	}
}
