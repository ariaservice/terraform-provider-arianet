package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

// ---------------------------------------------------------------- images

var imageAttrTypes = map[string]attr.Type{
	"id": types.StringType, "image": types.StringType, "type": types.StringType, "name": types.StringType,
	"description": types.StringType, "datacenters": types.ListType{ElemType: types.StringType},
	"min_disk_gb": types.Int64Type, "deprecated": types.BoolType, "status": types.StringType,
	"labels": types.MapType{ElemType: types.StringType}, "project_id": types.Int64Type, "created_at": types.StringType,
}

func imageAttrs() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":          schema.StringAttribute{Computed: true, Description: "Image ID (the slug for system/app images, a number for snapshots and backups)."},
		"image":       schema.StringAttribute{Computed: true, Description: "What `arianet_server.image` takes."},
		"type":        schema.StringAttribute{Computed: true, Description: "`system`, `app`, `snapshot` or `backup`."},
		"name":        schema.StringAttribute{Computed: true, Description: "Name."},
		"description": schema.StringAttribute{Computed: true, Description: "Description."},
		"datacenters": schema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "Datacenters that have the image."},
		"min_disk_gb": schema.Int64Attribute{Computed: true, Description: "Smallest root disk the image fits on."},
		"deprecated":  schema.BoolAttribute{Computed: true, Description: "Whether the image is deprecated."},
		"status":      schema.StringAttribute{Computed: true, Description: "Status."},
		"labels":      schema.MapAttribute{ElementType: types.StringType, Computed: true, Description: "Labels (snapshots and backups)."},
		"project_id":  schema.Int64Attribute{Computed: true, Description: "Project of a snapshot/backup (0 for platform images)."},
		"created_at":  schema.StringAttribute{Computed: true, Description: "Creation time of a snapshot/backup."},
	}
}

func imageValues(img client.Image) map[string]attr.Value {
	created := ""
	if img.CreatedAt != nil {
		created = timeValue(*img.CreatedAt).ValueString()
	}
	dcs := img.Datacenters
	if dcs == nil {
		dcs = []string{}
	}
	return map[string]attr.Value{
		"id": types.StringValue(img.ID), "image": types.StringValue(img.Image), "type": types.StringValue(img.Type),
		"name": types.StringValue(img.Name), "description": types.StringValue(img.Description),
		"datacenters": stringListValue(dcs), "min_disk_gb": types.Int64Value(img.MinDiskGB),
		"deprecated": types.BoolValue(img.Deprecated), "status": types.StringValue(img.Status),
		"labels": labelsValue(img.Labels), "project_id": types.Int64Value(img.ProjectID), "created_at": types.StringValue(created),
	}
}

var imageTypeValidator = []validator.String{stringvalidator.OneOf("system", "app", "snapshot", "backup")}

type imagesDataSource struct{ dsBase }

func newImagesDataSource() datasource.DataSource { return &imagesDataSource{} }

