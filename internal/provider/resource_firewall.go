package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*firewallResource)(nil)
	_ resource.ResourceWithImportState = (*firewallResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*firewallResource)(nil)
)

type firewallResource struct{ base }

func newFirewallResource() resource.Resource { return &firewallResource{} }

type firewallModel struct {
	ID               types.String   `tfsdk:"id"`
	Name             types.String   `tfsdk:"name"`
	Rules            []ruleModel    `tfsdk:"rule"`
	ApplyTo          types.Set      `tfsdk:"apply_to"`
	Labels           types.Map      `tfsdk:"labels"`
	ProtectionDelete types.Bool     `tfsdk:"protection_delete"`
	ProjectID        types.Int64    `tfsdk:"project_id"`
	ServerIDs        types.Set      `tfsdk:"server_ids"`
	CreatedAt        types.String   `tfsdk:"created_at"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

type ruleModel struct {
	Direction      types.String `tfsdk:"direction"`
	Protocol       types.String `tfsdk:"protocol"`
	Port           types.String `tfsdk:"port"`
	SourceIPs      types.List   `tfsdk:"source_ips"`
	DestinationIPs types.List   `tfsdk:"destination_ips"`
	Description    types.String `tfsdk:"description"`
}

var applyToAttrTypes = map[string]attr.Type{
	"server":         types.Int64Type,
	"label_selector": types.StringType,
}

type applyToModel struct {
	Server        types.Int64  `tfsdk:"server"`
	LabelSelector types.String `tfsdk:"label_selector"`
}

func (r *firewallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall"
}

func (r *firewallResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A firewall: its rules and what it is applied to (servers and label selectors). Maps to `/cloud/v1/firewalls` (`PUT` for name and rules, `apply_to_resources`/`remove_from_resources` actions for targets).",
		Attributes: map[string]schema.Attribute{
			"id":   idAttribute("Firewall ID."),
			"name": schema.StringAttribute{Required: true, Description: "Firewall name: letters, digits, dots and dashes, up to 63 characters."},
			"apply_to": schema.SetNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "What the firewall is applied to: each element sets exactly one of `server` (a server ID) or `label_selector` (e.g. `env=prod`). " +
					"When omitted, targets are left as they are (e.g. managed by `arianet_firewall_attachment` or a server's `firewall_ids`); set `apply_to = []` to remove every target.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"server":         schema.Int64Attribute{Optional: true, Description: "Server ID."},
						"label_selector": schema.StringAttribute{Optional: true, Description: "Label selector matching servers."},
					},
					Validators: []validator.Object{objectvalidator.AtLeastOneOf(
						path.MatchRelative().AtName("server"), path.MatchRelative().AtName("label_selector"))},
				},
			},
			"labels":            labelsAttribute(),
			"protection_delete": protectionDeleteAttribute(),
			"project_id":        projectIDAttribute(),
			"server_ids": schema.SetAttribute{
				ElementType: types.Int64Type,
				Computed:    true,
				Description: "Every server the firewall is on, directly or through a label selector.",
			},
			"created_at": computedStableString("Creation time (RFC 3339)."),
		},
		Blocks: map[string]schema.Block{
			"rule": schema.ListNestedBlock{
				Description: "A rule. Rules are replaced as a whole on change.",
				NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
					"direction": schema.StringAttribute{Required: true, Description: "`in` or `out`.", Validators: []validator.String{stringvalidator.OneOf("in", "out")}},
					"protocol": schema.StringAttribute{Required: true, Description: "`tcp`, `udp`, `icmp`, `esp` or `gre`.",
						Validators: []validator.String{stringvalidator.OneOf("tcp", "udp", "icmp", "esp", "gre")}},
					"port":            schema.StringAttribute{Optional: true, Description: "Port or range (`80`, `1024-65535`, `any`) for tcp and udp."},
					"source_ips":      schema.ListAttribute{ElementType: types.StringType, Optional: true, Description: "Source CIDRs (direction `in`)."},
					"destination_ips": schema.ListAttribute{ElementType: types.StringType, Optional: true, Description: "Destination CIDRs (direction `out`)."},
					"description":     schema.StringAttribute{Optional: true, Description: "Description of the rule."},
				}},
			},
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

// ModifyPlan keeps server_ids known when the targets do not change: Terraform
// proposes a null apply_to when it is not configured, which would otherwise
// mark server_ids unknown on every plan once a server is attached elsewhere.
func (r *firewallResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.ServerIDs.IsUnknown() && plan.ApplyTo.Equal(state.ApplyTo) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("server_ids"), state.ServerIDs)...)
	}
}

func (r *firewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func rulesToAPI(ctx context.Context, rules []ruleModel, diags *diagnosticsAlias) []client.FirewallRule {
	out := make([]client.FirewallRule, 0, len(rules))
	for _, rm := range rules {
		fr := client.FirewallRule{
			Direction:      rm.Direction.ValueString(),
			Protocol:       rm.Protocol.ValueString(),
			SourceIPs:      stringsFromList(ctx, rm.SourceIPs, diags),
			DestinationIPs: stringsFromList(ctx, rm.DestinationIPs, diags),
		}
		if !rm.Port.IsNull() && !rm.Port.IsUnknown() {
			p := rm.Port.ValueString()
			fr.Port = &p
		}
		if !rm.Description.IsNull() && !rm.Description.IsUnknown() {
			d := rm.Description.ValueString()
			fr.Description = &d
		}
		out = append(out, fr)
	}
	return out
}

func rulesFromAPI(rules []client.FirewallRule) []ruleModel {
	if len(rules) == 0 {
		return nil
	}
	out := make([]ruleModel, 0, len(rules))
	for _, fr := range rules {
		rm := ruleModel{
			Direction:      types.StringValue(fr.Direction),
			Protocol:       types.StringValue(fr.Protocol),
			Port:           ptrStringValue(fr.Port),
			Description:    ptrStringValue(fr.Description),
			SourceIPs:      types.ListNull(types.StringType),
			DestinationIPs: types.ListNull(types.StringType),
		}
		if len(fr.SourceIPs) > 0 {
			rm.SourceIPs = stringListValue(fr.SourceIPs)
		}
		if len(fr.DestinationIPs) > 0 {
			rm.DestinationIPs = stringListValue(fr.DestinationIPs)
		}
		out = append(out, rm)
	}
	return out
}

// targetsFrom splits apply_to into server IDs and selectors.
func targetsFrom(ctx context.Context, set types.Set, diags *diagnosticsAlias) (servers []int64, selectors []string) {
	if set.IsNull() || set.IsUnknown() {
		return nil, nil
	}
	var items []applyToModel
	diags.Append(set.ElementsAs(ctx, &items, false)...)
	for _, it := range items {
		if !it.Server.IsNull() && !it.Server.IsUnknown() {
			servers = append(servers, it.Server.ValueInt64())
		}
		if !it.LabelSelector.IsNull() && !it.LabelSelector.IsUnknown() {
			selectors = append(selectors, it.LabelSelector.ValueString())
		}
	}
	return servers, selectors
}

func applyToValue(f client.Firewall) types.Set {
	vals := []attr.Value{}
	for _, id := range f.DirectServerIDs() {
		vals = append(vals, types.ObjectValueMust(applyToAttrTypes, map[string]attr.Value{
			"server": types.Int64Value(id), "label_selector": types.StringNull(),
		}))
	}
	for _, sel := range f.Selectors() {
		vals = append(vals, types.ObjectValueMust(applyToAttrTypes, map[string]attr.Value{
			"server": types.Int64Null(), "label_selector": types.StringValue(sel),
		}))
	}
	return types.SetValueMust(types.ObjectType{AttrTypes: applyToAttrTypes}, vals)
}

func targetsList(servers []int64, selectors []string) []client.FirewallTarget {
	out := []client.FirewallTarget{}
	for _, id := range servers {
		out = append(out, client.ServerTarget(id))
	}
	for _, sel := range selectors {
		out = append(out, client.SelectorTarget(sel))
	}
	return out
}

func (r *firewallResource) fill(m *firewallModel, f client.Firewall) {
	m.ID = idString(f.ID)
	m.Name = types.StringValue(f.Name)
	m.Rules = rulesFromAPI(f.Rules)
	m.ApplyTo = applyToValue(f)
	m.Labels = labelsValue(f.Labels)
	m.ProtectionDelete = types.BoolValue(f.ProtectionDelete)
	m.ProjectID = types.Int64Value(f.ProjectID)
	ids := append([]int64{}, f.ServerIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	m.ServerIDs = int64SetValue(ids)
	m.CreatedAt = timeValue(f.CreatedAt)
}

func (r *firewallResource) refresh(ctx context.Context, m *firewallModel, diags *diagnosticsAlias) bool {
	id := parseID(diags, m.ID.ValueString())
	if diags.HasError() {
		return false
	}
	f, err := r.client.GetFirewall(ctx, id)
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading firewall failed", err)
		return false
	}
	r.fill(m, f)
	return true
}

func (r *firewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	servers, selectors := targetsFrom(ctx, plan.ApplyTo, &resp.Diagnostics)
	body := client.CreateFirewallRequest{
		Name:      plan.Name.ValueString(),
		Rules:     rulesToAPI(ctx, plan.Rules, &resp.Diagnostics),
		ServerIDs: servers,
		Labels:    labelsFrom(ctx, plan.Labels, &resp.Diagnostics),
		ProjectID: optionalInt64(plan.ProjectID),
	}
	if resp.Diagnostics.HasError() {
		return
	}
	f, actionID, err := r.client.CreateFirewall(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating firewall failed", err)
		return
	}
	userRules := plan.Rules
	r.fill(&plan, f)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if err := r.client.WaitAction(ctx, actionID); err != nil {
		addAPIError(&resp.Diagnostics, "Waiting for the firewall to be applied failed", err)
		return
	}
	if len(selectors) > 0 {
		actionID, err := r.client.ApplyFirewall(ctx, f.ID, targetsList(nil, selectors))
		if err == nil {
			err = r.client.WaitAction(ctx, actionID)
		}
		if err != nil {
			addAPIError(&resp.Diagnostics, "Applying the firewall to label selectors failed", err)
			return
		}
	}
	if plan.ProtectionDelete.ValueBool() != f.ProtectionDelete {
		pd := plan.ProtectionDelete.ValueBool()
		if _, err := r.client.UpdateFirewall(ctx, f.ID, client.UpdateRequest{ProtectionDelete: &pd}); err != nil {
			addAPIError(&resp.Diagnostics, "Setting the firewall's protection failed", err)
			return
		}
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	keepConfigShapes(&plan, userRules)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// keepConfigShapes keeps the configuration's own spelling of rules when the
// API's answer means the same (empty lists vs null), so the plan stays clean.
func keepConfigShapes(m *firewallModel, configRules []ruleModel) {
	if len(configRules) == len(m.Rules) {
		for i := range m.Rules {
			c := configRules[i]
			if m.Rules[i].SourceIPs.IsNull() && !c.SourceIPs.IsNull() && len(c.SourceIPs.Elements()) == 0 {
				m.Rules[i].SourceIPs = c.SourceIPs
			}
			if m.Rules[i].DestinationIPs.IsNull() && !c.DestinationIPs.IsNull() && len(c.DestinationIPs.Elements()) == 0 {
				m.Rules[i].DestinationIPs = c.DestinationIPs
			}
		}
	}
}

func (r *firewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state firewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	prior := state.Rules
	if !r.refresh(ctx, &state, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	keepConfigShapes(&state, prior)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *firewallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state firewallModel
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

	planRules := rulesToAPI(ctx, plan.Rules, &resp.Diagnostics)
	stateRules := rulesToAPI(ctx, state.Rules, &resp.Diagnostics)
	if !plan.Name.Equal(state.Name) || !rulesEqual(planRules, stateRules) {
		_, actionID, err := r.client.SetFirewallRules(ctx, id, plan.Name.ValueString(), planRules)
		if err == nil {
			err = r.client.WaitAction(ctx, actionID)
		}
		if err != nil {
			addAPIError(&resp.Diagnostics, "Updating firewall rules failed", err)
			return
		}
	}

	patch, changed := networkingPatch(ctx, types.StringUnknown(), state.Name, plan.Labels, state.Labels, plan.ProtectionDelete, state.ProtectionDelete, plan.ProjectID, state.ProjectID, &resp.Diagnostics)
	if changed {
		if _, err := r.client.UpdateFirewall(ctx, id, patch); err != nil {
			addAPIError(&resp.Diagnostics, "Updating firewall failed", err)
			return
		}
	}

	if !plan.ApplyTo.IsUnknown() && !plan.ApplyTo.Equal(state.ApplyTo) {
		wantS, wantL := targetsFrom(ctx, plan.ApplyTo, &resp.Diagnostics)
		haveS, haveL := targetsFrom(ctx, state.ApplyTo, &resp.Diagnostics)
		addS, remS := diffInt64(haveS, wantS)
		addL, remL := diffStrings(haveL, wantL)
		if err := r.wait(ctx)(r.client.RemoveFirewall(ctx, id, targetsList(remS, remL))); err != nil {
			addAPIError(&resp.Diagnostics, "Removing firewall targets failed", err)
			return
		}
		if err := r.wait(ctx)(r.client.ApplyFirewall(ctx, id, targetsList(addS, addL))); err != nil {
			addAPIError(&resp.Diagnostics, "Applying firewall targets failed", err)
			return
		}
	}

	userRules := plan.Rules
	plan.ID = state.ID
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Firewall disappeared", "The firewall was not found after the update.")
		}
		return
	}
	keepConfigShapes(&plan, userRules)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func rulesEqual(a, b []client.FirewallRule) bool {
	if len(a) != len(b) {
		return false
	}
	str := func(p *string) string {
		if p == nil {
			return "\x00"
		}
		return *p
	}
	eq := func(x, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	for i := range a {
		if a[i].Direction != b[i].Direction || a[i].Protocol != b[i].Protocol || str(a[i].Port) != str(b[i].Port) ||
			str(a[i].Description) != str(b[i].Description) || !eq(a[i].SourceIPs, b[i].SourceIPs) || !eq(a[i].DestinationIPs, b[i].DestinationIPs) {
			return false
		}
	}
	return true
}

func (r *firewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state firewallModel
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
	actionID, err := r.client.DeleteFirewall(ctx, id)
	if client.IsNotFound(err) {
		return
	}
	if err == nil {
		err = r.client.WaitAction(ctx, actionID)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Deleting firewall failed", err)
	}
}
