package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*networkRouteResource)(nil)
	_ resource.ResourceWithImportState = (*networkRouteResource)(nil)
)

type networkRouteResource struct{ base }

func newNetworkRouteResource() resource.Resource { return &networkRouteResource{} }

type networkRouteModel struct {
	ID          types.String   `tfsdk:"id"`
	NetworkID   types.Int64    `tfsdk:"network_id"`
	Destination types.String   `tfsdk:"destination"`
	Gateway     types.String   `tfsdk:"gateway"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}

func (r *networkRouteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network_route"
}

func (r *networkRouteResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A static route of a private network. The API replaces a network's whole route set (`PUT /networks/{id}/routes`); this resource reads the set, adds or removes its own route and writes it back. Import ID: `<network_id>/<destination>`, e.g. `12/10.100.1.0/24`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("`<network_id>/<destination>`."),
			"network_id": schema.Int64Attribute{
				Required: true, Description: "Network the route belongs to.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"destination": schema.StringAttribute{Required: true, Description: "Destination range in CIDR notation.", PlanModifiers: replaceStr},
			"gateway":     schema.StringAttribute{Required: true, Description: "Next hop: an address inside the network.", PlanModifiers: replaceStr},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *networkRouteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	a, b := splitID(&resp.Diagnostics, req.ID, "<network_id>/<destination>")
	if resp.Diagnostics.HasError() {
		return
	}
	nid := parseID(&resp.Diagnostics, a)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), nid)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("destination"), b)...)
}

// mutate rewrites the network's route set under a per-network lock.
func (r *networkRouteResource) mutate(ctx context.Context, networkID int64, change func([]client.Route) []client.Route) error {
	unlock := r.locks.Lock("network-routes-" + strconv.FormatInt(networkID, 10))
	defer unlock()
	n, err := r.client.GetNetwork(ctx, networkID)
	if err != nil {
		return err
	}
	actionID, err := r.client.SetRoutes(ctx, networkID, change(n.Routes))
	if err != nil {
		return err
	}
	return r.client.WaitAction(ctx, actionID)
}

func (r *networkRouteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkRouteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	nid, dest, gw := plan.NetworkID.ValueInt64(), plan.Destination.ValueString(), plan.Gateway.ValueString()
	exists := false
	err := r.mutate(ctx, nid, func(routes []client.Route) []client.Route {
		for _, rt := range routes {
			if rt.Destination == dest {
				exists = true
				return routes
			}
		}
		return append(routes, client.Route{Destination: dest, Gateway: gw})
	})
	if err == nil && exists {
		resp.Diagnostics.AddError("Route already exists", "The network already has a route to "+dest+"; import it instead.")
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Adding route failed", err)
		return
	}
	plan.ID = types.StringValue(compositeID(strconv.FormatInt(nid, 10), dest))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkRouteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkRouteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	n, err := r.client.GetNetwork(ctx, state.NetworkID.ValueInt64())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading network failed", err)
		return
	}
	for _, rt := range n.Routes {
		if rt.Destination == state.Destination.ValueString() {
			state.Gateway = types.StringValue(rt.Gateway)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *networkRouteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan networkRouteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkRouteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkRouteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(d...)
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	dest := state.Destination.ValueString()
	err := r.mutate(ctx, state.NetworkID.ValueInt64(), func(routes []client.Route) []client.Route {
		kept := []client.Route{}
		for _, rt := range routes {
			if rt.Destination != dest {
				kept = append(kept, rt)
			}
		}
		return kept
	})
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Removing route failed", err)
	}
}