type imagesModel struct {
	ID            types.String `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	Datacenter    types.String `tfsdk:"datacenter"`
	LabelSelector types.String `tfsdk:"label_selector"`
	Images        types.List   `tfsdk:"images"`
}

func (d *imagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_images"
}

func (d *imagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Images a server can be created from (`GET /cloud/v1/images`): platform system/app images and the account's snapshots and backups.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Computed: true, Description: "Identifier of this lookup."},
			"type":           schema.StringAttribute{Optional: true, Description: "Only `system`, `app`, `snapshot` or `backup` images.", Validators: imageTypeValidator},
			"datacenter":     schema.StringAttribute{Optional: true, Description: "Only images available in this datacenter (location)."},
			"label_selector": schema.StringAttribute{Optional: true, Description: "Only images matching this label selector."},
			"images": schema.ListNestedAttribute{
				Computed: true, Description: "The images.",
				NestedObject: schema.NestedAttributeObject{Attributes: imageAttrs()},
			},
		},
	}
}

func (d *imagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m imagesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	imgs, err := d.client.ListImages(ctx, m.Type.ValueString(), m.Datacenter.ValueString(), m.LabelSelector.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Listing images failed", err)
		return
	}
	vals := make([]attr.Value, 0, len(imgs))
	for _, img := range imgs {
		vals = append(vals, types.ObjectValueMust(imageAttrTypes, imageValues(img)))
	}
	m.Images = types.ListValueMust(types.ObjectType{AttrTypes: imageAttrTypes}, vals)
	m.ID = types.StringValue(fmt.Sprintf("images/%s/%s/%s", m.Type.ValueString(), m.Datacenter.ValueString(), m.LabelSelector.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- image

type imageDataSource struct{ dsBase }

func newImageDataSource() datasource.DataSource { return &imageDataSource{} }

type imageModel struct {
	ID            types.String `tfsdk:"id"`
	Image         types.String `tfsdk:"image"`
	Name          types.String `tfsdk:"name"`
	Type          types.String `tfsdk:"type"`
	Datacenter    types.String `tfsdk:"datacenter"`
	LabelSelector types.String `tfsdk:"label_selector"`
	MostRecent    types.Bool   `tfsdk:"most_recent"`
	Description   types.String `tfsdk:"description"`
	Datacenters   types.List   `tfsdk:"datacenters"`
	MinDiskGB     types.Int64  `tfsdk:"min_disk_gb"`
	Deprecated    types.Bool   `tfsdk:"deprecated"`
	Status        types.String `tfsdk:"status"`
	Labels        types.Map    `tfsdk:"labels"`
	ProjectID     types.Int64  `tfsdk:"project_id"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

func (d *imageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (d *imageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "One image, looked up in `GET /cloud/v1/images` by `id`, `image` (slug/reference), `name` or `label_selector`.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Optional: true, Computed: true, Description: "Image ID."},
			"image":          schema.StringAttribute{Optional: true, Computed: true, Description: "Image slug or reference, as `arianet_server.image` takes it."},
			"name":           schema.StringAttribute{Optional: true, Computed: true, Description: "Image name."},
			"type":           schema.StringAttribute{Optional: true, Computed: true, Description: "Restrict to `system`, `app`, `snapshot` or `backup`.", Validators: imageTypeValidator},
			"datacenter":     schema.StringAttribute{Optional: true, Description: "Restrict to images available in this datacenter."},
			"label_selector": schema.StringAttribute{Optional: true, Description: "Restrict to images matching this label selector."},
			"most_recent":    schema.BoolAttribute{Optional: true, Description: "When several images match, take the newest instead of failing."},
			"description":    schema.StringAttribute{Computed: true, Description: "Description."},
			"datacenters":    schema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "Datacenters that have the image."},
			"min_disk_gb":    schema.Int64Attribute{Computed: true, Description: "Smallest root disk the image fits on."},
			"deprecated":     schema.BoolAttribute{Computed: true, Description: "Whether the image is deprecated."},
			"status":         schema.StringAttribute{Computed: true, Description: "Status."},
			"labels":         schema.MapAttribute{ElementType: types.StringType, Computed: true, Description: "Labels."},
			"project_id":     schema.Int64Attribute{Computed: true, Description: "Project (snapshots/backups)."},
			"created_at":     schema.StringAttribute{Computed: true, Description: "Creation time (snapshots/backups)."},
		},
	}
}

func (d *imageDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.AtLeastOneOf(path.MatchRoot("id"), path.MatchRoot("image"), path.MatchRoot("name"), path.MatchRoot("label_selector")),
	}
}

