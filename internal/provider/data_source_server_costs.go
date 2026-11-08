package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var costLineAttrTypes = map[string]attr.Type{
	"type": types.StringType, "id": types.Int64Type, "name": types.StringType, "sku": types.StringType,
	"quantity": types.Float64Type, "unit": types.StringType, "hourly_price": types.Float64Type,
	"monthly_price": types.Float64Type, "currency": types.StringType,
}

var costTotalAttrTypes = map[string]attr.Type{
	"currency": types.StringType, "hourly_price": types.Float64Type, "monthly_price": types.Float64Type,
}

var retainedCostAttrTypes = map[string]attr.Type{
	"count": types.Int64Type, "size_gb": types.Float64Type, "hourly_price": types.Float64Type,
	"monthly_price": types.Float64Type, "currency": types.StringType,
}

type serverCostsDataSource struct{ dsBase }

func newServerCostsDataSource() datasource.DataSource { return &serverCostsDataSource{} }

type serverCostsModel struct {
	ID            types.String `tfsdk:"id"`
	ServerID      types.Int64  `tfsdk:"server_id"`
	HoursPerMonth types.Int64  `tfsdk:"hours_per_month"`
	Lines         types.List   `tfsdk:"lines"`
	Totals        types.List   `tfsdk:"totals"`
	Snapshots     types.Object `tfsdk:"snapshots"`
	KeptBackups   types.Object `tfsdk:"kept_backups"`
}

func (d *serverCostsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_costs"
}

func retainedAttrs(desc string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Computed: true, Description: desc,
		Attributes: map[string]schema.Attribute{
			"count":         schema.Int64Attribute{Computed: true, Description: "How many copies."},
			"size_gb":       schema.Float64Attribute{Computed: true, Description: "Their total size in GB."},
			"hourly_price":  schema.Float64Attribute{Computed: true, Description: "Hourly price."},
			"monthly_price": schema.Float64Attribute{Computed: true, Description: "Monthly price."},
			"currency":      schema.StringAttribute{Computed: true, Description: "Currency."},
		},
	}
}

func (d *serverCostsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A server's current rate (`GET /cloud/v1/servers/{id}/costs`): one line per billed piece (plan, primary IPv4, floating and elastic IPs, attached volumes, backup add-on), totals per currency, and what its copies cost after a delete.",
		Attributes: map[string]schema.Attribute{
			"id":              schema.StringAttribute{Computed: true, Description: "The server ID."},
			"server_id":       schema.Int64Attribute{Required: true, Description: "Server to price."},
			"hours_per_month": schema.Int64Attribute{Computed: true, Description: "Hours monthly prices are quoted over."},
			"lines": schema.ListNestedAttribute{
				Computed: true, Description: "Billed pieces.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"type":          schema.StringAttribute{Computed: true, Description: "`server`, `primary_ipv4`, `floating_ip`, `elastic_ip`, `volume` or `backup`."},
					"id":            schema.Int64Attribute{Computed: true, Description: "ID in its own list (the server's for `server` and `backup`)."},
					"name":          schema.StringAttribute{Computed: true, Description: "Name."},
					"sku":           schema.StringAttribute{Computed: true, Description: "SKU."},
					"quantity":      schema.Float64Attribute{Computed: true, Description: "1, a volume's GB, or the backup's percent of the plan."},
					"unit":          schema.StringAttribute{Computed: true, Description: "Unit."},
					"hourly_price":  schema.Float64Attribute{Computed: true, Description: "Hourly price."},
					"monthly_price": schema.Float64Attribute{Computed: true, Description: "Monthly price."},
					"currency":      schema.StringAttribute{Computed: true, Description: "Currency."},
				}},
			},
			"totals": schema.ListNestedAttribute{
				Computed: true, Description: "Sum of the lines per currency.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"currency":      schema.StringAttribute{Computed: true, Description: "Currency."},
					"hourly_price":  schema.Float64Attribute{Computed: true, Description: "Hourly total."},
					"monthly_price": schema.Float64Attribute{Computed: true, Description: "Monthly total."},
				}},
			},
			"snapshots":    retainedAttrs("The server's snapshots, which stay billed after a delete."),
			"kept_backups": retainedAttrs("The server's backups, billed after a delete only with `keep_backups`."),
		},
	}
}

func retainedValue(r client.RetainedCost) types.Object {
	return types.ObjectValueMust(retainedCostAttrTypes, map[string]attr.Value{
		"count": types.Int64Value(r.Count), "size_gb": types.Float64Value(r.SizeGB), "hourly_price": types.Float64Value(r.HourlyPrice),
		"monthly_price": types.Float64Value(r.MonthlyPrice), "currency": types.StringValue(r.Currency),
	})
}

func (d *serverCostsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m serverCostsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := d.client.GetServerCost(ctx, m.ServerID.ValueInt64())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading server costs failed", err)
		return
	}
	lines := make([]attr.Value, 0, len(c.Lines))
	for _, l := range c.Lines {
		lines = append(lines, types.ObjectValueMust(costLineAttrTypes, map[string]attr.Value{
			"type": types.StringValue(l.Type), "id": types.Int64Value(l.ID), "name": types.StringValue(l.Name),
			"sku": types.StringValue(l.SKU), "quantity": types.Float64Value(l.Quantity), "unit": types.StringValue(l.Unit),
			"hourly_price": types.Float64Value(l.HourlyPrice), "monthly_price": types.Float64Value(l.MonthlyPrice),
			"currency": types.StringValue(l.Currency),
		}))
	}
	totals := make([]attr.Value, 0, len(c.Totals))
	for _, t := range c.Totals {
		totals = append(totals, types.ObjectValueMust(costTotalAttrTypes, map[string]attr.Value{
			"currency": types.StringValue(t.Currency), "hourly_price": types.Float64Value(t.HourlyPrice),
			"monthly_price": types.Float64Value(t.MonthlyPrice),
		}))
	}
	m.ID = types.StringValue(strconv.FormatInt(m.ServerID.ValueInt64(), 10))
	m.HoursPerMonth = types.Int64Value(c.HoursPerMonth)
	m.Lines = types.ListValueMust(types.ObjectType{AttrTypes: costLineAttrTypes}, lines)
	m.Totals = types.ListValueMust(types.ObjectType{AttrTypes: costTotalAttrTypes}, totals)
	m.Snapshots = retainedValue(c.Snapshots)
	m.KeptBackups = retainedValue(c.KeptBackups)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
