package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*lbServiceResource)(nil)
	_ resource.ResourceWithImportState = (*lbServiceResource)(nil)
)

type lbServiceResource struct{ base }

func newLoadBalancerServiceResource() resource.Resource { return &lbServiceResource{} }

type lbServiceModel struct {
	ID              types.String   `tfsdk:"id"`
	LoadBalancerID  types.Int64    `tfsdk:"load_balancer_id"`
	Protocol        types.String   `tfsdk:"protocol"`
	ListenPort      types.Int64    `tfsdk:"listen_port"`
	DestinationPort types.Int64    `tfsdk:"destination_port"`
	Proxyprotocol   types.Bool     `tfsdk:"proxyprotocol"`
	HealthCheck     types.Object   `tfsdk:"health_check"`
	HTTP            types.Object   `tfsdk:"http"`
	Timeouts        timeouts.Value `tfsdk:"timeouts"`
}

type lbHealthModel struct {
	Protocol types.String `tfsdk:"protocol"`
	Port     types.Int64  `tfsdk:"port"`
	Interval types.Int64  `tfsdk:"interval"`
	Timeout  types.Int64  `tfsdk:"timeout"`
	Retries  types.Int64  `tfsdk:"retries"`
	HTTP     types.Object `tfsdk:"http"`
}

type lbHealthHTTPModel struct {
	Domain      types.String `tfsdk:"domain"`
	Path        types.String `tfsdk:"path"`
	StatusCodes types.List   `tfsdk:"status_codes"`
	TLS         types.Bool   `tfsdk:"tls"`
}

type lbServiceHTTPModel struct {
	StickySessions types.Bool   `tfsdk:"sticky_sessions"`
	CookieName     types.String `tfsdk:"cookie_name"`
	RedirectHTTP   types.Bool   `tfsdk:"redirect_http"`
	RedirectHost   types.String `tfsdk:"redirect_host"`
}

var lbHealthHTTPAttrTypes = map[string]attr.Type{
	"domain": types.StringType, "path": types.StringType,
	"status_codes": types.ListType{ElemType: types.StringType}, "tls": types.BoolType,
}

var lbHealthAttrTypes = map[string]attr.Type{
	"protocol": types.StringType, "port": types.Int64Type, "interval": types.Int64Type,
	"timeout": types.Int64Type, "retries": types.Int64Type,
	"http": types.ObjectType{AttrTypes: lbHealthHTTPAttrTypes},
}

var lbServiceHTTPAttrTypes = map[string]attr.Type{
	"sticky_sessions": types.BoolType, "cookie_name": types.StringType,
	"redirect_http": types.BoolType, "redirect_host": types.StringType,
}

func (r *lbServiceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer_service"
}

func optComputedInt(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{Optional: true, Computed: true, Description: desc,
		PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}}
}

func optComputedStr(desc string) schema.StringAttribute {
	return schema.StringAttribute{Optional: true, Computed: true, Description: desc}
}

