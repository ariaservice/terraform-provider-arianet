package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Volume is a block storage volume.
type Volume struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	Datacenter       string            `json:"datacenter"`
	SizeGB           int64             `json:"size_gb"`
	Status           string            `json:"status"`
	ServerID         *int64            `json:"server_id"`
	Device           string            `json:"device"`
	DiskBus          string            `json:"disk_bus"`
	DeviceIDPrefix   string            `json:"device_id_prefix"`
	ProtectionDelete bool              `json:"protection_delete"`
	Labels           map[string]string `json:"labels"`
	Filesystem       string            `json:"filesystem"`
	ProjectID        int64             `json:"project_id"`
	CreatedAt        time.Time         `json:"created_at"`
}

// CreateVolumeRequest is POST /volumes.
type CreateVolumeRequest struct {
	Datacenter string            `json:"datacenter"`
	Name       string            `json:"name"`
	SizeGB     int64             `json:"size_gb"`
	ServerID   int64             `json:"server_id,omitempty"`
	Filesystem string            `json:"filesystem,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	ProjectID  *int64            `json:"project_id,omitempty"`
}

// UpdateVolumeRequest is PATCH /volumes/{id}.
type UpdateVolumeRequest struct {
	Name             *string            `json:"name,omitempty"`
	ProtectionDelete *bool              `json:"protection_delete,omitempty"`
	Labels           *map[string]string `json:"labels,omitempty"`
	Filesystem       *string            `json:"filesystem,omitempty"`
	ProjectID        *int64             `json:"project_id,omitempty"`
}

func (c *Client) ListVolumes(ctx context.Context, labelSelector string) ([]Volume, error) {
	return listAll[Volume](ctx, c, "/volumes", url.Values{"label_selector": {labelSelector}})
}

func (c *Client) GetVolume(ctx context.Context, id int64) (Volume, error) {
	var out one[Volume]
	err := c.Cloud(ctx, http.MethodGet, idPath("/volumes", id), nil, nil, &out)
	return out.Data, err
}

func (c *Client) CreateVolume(ctx context.Context, req CreateVolumeRequest) (Volume, int64, error) {
	var out struct {
		Data     Volume `json:"data"`
		ActionID int64  `json:"action_id"`
	}
	err := c.Cloud(ctx, http.MethodPost, "/volumes", nil, req, &out)
	return out.Data, out.ActionID, err
}

func (c *Client) UpdateVolume(ctx context.Context, id int64, req UpdateVolumeRequest) (Volume, error) {
	var out one[Volume]
	err := c.Cloud(ctx, http.MethodPatch, idPath("/volumes", id), nil, req, &out)
	return out.Data, err
}

func (c *Client) DeleteVolume(ctx context.Context, id int64) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodDelete, idPath("/volumes", id), nil, nil, &out)
	return out.ActionID, err
}

func (c *Client) volumeAction(ctx context.Context, id int64, op string, body any) (int64, error) {
	var out actionRef
	err := c.Cloud(ctx, http.MethodPost, idPath("/volumes", id)+"/actions/"+op, nil, body, &out)
	return out.ActionID, err
}

func (c *Client) AttachVolume(ctx context.Context, id, serverID int64) (int64, error) {
	return c.volumeAction(ctx, id, "attach", map[string]any{"server_id": serverID})
}

func (c *Client) DetachVolume(ctx context.Context, id int64) (int64, error) {
	return c.volumeAction(ctx, id, "detach", nil)
}

func (c *Client) ResizeVolume(ctx context.Context, id, sizeGB int64) (int64, error) {
	return c.volumeAction(ctx, id, "resize", map[string]any{"size_gb": sizeGB})
}