func (d *imageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m imageModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	imgs, err := d.client.ListImages(ctx, m.Type.ValueString(), m.Datacenter.ValueString(), m.LabelSelector.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Listing images failed", err)
		return
	}
	var found []client.Image
	for _, img := range imgs {
		if (!m.ID.IsNull() && img.ID != m.ID.ValueString()) ||
			(!m.Image.IsNull() && img.Image != m.Image.ValueString()) ||
			(!m.Name.IsNull() && img.Name != m.Name.ValueString()) {
			continue
		}
		found = append(found, img)
	}
	if len(found) == 0 {
		resp.Diagnostics.AddError("No image found", "No image matches the given arguments.")
		return
	}
	if len(found) > 1 {
		if !m.MostRecent.ValueBool() {
			resp.Diagnostics.AddError("Several images found", fmt.Sprintf("%d images match; narrow the lookup or set most_recent = true.", len(found)))
			return
		}
		sort.SliceStable(found, func(i, j int) bool {
			a, b := found[i].CreatedAt, found[j].CreatedAt
			if a == nil || b == nil {
				return b == nil && a != nil
			}
			return a.After(*b)
		})
	}
	v := imageValues(found[0])
	m.ID, m.Image, m.Name, m.Type = v["id"].(types.String), v["image"].(types.String), v["name"].(types.String), v["type"].(types.String)
	m.Description = v["description"].(types.String)
	m.Datacenters = v["datacenters"].(types.List)
	m.MinDiskGB = v["min_disk_gb"].(types.Int64)
	m.Deprecated = v["deprecated"].(types.Bool)
	m.Status = v["status"].(types.String)
	m.Labels = v["labels"].(types.Map)
	m.ProjectID = v["project_id"].(types.Int64)
	m.CreatedAt = v["created_at"].(types.String)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- plans

var planAttrTypes = map[string]attr.Type{
	"id": types.Int64Type, "name": types.StringType, "datacenter": types.StringType, "vcpu": types.Int64Type,
	"ram_mb": types.Int64Type, "disk_gb": types.Int64Type, "traffic_gb": types.Int64Type,
	"hourly_price": types.Float64Type, "monthly_price": types.Float64Type, "currency": types.StringType,
	"group": types.StringType, "recommended": types.BoolType, "available": types.BoolType,
}

type plansDataSource struct{ dsBase }

func newPlansDataSource() datasource.DataSource { return &plansDataSource{} }

type plansModel struct {
	ID         types.String `tfsdk:"id"`
	Datacenter types.String `tfsdk:"datacenter"`
	Plans      types.List   `tfsdk:"plans"`
}

func (d *plansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plans"
}

func (d *plansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Active server plans (`GET /cloud/v1/plans`), optionally of one datacenter.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, Description: "Identifier of this lookup."},
			"datacenter": schema.StringAttribute{Optional: true, Description: "Only plans of this datacenter."},
			"plans": schema.ListNestedAttribute{
				Computed: true, Description: "The plans.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":            schema.Int64Attribute{Computed: true, Description: "Plan ID (`arianet_server.plan_id`)."},
					"name":          schema.StringAttribute{Computed: true, Description: "Name."},
					"datacenter":    schema.StringAttribute{Computed: true, Description: "Datacenter."},
					"vcpu":          schema.Int64Attribute{Computed: true, Description: "vCPUs."},
					"ram_mb":        schema.Int64Attribute{Computed: true, Description: "Memory in MB."},
					"disk_gb":       schema.Int64Attribute{Computed: true, Description: "Disk in GB."},
					"traffic_gb":    schema.Int64Attribute{Computed: true, Description: "Included traffic in GB; null when unmetered."},
					"hourly_price":  schema.Float64Attribute{Computed: true, Description: "Hourly price."},
					"monthly_price": schema.Float64Attribute{Computed: true, Description: "Monthly price."},
					"currency":      schema.StringAttribute{Computed: true, Description: "Currency."},
					"group":         schema.StringAttribute{Computed: true, Description: "Plan group key."},
					"recommended":   schema.BoolAttribute{Computed: true, Description: "Whether the plan is recommended."},
					"available":     schema.BoolAttribute{Computed: true, Description: "Whether the plan can be ordered now (not sold out)."},
				}},
			},
		},
	}
}