func (r *lbServiceResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A listener (service) of a load balancer, managed with the `add_service`, `update_service` and `delete_service` actions. Import ID: `<load_balancer_id>/<listen_port>`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("`<load_balancer_id>/<listen_port>`."),
			"load_balancer_id": schema.Int64Attribute{
				Required: true, Description: "Load balancer the service belongs to.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"protocol": schema.StringAttribute{
				Required: true, Description: "`tcp`, `http` or `https` (TLS passthrough).",
				Validators: []validator.String{stringvalidator.OneOf("tcp", "http", "https")},
			},
			"listen_port": schema.Int64Attribute{
				Required: true, Description: "Port the load balancer listens on. Changing it replaces the service.",
				Validators:    []validator.Int64{int64validator.Between(1, 65535)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"destination_port": schema.Int64Attribute{
				Optional: true, Computed: true, Description: "Port on the targets. Defaults to the API's choice (the listen port).",
				Validators:    []validator.Int64{int64validator.Between(1, 65535)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"proxyprotocol": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Whether the PROXY protocol header is sent to targets.",
			},
			"health_check": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				Description:   "Health check of the targets. Unset fields take the API's defaults.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"protocol": optComputedStr("`tcp` or `http`."),
					"port":     optComputedInt("Port checked on the targets."),
					"interval": optComputedInt("Seconds between checks."),
					"timeout":  optComputedInt("Seconds before a check fails."),
					"retries":  optComputedInt("Failed checks before a target is unhealthy."),
					"http": schema.SingleNestedAttribute{
						Optional: true, Computed: true, Description: "HTTP options of an `http` check.",
						Attributes: map[string]schema.Attribute{
							"domain":       optComputedStr("Host header."),
							"path":         optComputedStr("Request path."),
							"status_codes": schema.ListAttribute{ElementType: types.StringType, Optional: true, Computed: true, Description: "Accepted status codes, e.g. `[\"2??\", \"301\"]`."},
							"tls":          schema.BoolAttribute{Optional: true, Computed: true, Description: "Check over HTTPS (the certificate is not verified)."},
						},
					},
				},
			},
			"http": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				Description:   "Options of `http`/`https` services.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"sticky_sessions": schema.BoolAttribute{Optional: true, Computed: true, Description: "Keep a client on one target (cookie for http, source address for https)."},
					"cookie_name":     optComputedStr("For http: the application's cookie to stick on. Empty: the load balancer sets its own."),
					"redirect_http":   schema.BoolAttribute{Optional: true, Computed: true, Description: "For https: answer plain http on port 80 with a redirect."},
					"redirect_host":   optComputedStr("For https with `redirect_http`: the host clients are redirected to."),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *lbServiceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	a, b := splitID(&resp.Diagnostics, req.ID, "<load_balancer_id>/<listen_port>")
	if resp.Diagnostics.HasError() {
		return
	}
	lbID, port := parseID(&resp.Diagnostics, a), parseID(&resp.Diagnostics, b)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("load_balancer_id"), lbID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("listen_port"), port)...)
}

func knownInt(v types.Int64) int64 {
	if v.IsNull() || v.IsUnknown() {
		return 0
	}
	return v.ValueInt64()
}

