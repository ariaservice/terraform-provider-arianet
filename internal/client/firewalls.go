package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// FirewallRule is one rule of a firewall.
type FirewallRule struct {
	Direction      string   `json:"direction"`
	Protocol       string   `json:"protocol"`
	Port           *string  `json:"port,omitempty"`
	SourceIPs      []string `json:"source_ips,omitempty"`
	DestinationIPs []string `json:"destination_ips,omitempty"`
	Description    *string  `json:"description,omitempty"`
}

// FirewallTargetServer is {"id": n}.
type FirewallTargetServer struct {
	ID int64 `json:"id"`
}

// FirewallTargetSelector is {"selector": "..."}.
type FirewallTargetSelector struct {
	Selector string `json:"selector"`
}

// FirewallTarget is what a firewall is applied to: a server or a label selector.
type FirewallTarget struct {
	Type          string                  `json:"type"`
	Server        *FirewallTargetServer   `json:"server,omitempty"`
	LabelSelector *FirewallTargetSelector `json:"label_selector,omitempty"`
}

// ServerTarget is a server target.
func ServerTarget(id int64) FirewallTarget {
	return FirewallTarget{Type: "server", Server: &FirewallTargetServer{ID: id}}
}

// SelectorTarget is a label selector target.
func SelectorTarget(sel string) FirewallTarget {
	return FirewallTarget{Type: "label_selector", LabelSelector: &FirewallTargetSelector{Selector: sel}}
}

// Firewall is a firewall.
type Firewall struct {
	ID                int64             `json:"id"`
	Name              string            `json:"name"`
	Rules             []FirewallRule    `json:"rules"`
	ServerIDs         []int64           `json:"server_ids"`
	SelectorServerIDs []int64           `json:"selector_server_ids"`
	AppliedTo         []FirewallTarget  `json:"applied_to"`
	Labels            map[string]string `json:"labels"`
	ProtectionDelete  bool              `json:"protection_delete"`
	ProjectID         int64             `json:"project_id"`
	CreatedAt         time.Time         `json:"created_at"`
}

// DirectServerIDs are the servers the firewall is applied to directly.
func (f Firewall) DirectServerIDs() []int64 {
	out := []int64{}
	for _, t := range f.AppliedTo {
		if t.Type == "server" && t.Server != nil {
			out = append(out, t.Server.ID)
		}
	}
	return out
}

// Selectors are the label selectors the firewall is applied to.
func (f Firewall) Selectors() []string {
	out := []string{}
	for _, t := range f.AppliedTo {
		if t.Type == "label_selector" && t.LabelSelector != nil {
			out = append(out, t.LabelSelector.Selector)
		}
	}
	return out
}

// CreateFirewallRequest is POST /firewalls.
type CreateFirewallRequest struct {
	Name      string            `json:"name"`
	Rules     []FirewallRule    `json:"rules"`
	ServerIDs []int64           `json:"server_ids,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	ProjectID *int64            `json:"project_id,omitempty"`
}

func (c *Client) ListFirewalls(ctx context.Context, labelSelector string) ([]Firewall, error) {
	return listAll[Firewall](ctx, c, "/firewalls", url.Values{"label_selector": {labelSelector}})
}

func (c *Client) GetFirewall(ctx context.Context, id int64) (Firewall, error) {
	var out one[Firewall]
	err := c.Cloud(ctx, http.MethodGet, idPath("/firewalls", id), nil, nil, &out)
	return out.Data, err
}

func (c *Client) CreateFirewall(ctx context.Context, req CreateFirewallRequest) (Firewall, int64, error) {
	if req.Rules == nil {
		req.Rules = []FirewallRule{}
	}
	var out struct {
		Data     Firewall `json:"data"`
		ActionID int64    `json:"action_id"`
	}
	err := c.Cloud(ctx, http.MethodPost, "/firewalls", nil, req, &out)
	return out.Data, out.ActionID, err
}

// SetFirewallRules is PUT /firewalls/{id}: name and the whole rule set.
func (c *Client) SetFirewallRules(ctx context.Context, id int64, name string, rules []FirewallRule) (Firewall, int64, error) {
	if rules == nil {
		rules = []FirewallRule{}
	}
	var out struct {
		Data     Firewall `json:"data"`
		ActionID int64    `json:"action_id"`
	}
	err := c.Cloud(ctx, http.MethodPut, idPath("/firewalls", id), nil, map[string]any{"name": name, "rules": rules}, &out)
	return out.Data, out.ActionID, err
}

func (c *Client) UpdateFirewall(ctx context.Context, id int64, req UpdateRequest) (Firewall, error) {
	var out one[Firewall]
	err := c.Cloud(ctx, http.MethodPatch, idPath("/firewalls", id), nil, req, &out)
	return out.Data, err
}

func (c *Client) DeleteFirewall(ctx context.Context, id int64) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodDelete, idPath("/firewalls", id), nil, nil, &out)
	return out.ActionID, err
}

// ApplyFirewall applies a firewall to servers and label selectors.
func (c *Client) ApplyFirewall(ctx context.Context, id int64, targets []FirewallTarget) (int64, error) {
	if len(targets) == 0 {
		return 0, nil
	}
	var out actionRef
	err := c.Cloud(ctx, http.MethodPost, idPath("/firewalls", id)+"/actions/apply_to_resources", nil, map[string]any{"apply_to": targets}, &out)
	return out.ActionID, err
}

// RemoveFirewall removes a firewall from servers and label selectors.
func (c *Client) RemoveFirewall(ctx context.Context, id int64, targets []FirewallTarget) (int64, error) {
	if len(targets) == 0 {
		return 0, nil
	}
	var out actionRef
	err := c.Cloud(ctx, http.MethodPost, idPath("/firewalls", id)+"/actions/remove_from_resources", nil, map[string]any{"remove_from": targets}, &out)
	return out.ActionID, err
}