func (d *plansDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m plansModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plans, err := d.client.ListPlans(ctx, m.Datacenter.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Listing plans failed", err)
		return
	}
	vals := make([]attr.Value, 0, len(plans))
	for _, p := range plans {
		available := p.Availability.Available || !p.Availability.Known
		vals = append(vals, types.ObjectValueMust(planAttrTypes, map[string]attr.Value{
			"id": types.Int64Value(p.ID), "name": types.StringValue(p.Name), "datacenter": types.StringValue(p.Datacenter),
			"vcpu": types.Int64Value(p.VCPU), "ram_mb": types.Int64Value(p.RAMMB), "disk_gb": types.Int64Value(p.DiskGB),
			"traffic_gb": ptrInt64Value(p.TrafficGB), "hourly_price": types.Float64Value(p.HourlyPrice),
			"monthly_price": types.Float64Value(p.MonthlyPrice), "currency": types.StringValue(p.Currency),
			"group": types.StringValue(p.GroupKey), "recommended": types.BoolValue(p.IsRecommended), "available": types.BoolValue(available),
		}))
	}
	m.Plans = types.ListValueMust(types.ObjectType{AttrTypes: planAttrTypes}, vals)
	m.ID = types.StringValue("plans/" + m.Datacenter.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- locations

var locationAttrTypes = map[string]attr.Type{
	"code": types.StringType, "name": types.StringType, "name_fa": types.StringType, "country": types.StringType,
	"city": types.StringType, "status": types.StringType, "orderable": types.BoolType,
	"features": types.ListType{ElemType: types.StringType}, "ipv6": types.BoolType,
}

type locationsDataSource struct{ dsBase }

func newLocationsDataSource() datasource.DataSource { return &locationsDataSource{} }

type locationsModel struct {
	ID        types.String `tfsdk:"id"`
	Locations types.List   `tfsdk:"locations"`
	Codes     types.List   `tfsdk:"codes"`
}

func (d *locationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_locations"
}

func (d *locationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Datacenters (locations) resources can be created in (`GET /cloud/v1/datacenters`). Their `code` is the `datacenter` argument of every resource.",
		Attributes: map[string]schema.Attribute{
			"id":    schema.StringAttribute{Computed: true, Description: "Identifier of this lookup."},
			"codes": schema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "Codes of every listed datacenter."},
			"locations": schema.ListNestedAttribute{
				Computed: true, Description: "The datacenters.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"code":      schema.StringAttribute{Computed: true, Description: "Code (the `datacenter` argument)."},
					"name":      schema.StringAttribute{Computed: true, Description: "Name."},
					"name_fa":   schema.StringAttribute{Computed: true, Description: "Persian name."},
					"country":   schema.StringAttribute{Computed: true, Description: "Country code."},
					"city":      schema.StringAttribute{Computed: true, Description: "City."},
					"status":    schema.StringAttribute{Computed: true, Description: "Status."},
					"orderable": schema.BoolAttribute{Computed: true, Description: "Whether new resources can be created there now."},
					"features":  schema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "Features, e.g. `ipv6`, `private_networks`."},
					"ipv6":      schema.BoolAttribute{Computed: true, Description: "Whether IPv6 is offered."},
				}},
			},
		},
	}
}

func (d *locationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m locationsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	dcs, err := d.client.ListDatacenters(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Listing datacenters failed", err)
		return
	}
	vals := make([]attr.Value, 0, len(dcs))
	codes := make([]string, 0, len(dcs))
	for _, dc := range dcs {
		features := dc.Features
		if features == nil {
			features = []string{}
		}
		codes = append(codes, dc.Code)
		vals = append(vals, types.ObjectValueMust(locationAttrTypes, map[string]attr.Value{
			"code": types.StringValue(dc.Code), "name": types.StringValue(dc.Name), "name_fa": types.StringValue(dc.NameFA),
			"country": types.StringValue(dc.Country), "city": types.StringValue(dc.City), "status": types.StringValue(dc.Status),
			"orderable": types.BoolValue(dc.Orderable), "features": stringListValue(features), "ipv6": types.BoolValue(dc.IPv6),
		}))
	}
	m.Locations = types.ListValueMust(types.ObjectType{AttrTypes: locationAttrTypes}, vals)
	m.Codes = stringListValue(codes)
	m.ID = types.StringValue("locations")
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- pricing

var locationPriceAttrTypes = map[string]attr.Type{
	"location": types.StringType, "hourly_price": types.Float64Type,
	"monthly_price": types.Float64Type, "currency": types.StringType,
}

var serverTypePriceAttrTypes = map[string]attr.Type{
	"id": types.Int64Type, "name": types.StringType, "description": types.StringType, "group": types.StringType,
	"group_name": types.StringType, "cores": types.Int64Type, "memory_mb": types.Int64Type, "disk_gb": types.Int64Type,
	"included_traffic_gb": types.Int64Type, "minimum_usage_hours": types.Int64Type,
	"prices": types.ListType{ElemType: types.ObjectType{AttrTypes: locationPriceAttrTypes}},
}

var resourcePriceAttrTypes = map[string]attr.Type{
	"sku": types.StringType, "location": types.StringType, "unit": types.StringType,
	"hourly_price": types.Float64Type, "monthly_price": types.Float64Type, "currency": types.StringType,
	"percent": types.Float64Type,
}

type pricingDataSource struct{ dsBase }

func newPricingDataSource() datasource.DataSource { return &pricingDataSource{} }

type pricingModel struct {
	ID            types.String `tfsdk:"id"`
	Datacenter    types.String `tfsdk:"datacenter"`
	VATIncluded   types.Bool   `tfsdk:"vat_included"`
	HoursPerMonth types.Int64  `tfsdk:"hours_per_month"`
	ServerTypes   types.List   `tfsdk:"server_types"`
	Resources     types.List   `tfsdk:"resources"`
	RawJSON       types.String `tfsdk:"raw_json"`
}

func (d *pricingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pricing"
}

func priceAttrs() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"location":      schema.StringAttribute{Computed: true, Description: "Datacenter code."},
		"hourly_price":  schema.Float64Attribute{Computed: true, Description: "Hourly price, VAT excluded."},
		"monthly_price": schema.Float64Attribute{Computed: true, Description: "Monthly price, VAT excluded."},
		"currency":      schema.StringAttribute{Computed: true, Description: "Currency, e.g. `IRR`."},
	}
}

