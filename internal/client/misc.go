package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ---------------------------------------------------------------- floating IPs

// FloatingIP is a floating IP.
type FloatingIP struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Datacenter       string            `json:"datacenter"`
	Type             string            `json:"type"`
	IP               string            `json:"ip"`
	ServerID         *int64            `json:"server_id"`
	DNSPtr           string            `json:"dns_ptr"`
	Status           string            `json:"status"`
	ProtectionDelete bool              `json:"protection_delete"`
	Labels           map[string]string `json:"labels"`
	ProjectID        int64             `json:"project_id"`
	CreatedAt        time.Time         `json:"created_at"`
}

// CreateFloatingIPRequest is POST /floating-ips.
type CreateFloatingIPRequest struct {
	Datacenter  string            `json:"datacenter"`
	Type        string            `json:"type"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	ServerID    int64             `json:"server_id,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	ProjectID   *int64            `json:"project_id,omitempty"`
}

func (c *Client) GetFloatingIP(ctx context.Context, id int64) (FloatingIP, error) {
	var out one[FloatingIP]
	err := c.Cloud(ctx, http.MethodGet, idPath("/floating-ips", id), nil, nil, &out)
	return out.Data, err
}

func (c *Client) CreateFloatingIP(ctx context.Context, req CreateFloatingIPRequest) (FloatingIP, int64, error) {
	var out struct {
		Data     FloatingIP `json:"data"`
		ActionID int64      `json:"action_id"`
	}
	err := c.Cloud(ctx, http.MethodPost, "/floating-ips", nil, req, &out)
	return out.Data, out.ActionID, err
}

func (c *Client) UpdateFloatingIP(ctx context.Context, id int64, req UpdateRequest) (FloatingIP, error) {
	var out one[FloatingIP]
	err := c.Cloud(ctx, http.MethodPatch, idPath("/floating-ips", id), nil, req, &out)
	return out.Data, err
}

func (c *Client) DeleteFloatingIP(ctx context.Context, id int64) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodDelete, idPath("/floating-ips", id), nil, nil, &out)
	return out.ActionID, err
}

func (c *Client) floatingIPAction(ctx context.Context, id int64, op string, body any) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodPost, idPath("/floating-ips", id)+"/actions/"+op, nil, body, &out)
	return out.ActionID, err
}

func (c *Client) AssignFloatingIP(ctx context.Context, id, serverID int64) (int64, error) {
	return c.floatingIPAction(ctx, id, "assign", map[string]any{"server_id": serverID})
}

func (c *Client) UnassignFloatingIP(ctx context.Context, id int64) (int64, error) {
	return c.floatingIPAction(ctx, id, "unassign", nil)
}

// ChangeFloatingIPDNSPtr sets the reverse DNS; "" resets it.
func (c *Client) ChangeFloatingIPDNSPtr(ctx context.Context, id int64, ptr string) (int64, error) {
	var v *string
	if ptr != "" {
		v = &ptr
	}
	return c.floatingIPAction(ctx, id, "change_dns_ptr", map[string]any{"dns_ptr": v})
}

// ---------------------------------------------------------------- catalog

// Datacenter is a location servers can be created in.
type Datacenter struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	NameFA    string   `json:"name_fa"`
	Country   string   `json:"country"`
	City      string   `json:"city"`
	Flag      string   `json:"flag"`
	Status    string   `json:"status"`
	Orderable bool     `json:"orderable"`
	Features  []string `json:"features"`
	SortOrder int64    `json:"sort_order"`
	IPv6      bool     `json:"ipv6"`
}

// Image is a row of GET /images.
type Image struct {
	ID          string            `json:"id"`
	Image       string            `json:"image"`
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Datacenters []string          `json:"datacenters"`
	MinDiskGB   int64             `json:"min_disk_gb"`
	Deprecated  bool              `json:"deprecated"`
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	ProjectID   int64             `json:"project_id"`
	CreatedAt   *time.Time        `json:"created_at"`
}

// PlanAvailability says whether a plan is on sale right now.
type PlanAvailability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	Known     bool   `json:"known"`
}

