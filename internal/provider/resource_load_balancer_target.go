package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                   = (*lbTargetResource)(nil)
	_ resource.ResourceWithImportState    = (*lbTargetResource)(nil)
	_ resource.ResourceWithValidateConfig = (*lbTargetResource)(nil)
)

type lbTargetResource struct{ base }

func newLoadBalancerTargetResource() resource.Resource { return &lbTargetResource{} }

type lbTargetModel struct {
	ID             types.String   `tfsdk:"id"`
	LoadBalancerID types.Int64    `tfsdk:"load_balancer_id"`
	Type           types.String   `tfsdk:"type"`
	ServerID       types.Int64    `tfsdk:"server_id"`
	IP             types.String   `tfsdk:"ip"`
	LabelSelector  types.String   `tfsdk:"label_selector"`
	UsePrivateIP   types.Bool     `tfsdk:"use_private_ip"`
	TargetID       types.Int64    `tfsdk:"target_id"`
	Address        types.String   `tfsdk:"address"`
	HealthStatus   types.String   `tfsdk:"health_status"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *lbTargetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer_target"
}

func (r *lbTargetResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A target of a load balancer: a server, an IP, or every server matching a label selector (`add_target` / `remove_target` actions). Import ID: `<load_balancer_id>/<target_id>`.",
		Attributes: map[string]schema.Attribute{
			"id":               idAttribute("`<load_balancer_id>/<target_id>`."),
			"load_balancer_id": schema.Int64Attribute{Required: true, Description: "Load balancer.", PlanModifiers: replaceInt},
			"type": schema.StringAttribute{
				Required: true, Description: "`server`, `ip` or `label_selector`.", PlanModifiers: replaceStr,
				Validators: []validator.String{stringvalidator.OneOf("server", "ip", "label_selector")},
			},
			"server_id":      schema.Int64Attribute{Optional: true, Description: "Server, for type `server`.", PlanModifiers: replaceInt},
			"ip":             schema.StringAttribute{Optional: true, Description: "IP address, for type `ip`.", PlanModifiers: replaceStr},
			"label_selector": schema.StringAttribute{Optional: true, Description: "Label selector, for type `label_selector`.", PlanModifiers: replaceStr},
			"use_private_ip": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description:   "Reach the target over the load balancer's private network.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"target_id":     schema.Int64Attribute{Computed: true, Description: "Target ID.", PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"address":       computedString("Address the load balancer sends traffic to."),
			"health_status": computedString("`healthy`, `unhealthy` or `unknown`."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *lbTargetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m lbTargetModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Type.IsUnknown() {
		return
	}
	need := map[string]struct {
		attr string
		set  bool
	}{
		"server":         {"server_id", !m.ServerID.IsNull()},
		"ip":             {"ip", !m.IP.IsNull()},
		"label_selector": {"label_selector", !m.LabelSelector.IsNull()},
	}
	for typ, n := range need {
		if typ == m.Type.ValueString() && !n.set {
			resp.Diagnostics.AddAttributeError(path.Root(n.attr), "Missing target", "type "+typ+" needs "+n.attr+".")
		}
		if typ != m.Type.ValueString() && n.set {
			resp.Diagnostics.AddAttributeError(path.Root(n.attr), "Conflicting target", n.attr+" is only used with type "+typ+".")
		}
	}
}

func (r *lbTargetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	a, b := splitID(&resp.Diagnostics, req.ID, "<load_balancer_id>/<target_id>")
	if resp.Diagnostics.HasError() {
		return
	}
	lbID, tid := parseID(&resp.Diagnostics, a), parseID(&resp.Diagnostics, b)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("load_balancer_id"), lbID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target_id"), tid)...)
}

func (m *lbTargetModel) fill(t client.LBTarget) {
	m.TargetID = types.Int64Value(t.ID)
	m.Type = types.StringValue(t.Type)
	m.ServerID = ptrInt64Value(t.ServerID)
	m.IP = types.StringNull()
	if t.IP != "" {
		m.IP = types.StringValue(t.IP)
	}
	m.LabelSelector = types.StringNull()
	if t.LabelSelector != "" {
		m.LabelSelector = types.StringValue(t.LabelSelector)
	}
	m.UsePrivateIP = types.BoolValue(t.UsePrivateIP)
	m.Address = types.StringValue(t.Address)
	m.HealthStatus = types.StringValue(t.HealthStatus)
}

func (m lbTargetModel) matches(t client.LBTarget) bool {
	if t.Type != m.Type.ValueString() || t.UsePrivateIP != m.UsePrivateIP.ValueBool() {
		return false
	}
	switch t.Type {
	case "server":
		return t.ServerID != nil && *t.ServerID == m.ServerID.ValueInt64()
	case "ip":
		return t.IP == m.IP.ValueString()
	default:
		return t.LabelSelector == m.LabelSelector.ValueString()
	}
}

func (r *lbTargetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lbTargetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	lbID := plan.LoadBalancerID.ValueInt64()
	in := client.LBTargetInput{
		Type:          plan.Type.ValueString(),
		ServerID:      knownInt(plan.ServerID),
		IP:            knownStr(plan.IP),
		LabelSelector: knownStr(plan.LabelSelector),
		UsePrivateIP:  plan.UsePrivateIP.ValueBool(),
	}
	if err := r.wait(ctx)(r.client.AddLBTarget(ctx, lbID, in)); err != nil {
		addAPIError(&resp.Diagnostics, "Adding load balancer target failed", err)
		return
	}
	lb, err := r.client.GetLoadBalancer(ctx, lbID)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading load balancer failed", err)
		return
	}
	for _, t := range lb.Targets {
		if plan.matches(t) {
			plan.fill(t)
			plan.ID = types.StringValue(compositeID(strconv.FormatInt(lbID, 10), strconv.FormatInt(t.ID, 10)))
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			return
		}
	}
	resp.Diagnostics.AddError("Target not found", "The target was not found on the load balancer after it was added.")
}

func (r *lbTargetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lbTargetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	lb, err := r.client.GetLoadBalancer(ctx, state.LoadBalancerID.ValueInt64())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading load balancer failed", err)
		return
	}
	for _, t := range lb.Targets {
		if t.ID == state.TargetID.ValueInt64() {
			state.fill(t)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *lbTargetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan lbTargetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lbTargetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lbTargetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	err := r.wait(ctx)(r.client.RemoveLBTarget(ctx, state.LoadBalancerID.ValueInt64(), state.TargetID.ValueInt64()))
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Removing load balancer target failed", err)
	}
}
