package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Subnet is a subnet of a private network.
type Subnet struct {
	ID        int64     `json:"id"`
	IPRange   string    `json:"ip_range"`
	Status    string    `json:"status"`
	Gateway   string    `json:"gateway"`
	CreatedAt time.Time `json:"created_at"`
}

// Route is a static route of a private network.
type Route struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
}

// NetworkServer is a server attached to a network.
type NetworkServer struct {
	ServerID int64  `json:"server_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	SubnetID int64  `json:"subnet_id"`
	IP       string `json:"ip"`
	Primary  bool   `json:"primary"`
}

// Network is a private network.
type Network struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	Datacenter       string            `json:"datacenter"`
	IPRange          string            `json:"ip_range"`
	Status           string            `json:"status"`
	ProtectionDelete bool              `json:"protection_delete"`
	Labels           map[string]string `json:"labels"`
	Subnets          []Subnet          `json:"subnets"`
	Routes           []Route           `json:"routes"`
	Servers          []NetworkServer   `json:"servers"`
	ProjectID        int64             `json:"project_id"`
	CreatedAt        time.Time         `json:"created_at"`
}

// CreateNetworkRequest is POST /networks.
type CreateNetworkRequest struct {
	Name          string            `json:"name"`
	Datacenter    string            `json:"datacenter"`
	IPRange       string            `json:"ip_range"`
	SubnetIPRange string            `json:"subnet_ip_range,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	ProjectID     *int64            `json:"project_id,omitempty"`
}

// UpdateRequest is the shared PATCH of networking resources.
type UpdateRequest struct {
	Name             *string            `json:"name,omitempty"`
	Description      *string            `json:"description,omitempty"`
	Labels           *map[string]string `json:"labels,omitempty"`
	ProtectionDelete *bool              `json:"protection_delete,omitempty"`
	ProjectID        *int64             `json:"project_id,omitempty"`
}

func (c *Client) ListNetworks(ctx context.Context, labelSelector string) ([]Network, error) {
	return listAll[Network](ctx, c, "/networks", url.Values{"label_selector": {labelSelector}})
}

func (c *Client) GetNetwork(ctx context.Context, id int64) (Network, error) {
	var out one[Network]
	err := c.Cloud(ctx, http.MethodGet, idPath("/networks", id), nil, nil, &out)
	return out.Data, err
}

func (c *Client) CreateNetwork(ctx context.Context, req CreateNetworkRequest) (Network, int64, error) {
	var out struct {
		Data     Network `json:"data"`
		ActionID int64   `json:"action_id"`
	}
	err := c.Cloud(ctx, http.MethodPost, "/networks", nil, req, &out)
	return out.Data, out.ActionID, err
}

func (c *Client) UpdateNetwork(ctx context.Context, id int64, req UpdateRequest) (Network, error) {
	var out one[Network]
	err := c.Cloud(ctx, http.MethodPatch, idPath("/networks", id), nil, req, &out)
	return out.Data, err
}

func (c *Client) DeleteNetwork(ctx context.Context, id int64) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodDelete, idPath("/networks", id), nil, nil, &out)
	return out.ActionID, err
}

func (c *Client) AddSubnet(ctx context.Context, networkID int64, ipRange string) (Subnet, int64, error) {
	var out struct {
		Data     Subnet `json:"data"`
		ActionID int64  `json:"action_id"`
	}
	err := c.Cloud(ctx, http.MethodPost, idPath("/networks", networkID)+"/subnets", nil, map[string]any{"ip_range": ipRange}, &out)
	return out.Data, out.ActionID, err
}

func (c *Client) DeleteSubnet(ctx context.Context, networkID, subnetID int64) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodDelete, idPath(idPath("/networks", networkID)+"/subnets", subnetID), nil, nil, &out)
	return out.ActionID, err
}

// SetRoutes replaces every route of a network.
func (c *Client) SetRoutes(ctx context.Context, networkID int64, routes []Route) (int64, error) {
	if routes == nil {
		routes = []Route{}
	}
	var out actionRef
	err := c.Cloud(ctx, http.MethodPut, idPath("/networks", networkID)+"/routes", nil, map[string]any{"routes": routes}, &out)
	return out.ActionID, err
}
