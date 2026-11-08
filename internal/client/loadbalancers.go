package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// LBHealthHTTP is the http part of a health check.
type LBHealthHTTP struct {
	Domain      string   `json:"domain,omitempty"`
	Path        string   `json:"path"`
	StatusCodes []string `json:"status_codes"`
	TLS         bool     `json:"tls"`
}

// LBHealthCheck is a service's health check.
type LBHealthCheck struct {
	Protocol string        `json:"protocol"`
	Port     int64         `json:"port"`
	Interval int64         `json:"interval"`
	Timeout  int64         `json:"timeout"`
	Retries  int64         `json:"retries"`
	HTTP     *LBHealthHTTP `json:"http,omitempty"`
}

// LBServiceHTTP is what an http or https service adds.
type LBServiceHTTP struct {
	StickySessions bool   `json:"sticky_sessions"`
	CookieName     string `json:"cookie_name,omitempty"`
	RedirectHTTP   bool   `json:"redirect_http"`
	RedirectHost   string `json:"redirect_host,omitempty"`
}

// LBService is a listener of a load balancer.
type LBService struct {
	Protocol        string         `json:"protocol"`
	ListenPort      int64          `json:"listen_port"`
	DestinationPort int64          `json:"destination_port"`
	Proxyprotocol   bool           `json:"proxyprotocol"`
	HealthCheck     LBHealthCheck  `json:"health_check"`
	HTTP            *LBServiceHTTP `json:"http,omitempty"`
}

// LBTarget is a target as the load balancer view lists it.
type LBTarget struct {
	ID            int64  `json:"id"`
	Type          string `json:"type"`
	ServerID      *int64 `json:"server_id"`
	IP            string `json:"ip"`
	LabelSelector string `json:"label_selector"`
	UsePrivateIP  bool   `json:"use_private_ip"`
	Address       string `json:"address"`
	HealthStatus  string `json:"health_status"`
}

// LBTargetInput is a target as add_target takes it.
type LBTargetInput struct {
	Type          string `json:"type"`
	ServerID      int64  `json:"server_id,omitempty"`
	IP            string `json:"ip,omitempty"`
	LabelSelector string `json:"label_selector,omitempty"`
	UsePrivateIP  bool   `json:"use_private_ip"`
}

// LBNetwork is the private network of a load balancer.
type LBNetwork struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	IPRange string `json:"ip_range"`
}

// LoadBalancer is a load balancer.
type LoadBalancer struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	Datacenter       string            `json:"datacenter"`
	Type             string            `json:"type"`
	Algorithm        string            `json:"algorithm"`
	Status           string            `json:"status"`
	PublicInterface  bool              `json:"public_interface"`
	IPv4             *string           `json:"ipv4"`
	IPv6             *string           `json:"ipv6"`
	PrivateIP        *string           `json:"private_ip"`
	Network          *LBNetwork        `json:"network"`
	Services         []LBService       `json:"services"`
	Targets          []LBTarget        `json:"targets"`
	ProtectionDelete bool              `json:"protection_delete"`
	Labels           map[string]string `json:"labels"`
	Locked           bool              `json:"locked"`
	ProjectID        int64             `json:"project_id"`
	CreatedAt        time.Time         `json:"created_at"`
}

// CreateLoadBalancerRequest is POST /load-balancers.
type CreateLoadBalancerRequest struct {
	Datacenter      string            `json:"datacenter"`
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Algorithm       string            `json:"algorithm,omitempty"`
	PublicInterface *bool             `json:"public_interface,omitempty"`
	NetworkID       int64             `json:"network_id,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	ProjectID       *int64            `json:"project_id,omitempty"`
}

// UpdateLoadBalancerRequest is PATCH /load-balancers/{id}.
type UpdateLoadBalancerRequest struct {
	Name             *string            `json:"name,omitempty"`
	ProtectionDelete *bool              `json:"protection_delete,omitempty"`
	Labels           *map[string]string `json:"labels,omitempty"`
	ProjectID        *int64             `json:"project_id,omitempty"`
}

func (c *Client) ListLoadBalancers(ctx context.Context, labelSelector string) ([]LoadBalancer, error) {
	return listAll[LoadBalancer](ctx, c, "/load-balancers", url.Values{"label_selector": {labelSelector}})
}

func (c *Client) GetLoadBalancer(ctx context.Context, id int64) (LoadBalancer, error) {
	var out one[LoadBalancer]
	err := c.Cloud(ctx, http.MethodGet, idPath("/load-balancers", id), nil, nil, &out)
	return out.Data, err
}

func (c *Client) CreateLoadBalancer(ctx context.Context, req CreateLoadBalancerRequest) (LoadBalancer, int64, error) {
	var out struct {
		Data     LoadBalancer `json:"data"`
		ActionID int64        `json:"action_id"`
	}
	err := c.Cloud(ctx, http.MethodPost, "/load-balancers", nil, req, &out)
	return out.Data, out.ActionID, err
}

func (c *Client) UpdateLoadBalancer(ctx context.Context, id int64, req UpdateLoadBalancerRequest) (LoadBalancer, error) {
	var out one[LoadBalancer]
	err := c.Cloud(ctx, http.MethodPatch, idPath("/load-balancers", id), nil, req, &out)
	return out.Data, err
}

func (c *Client) DeleteLoadBalancer(ctx context.Context, id int64) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodDelete, idPath("/load-balancers", id), nil, nil, &out)
	return out.ActionID, err
}

// LoadBalancerAction runs POST /load-balancers/{id}/actions/{op}.
func (c *Client) LoadBalancerAction(ctx context.Context, id int64, op string, body any) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodPost, idPath("/load-balancers", id)+"/actions/"+op, nil, body, &out)
	return out.ActionID, err
}

func (c *Client) AddLBService(ctx context.Context, id int64, svc LBService) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "add_service", svc)
}

func (c *Client) UpdateLBService(ctx context.Context, id, listenPort int64, svc LBService) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "update_service", map[string]any{"listen_port": listenPort, "service": svc})
}

func (c *Client) DeleteLBService(ctx context.Context, id, listenPort int64) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "delete_service", map[string]any{"listen_port": listenPort})
}

func (c *Client) AddLBTarget(ctx context.Context, id int64, t LBTargetInput) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "add_target", t)
}

func (c *Client) RemoveLBTarget(ctx context.Context, id, targetID int64) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "remove_target", map[string]any{"target_id": targetID})
}

func (c *Client) ChangeLBType(ctx context.Context, id int64, typ string) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "change_type", map[string]any{"type": typ})
}

func (c *Client) ChangeLBAlgorithm(ctx context.Context, id int64, algorithm string) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "change_algorithm", map[string]any{"algorithm": algorithm})
}

func (c *Client) AttachLBToNetwork(ctx context.Context, id, networkID int64) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "attach_to_network", map[string]any{"network_id": networkID})
}

func (c *Client) DetachLBFromNetwork(ctx context.Context, id int64) (int64, error) {
	return c.LoadBalancerAction(ctx, id, "detach_from_network", map[string]any{})
}
