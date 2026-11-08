package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Server is a cloud server as GET /servers/{id} returns it.
type Server struct {
	ID                int64             `json:"id"`
	Name              string            `json:"name"`
	Status            string            `json:"status"`
	Datacenter        string            `json:"datacenter"`
	PlanID            int64             `json:"plan_id"`
	DiskGB            int64             `json:"disk_gb"`
	Image             string            `json:"image"`
	IPv4              string            `json:"ipv4"`
	IPv6              string            `json:"ipv6"`
	RescueEnabled     bool              `json:"rescue_enabled"`
	Locked            bool              `json:"locked"`
	LockedReason      string            `json:"locked_reason"`
	ProtectionDelete  bool              `json:"protection_delete"`
	ProtectionRebuild bool              `json:"protection_rebuild"`
	Labels            map[string]string `json:"labels"`
	ProjectID         int64             `json:"project_id"`
	CreatedAt         time.Time         `json:"created_at"`
}

// ServerNetworks is the create body's "networks".
type ServerNetworks struct {
	EnableIPv4 *bool   `json:"enable_ipv4,omitempty"`
	EnableIPv6 *bool   `json:"enable_ipv6,omitempty"`
	NetworkIDs []int64 `json:"network_ids,omitempty"`
}

// CreateServerRequest is POST /servers.
type CreateServerRequest struct {
	Datacenter       string            `json:"datacenter"`
	Name             string            `json:"name"`
	PlanID           int64             `json:"plan_id"`
	Image            string            `json:"image"`
	SSHKeys          []string          `json:"ssh_keys,omitempty"`
	SSHKeyIDs        []int64           `json:"ssh_key_ids,omitempty"`
	UserData         string            `json:"user_data,omitempty"`
	StartAfterCreate *bool             `json:"start_after_create,omitempty"`
	FirewallIDs      []int64           `json:"firewall_ids,omitempty"`
	PlacementGroupID *int64            `json:"placement_group_id,omitempty"`
	Backups          bool              `json:"backups,omitempty"`
	Networks         *ServerNetworks   `json:"networks,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	ProjectID        *int64            `json:"project_id,omitempty"`
}

// CreateServerResult is the create's answer.
type CreateServerResult struct {
	Server       Server
	ActionID     int64
	RootPassword string
}

// UpdateServerRequest is PATCH /servers/{id}; nil fields stay.
type UpdateServerRequest struct {
	Name              *string            `json:"name,omitempty"`
	ProtectionDelete  *bool              `json:"protection_delete,omitempty"`
	ProtectionRebuild *bool              `json:"protection_rebuild,omitempty"`
	Labels            *map[string]string `json:"labels,omitempty"`
	ProjectID         *int64             `json:"project_id,omitempty"`
}

// DeleteServerOptions are what DELETE /servers/{id} is told about the
// server's copies, as ?keep_backups=true / ?delete_snapshots=true (absent
// means false).
type DeleteServerOptions struct {
	KeepBackups     bool
	DeleteSnapshots bool
}

// BackupPlan is the server's backup schedule, nil when backups are off.
type BackupPlan struct {
	ServerID   int64 `json:"server_id"`
	WindowHour int   `json:"window_hour"`
	Retention  int   `json:"retention"`
}

func (c *Client) ListServers(ctx context.Context, labelSelector string) ([]Server, error) {
	return listAll[Server](ctx, c, "/servers", url.Values{"label_selector": {labelSelector}})
}

func (c *Client) GetServer(ctx context.Context, id int64) (Server, error) {
	var out one[Server]
	err := c.Cloud(ctx, http.MethodGet, idPath("/servers", id), nil, nil, &out)
	return out.Data, err
}

func (c *Client) CreateServer(ctx context.Context, req CreateServerRequest) (CreateServerResult, error) {
	var out struct {
		Data         Server `json:"data"`
		ActionID     int64  `json:"action_id"`
		RootPassword string `json:"root_password"`
	}
	if err := c.Cloud(ctx, http.MethodPost, "/servers", nil, req, &out); err != nil {
		return CreateServerResult{}, err
	}
	return CreateServerResult{Server: out.Data, ActionID: out.ActionID, RootPassword: out.RootPassword}, nil
}

func (c *Client) UpdateServer(ctx context.Context, id int64, req UpdateServerRequest) (Server, error) {
	var out one[Server]
	err := c.Cloud(ctx, http.MethodPatch, idPath("/servers", id), nil, req, &out)
	return out.Data, err
}

func (c *Client) DeleteServer(ctx context.Context, id int64, opts DeleteServerOptions) (int64, error) {
	q := url.Values{}
	if opts.KeepBackups {
		q.Set("keep_backups", "true")
	}
	if opts.DeleteSnapshots {
		q.Set("delete_snapshots", "true")
	}
	var out actionRef
	err := c.Cloud(ctx, http.MethodDelete, idPath("/servers", id), q, nil, &out)
	return out.ActionID, err
}

// ServerAction runs POST /servers/{id}/actions/{op}.
func (c *Client) ServerAction(ctx context.Context, id int64, op string, body any) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodPost, idPath("/servers", id)+"/actions/"+op, nil, body, &out)
	return out.ActionID, err
}

func (c *Client) ChangeServerPlan(ctx context.Context, id, planID int64) (int64, error) {
	return c.ServerAction(ctx, id, "change_plan", map[string]any{"plan_id": planID})
}

func (c *Client) EnableServerBackup(ctx context.Context, id int64) (int64, error) {
	return c.ServerAction(ctx, id, "enable_backup", map[string]any{})
}

func (c *Client) DisableServerBackup(ctx context.Context, id int64) (int64, error) {
	return c.ServerAction(ctx, id, "disable_backup", nil)
}

// ServerBackupPlan is the plan of GET /servers/{id}/backups.
func (c *Client) ServerBackupPlan(ctx context.Context, id int64) (*BackupPlan, error) {
	var out struct {
		Plan *BackupPlan `json:"plan"`
	}
	err := c.Cloud(ctx, http.MethodGet, idPath("/servers", id)+"/backups", nil, nil, &out)
	return out.Plan, err
}

// CostLine is one billed piece of a server.
type CostLine struct {
	Type         string  `json:"type"`
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	SKU          string  `json:"sku"`
	Quantity     float64 `json:"quantity"`
	Unit         string  `json:"unit"`
	HourlyPrice  float64 `json:"hourly_price"`
	MonthlyPrice float64 `json:"monthly_price"`
	Currency     string  `json:"currency"`
}

// CostTotal is the sum of a server's lines in one currency.
type CostTotal struct {
	Currency     string  `json:"currency"`
	HourlyPrice  float64 `json:"hourly_price"`
	MonthlyPrice float64 `json:"monthly_price"`
}

// RetainedCost is what copies cost once the server is deleted.
type RetainedCost struct {
	Count        int64   `json:"count"`
	SizeGB       float64 `json:"size_gb"`
	HourlyPrice  float64 `json:"hourly_price"`
	MonthlyPrice float64 `json:"monthly_price"`
	Currency     string  `json:"currency"`
}

// ServerCost is GET /servers/{id}/costs.
type ServerCost struct {
	ServerID      int64        `json:"server_id"`
	HoursPerMonth int64        `json:"hours_per_month"`
	Lines         []CostLine   `json:"lines"`
	Totals        []CostTotal  `json:"totals"`
	Snapshots     RetainedCost `json:"snapshots"`
	KeptBackups   RetainedCost `json:"kept_backups"`
}

func (c *Client) GetServerCost(ctx context.Context, id int64) (ServerCost, error) {
	var out one[ServerCost]
	err := c.Cloud(ctx, http.MethodGet, idPath("/servers", id)+"/costs", nil, nil, &out)
	return out.Data, err
}