// Plan is a server plan.
type Plan struct {
	ID                int64            `json:"id"`
	Datacenter        string           `json:"datacenter"`
	Name              string           `json:"name"`
	VCPU              int64            `json:"vcpu"`
	RAMMB             int64            `json:"ram_mb"`
	DiskGB            int64            `json:"disk_gb"`
	TrafficGB         *int64           `json:"traffic_gb"`
	HourlyPrice       float64          `json:"hourly_price"`
	MonthlyPrice      float64          `json:"monthly_price"`
	Currency          string           `json:"currency"`
	GroupKey          string           `json:"group_key"`
	GroupName         string           `json:"group_name"`
	MinimumUsageHours int64            `json:"minimum_usage_hours"`
	IsRecommended     bool             `json:"is_recommended"`
	Availability      PlanAvailability `json:"availability"`
}

// LoadBalancerType is a size of load balancer.
type LoadBalancerType struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	MaxServices    int64  `json:"max_services"`
	MaxTargets     int64  `json:"max_targets"`
	MaxConnections int64  `json:"max_connections"`
}

func (c *Client) ListDatacenters(ctx context.Context) ([]Datacenter, error) {
	var out struct {
		Data []Datacenter `json:"data"`
	}
	err := c.Cloud(ctx, http.MethodGet, "/datacenters", nil, nil, &out)
	return out.Data, err
}

// ListImages is GET /images?type=&datacenter=&label_selector=.
func (c *Client) ListImages(ctx context.Context, typ, datacenter, labelSelector string) ([]Image, error) {
	return listAll[Image](ctx, c, "/images", url.Values{"type": {typ}, "datacenter": {datacenter}, "label_selector": {labelSelector}})
}

func (c *Client) ListPlans(ctx context.Context, datacenter string) ([]Plan, error) {
	q := url.Values{}
	if datacenter != "" {
		q.Set("datacenter", datacenter)
	}
	var out struct {
		Data []Plan `json:"data"`
	}
	err := c.Cloud(ctx, http.MethodGet, "/plans", q, nil, &out)
	return out.Data, err
}

func (c *Client) ListLoadBalancerTypes(ctx context.Context) ([]LoadBalancerType, error) {
	var out struct {
		Data []LoadBalancerType `json:"data"`
	}
	err := c.Cloud(ctx, http.MethodGet, "/load-balancer-types", nil, nil, &out)
	return out.Data, err
}

// PublicPrice is an amount, VAT excluded.
type PublicPrice struct {
	Net      float64 `json:"net"`
	Currency string  `json:"currency"`
}

// PublicLocationPrice is a plan's price in one datacenter.
type PublicLocationPrice struct {
	Location     string      `json:"location"`
	PriceHourly  PublicPrice `json:"price_hourly"`
	PriceMonthly PublicPrice `json:"price_monthly"`
}

// PublicServerType is a plan on the public price list.
type PublicServerType struct {
	ID                int64                 `json:"id"`
	Name              string                `json:"name"`
	Description       string                `json:"description"`
	Group             string                `json:"group"`
	GroupName         string                `json:"group_name"`
	Cores             int64                 `json:"cores"`
	MemoryMB          int64                 `json:"memory_mb"`
	DiskGB            int64                 `json:"disk_gb"`
	IncludedTrafficGB *int64                `json:"included_traffic_gb"`
	MinimumUsageHours int64                 `json:"minimum_usage_hours"`
	Prices            []PublicLocationPrice `json:"prices"`
}

// PublicResourcePrice is a non-plan SKU price (IPs, volumes, backups, ...).
type PublicResourcePrice struct {
	SKU          string      `json:"sku"`
	Location     string      `json:"location"`
	Unit         string      `json:"unit"`
	PriceHourly  PublicPrice `json:"price_hourly"`
	PriceMonthly PublicPrice `json:"price_monthly"`
	Percent      *float64    `json:"percent"`
}

// PublicPricing is the public price list.
type PublicPricing struct {
	VATIncluded   bool                  `json:"vat_included"`
	HoursPerMonth int64                 `json:"hours_per_month"`
	ServerTypes   []PublicServerType    `json:"server_types"`
	Resources     []PublicResourcePrice `json:"resources"`
}

// PublicPricing is GET /public/pricing (no token needed): the typed list and
// the raw "pricing" object.
func (c *Client) PublicPricing(ctx context.Context) (PublicPricing, json.RawMessage, error) {
	var out struct {
		Pricing json.RawMessage `json:"pricing"`
	}
	if err := c.Cloud(ctx, http.MethodGet, "/public/pricing", nil, nil, &out); err != nil {
		return PublicPricing{}, nil, err
	}
	var p PublicPricing
	if err := json.Unmarshal(out.Pricing, &p); err != nil {
		return PublicPricing{}, out.Pricing, fmt.Errorf("decoding pricing: %w", err)
	}
	return p, out.Pricing, nil
}