func (d *pricingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resources := priceAttrs()
	resources["sku"] = schema.StringAttribute{Computed: true, Description: "SKU: `primary_ipv4`, `floating_ipv4`, `floating_ipv6`, `elastic_ipv4`, `volume_gb`, `snapshot_gb`, `backup`, `nat_gateway`, or a load balancer type such as `lb11`."}
	resources["unit"] = schema.StringAttribute{Computed: true, Description: "`hour` (per resource), `gb_hour` (per GB) or `percent` (a share of the server's plan; prices are 0)."}
	resources["percent"] = schema.Float64Attribute{Computed: true, Description: "The share of the plan for a `percent` SKU (backups); null otherwise."}
	resp.Schema = schema.Schema{
		Description: "The public price list (`GET /cloud/v1/public/pricing`): every plan per location and every other price on sale, VAT excluded, from the rows billing uses.",
		Attributes: map[string]schema.Attribute{
			"id":              schema.StringAttribute{Computed: true, Description: "Identifier of this lookup."},
			"datacenter":      schema.StringAttribute{Optional: true, Description: "Only prices in this datacenter (location); plans without a price there are left out."},
			"vat_included":    schema.BoolAttribute{Computed: true, Description: "Whether prices include VAT (always false)."},
			"hours_per_month": schema.Int64Attribute{Computed: true, Description: "Hours monthly prices are quoted over."},
			"server_types": schema.ListNestedAttribute{
				Computed: true, Description: "Plans and their price per location.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":                  schema.Int64Attribute{Computed: true, Description: "Plan ID (`arianet_server.plan_id`)."},
					"name":                schema.StringAttribute{Computed: true, Description: "Name."},
					"description":         schema.StringAttribute{Computed: true, Description: "Description."},
					"group":               schema.StringAttribute{Computed: true, Description: "Plan group key."},
					"group_name":          schema.StringAttribute{Computed: true, Description: "Plan group name."},
					"cores":               schema.Int64Attribute{Computed: true, Description: "vCPUs."},
					"memory_mb":           schema.Int64Attribute{Computed: true, Description: "Memory in MB."},
					"disk_gb":             schema.Int64Attribute{Computed: true, Description: "Disk in GB."},
					"included_traffic_gb": schema.Int64Attribute{Computed: true, Description: "Included traffic; null when not metered."},
					"minimum_usage_hours": schema.Int64Attribute{Computed: true, Description: "Hours charged up front when a server is created."},
					"prices": schema.ListNestedAttribute{
						Computed: true, Description: "Price per location.",
						NestedObject: schema.NestedAttributeObject{Attributes: priceAttrs()},
					},
				}},
			},
			"resources": schema.ListNestedAttribute{
				Computed: true, Description: "Every other price on sale (IPs, volumes, snapshots, backups, NAT gateways, load balancers).",
				NestedObject: schema.NestedAttributeObject{Attributes: resources},
			},
			"raw_json": schema.StringAttribute{Computed: true, Description: "The `pricing` object as JSON, for fields not modelled here (use `jsondecode`)."},
		},
	}
}

func locationPriceValue(location string, hourly, monthly client.PublicPrice) attr.Value {
	currency := monthly.Currency
	if currency == "" {
		currency = hourly.Currency
	}
	return types.ObjectValueMust(locationPriceAttrTypes, map[string]attr.Value{
		"location": types.StringValue(location), "hourly_price": types.Float64Value(hourly.Net),
		"monthly_price": types.Float64Value(monthly.Net), "currency": types.StringValue(currency),
	})
}