func knownStr(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

func knownBool(v types.Bool) bool {
	return !v.IsNull() && !v.IsUnknown() && v.ValueBool()
}

func usable(o types.Object) bool { return !o.IsNull() && !o.IsUnknown() }

// toAPI builds the API service; unknown fields are left zero for the API's defaults.
func (m lbServiceModel) toAPI(ctx context.Context, diags *diagnosticsAlias) client.LBService {
	svc := client.LBService{
		Protocol:        m.Protocol.ValueString(),
		ListenPort:      m.ListenPort.ValueInt64(),
		DestinationPort: knownInt(m.DestinationPort),
		Proxyprotocol:   knownBool(m.Proxyprotocol),
	}
	if usable(m.HealthCheck) {
		var hc lbHealthModel
		diags.Append(m.HealthCheck.As(ctx, &hc, basetypes.ObjectAsOptions{UnhandledUnknownAsEmpty: true})...)
		svc.HealthCheck = client.LBHealthCheck{
			Protocol: knownStr(hc.Protocol), Port: knownInt(hc.Port), Interval: knownInt(hc.Interval),
			Timeout: knownInt(hc.Timeout), Retries: knownInt(hc.Retries),
		}
		if usable(hc.HTTP) {
			var hh lbHealthHTTPModel
			diags.Append(hc.HTTP.As(ctx, &hh, basetypes.ObjectAsOptions{UnhandledUnknownAsEmpty: true})...)
			svc.HealthCheck.HTTP = &client.LBHealthHTTP{
				Domain: knownStr(hh.Domain), Path: knownStr(hh.Path), TLS: knownBool(hh.TLS),
			}
			if !hh.StatusCodes.IsNull() && !hh.StatusCodes.IsUnknown() {
				svc.HealthCheck.HTTP.StatusCodes = stringsFromList(ctx, hh.StatusCodes, diags)
			}
		}
	}
	if usable(m.HTTP) {
		var h lbServiceHTTPModel
		diags.Append(m.HTTP.As(ctx, &h, basetypes.ObjectAsOptions{UnhandledUnknownAsEmpty: true})...)
		svc.HTTP = &client.LBServiceHTTP{
			StickySessions: knownBool(h.StickySessions), CookieName: knownStr(h.CookieName),
			RedirectHTTP: knownBool(h.RedirectHTTP), RedirectHost: knownStr(h.RedirectHost),
		}
	}
	return svc
}

func (m *lbServiceModel) fill(svc client.LBService) {
	m.Protocol = types.StringValue(svc.Protocol)
	m.ListenPort = types.Int64Value(svc.ListenPort)
	m.DestinationPort = types.Int64Value(svc.DestinationPort)
	m.Proxyprotocol = types.BoolValue(svc.Proxyprotocol)
	hh := types.ObjectNull(lbHealthHTTPAttrTypes)
	if h := svc.HealthCheck.HTTP; h != nil {
		codes := h.StatusCodes
		if codes == nil {
			codes = []string{}
		}
		hh = types.ObjectValueMust(lbHealthHTTPAttrTypes, map[string]attr.Value{
			"domain": types.StringValue(h.Domain), "path": types.StringValue(h.Path),
			"status_codes": stringListValue(codes), "tls": types.BoolValue(h.TLS),
		})
	}
	m.HealthCheck = types.ObjectValueMust(lbHealthAttrTypes, map[string]attr.Value{
		"protocol": types.StringValue(svc.HealthCheck.Protocol), "port": types.Int64Value(svc.HealthCheck.Port),
		"interval": types.Int64Value(svc.HealthCheck.Interval), "timeout": types.Int64Value(svc.HealthCheck.Timeout),
		"retries": types.Int64Value(svc.HealthCheck.Retries), "http": hh,
	})
	m.HTTP = types.ObjectNull(lbServiceHTTPAttrTypes)
	if h := svc.HTTP; h != nil {
		m.HTTP = types.ObjectValueMust(lbServiceHTTPAttrTypes, map[string]attr.Value{
			"sticky_sessions": types.BoolValue(h.StickySessions), "cookie_name": types.StringValue(h.CookieName),
			"redirect_http": types.BoolValue(h.RedirectHTTP), "redirect_host": types.StringValue(h.RedirectHost),
		})
	}
}

func (r *lbServiceResource) refresh(ctx context.Context, m *lbServiceModel, diags *diagnosticsAlias) bool {
	lb, err := r.client.GetLoadBalancer(ctx, m.LoadBalancerID.ValueInt64())
	if client.IsNotFound(err) {
		return false
	}
	if err != nil {
		addAPIError(diags, "Reading load balancer failed", err)
		return false
	}
	for _, svc := range lb.Services {
		if svc.ListenPort == m.ListenPort.ValueInt64() {
			m.fill(svc)
			return true
		}
	}
	return false
}

func (r *lbServiceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lbServiceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	svc := plan.toAPI(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	lbID := plan.LoadBalancerID.ValueInt64()
	if err := r.wait(ctx)(r.client.AddLBService(ctx, lbID, svc)); err != nil {
		addAPIError(&resp.Diagnostics, "Adding load balancer service failed", err)
		return
	}
	plan.ID = types.StringValue(compositeID(strconv.FormatInt(lbID, 10), strconv.FormatInt(svc.ListenPort, 10)))
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Service not found", "The service was not found on the load balancer after it was added.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lbServiceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lbServiceModel
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

func (r *lbServiceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state lbServiceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Update(ctx, defaultUpdateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	svc := plan.toAPI(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	lbID := state.LoadBalancerID.ValueInt64()
	if err := r.wait(ctx)(r.client.UpdateLBService(ctx, lbID, state.ListenPort.ValueInt64(), svc)); err != nil {
		addAPIError(&resp.Diagnostics, "Updating load balancer service failed", err)
		return
	}
	plan.ID = state.ID
	if !r.refresh(ctx, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Service not found", "The service was not found on the load balancer after the update.")
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lbServiceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lbServiceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	err := r.wait(ctx)(r.client.DeleteLBService(ctx, state.LoadBalancerID.ValueInt64(), state.ListenPort.ValueInt64()))
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Deleting load balancer service failed", err)
	}
}