func (d *pricingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m pricingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, raw, err := d.client.PublicPricing(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading prices failed", err)
		return
	}
	only := m.Datacenter.ValueString()

	plans := []attr.Value{}
	for _, st := range p.ServerTypes {
		prices := []attr.Value{}
		for _, lp := range st.Prices {
			if only == "" || lp.Location == only {
				prices = append(prices, locationPriceValue(lp.Location, lp.PriceHourly, lp.PriceMonthly))
			}
		}
		if only != "" && len(prices) == 0 {
			continue
		}
		plans = append(plans, types.ObjectValueMust(serverTypePriceAttrTypes, map[string]attr.Value{
			"id": types.Int64Value(st.ID), "name": types.StringValue(st.Name), "description": types.StringValue(st.Description),
			"group": types.StringValue(st.Group), "group_name": types.StringValue(st.GroupName), "cores": types.Int64Value(st.Cores),
			"memory_mb": types.Int64Value(st.MemoryMB), "disk_gb": types.Int64Value(st.DiskGB),
			"included_traffic_gb": ptrInt64Value(st.IncludedTrafficGB), "minimum_usage_hours": types.Int64Value(st.MinimumUsageHours),
			"prices": types.ListValueMust(types.ObjectType{AttrTypes: locationPriceAttrTypes}, prices),
		}))
	}

	res := []attr.Value{}
	for _, rp := range p.Resources {
		if only != "" && rp.Location != only {
			continue
		}
		percent := types.Float64Null()
		if rp.Percent != nil {
			percent = types.Float64Value(*rp.Percent)
		}
		lp := locationPriceValue(rp.Location, rp.PriceHourly, rp.PriceMonthly).(types.Object).Attributes()
		vals := map[string]attr.Value{"sku": types.StringValue(rp.SKU), "unit": types.StringValue(rp.Unit), "percent": percent}
		for k, v := range lp {
			vals[k] = v
		}
		res = append(res, types.ObjectValueMust(resourcePriceAttrTypes, vals))
	}

	m.VATIncluded = types.BoolValue(p.VATIncluded)
	m.HoursPerMonth = types.Int64Value(p.HoursPerMonth)
	m.ServerTypes = types.ListValueMust(types.ObjectType{AttrTypes: serverTypePriceAttrTypes}, plans)
	m.Resources = types.ListValueMust(types.ObjectType{AttrTypes: resourcePriceAttrTypes}, res)
	m.RawJSON = types.StringValue(string(raw))
	m.ID = types.StringValue("pricing/" + only)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- load balancer types

var lbTypeAttrTypes = map[string]attr.Type{
	"code": types.StringType, "name": types.StringType, "max_services": types.Int64Type,
	"max_targets": types.Int64Type, "max_connections": types.Int64Type,
}

type lbTypesDataSource struct{ dsBase }

func newLoadBalancerTypesDataSource() datasource.DataSource { return &lbTypesDataSource{} }

type lbTypesModel struct {
	ID    types.String `tfsdk:"id"`
	Types types.List   `tfsdk:"load_balancer_types"`
}

func (d *lbTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer_types"
}

func (d *lbTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Load balancer sizes on sale (`GET /cloud/v1/load-balancer-types`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Identifier of this lookup."},
			"load_balancer_types": schema.ListNestedAttribute{
				Computed: true, Description: "The types, smallest first.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"code":            schema.StringAttribute{Computed: true, Description: "Code (`arianet_load_balancer.type`)."},
					"name":            schema.StringAttribute{Computed: true, Description: "Name."},
					"max_services":    schema.Int64Attribute{Computed: true, Description: "Most services (listeners)."},
					"max_targets":     schema.Int64Attribute{Computed: true, Description: "Most targets."},
					"max_connections": schema.Int64Attribute{Computed: true, Description: "Connection limit per listener."},
				}},
			},
		},
	}
}

func (d *lbTypesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m lbTypesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	lts, err := d.client.ListLoadBalancerTypes(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Listing load balancer types failed", err)
		return
	}
	vals := make([]attr.Value, 0, len(lts))
	for _, t := range lts {
		vals = append(vals, types.ObjectValueMust(lbTypeAttrTypes, map[string]attr.Value{
			"code": types.StringValue(t.Code), "name": types.StringValue(t.Name), "max_services": types.Int64Value(t.MaxServices),
			"max_targets": types.Int64Value(t.MaxTargets), "max_connections": types.Int64Value(t.MaxConnections),
		}))
	}
	m.Types = types.ListValueMust(types.ObjectType{AttrTypes: lbTypeAttrTypes}, vals)
	m.ID = types.StringValue("load_balancer_types")
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
