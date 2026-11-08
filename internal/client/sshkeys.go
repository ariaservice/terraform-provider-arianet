package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// SSHKey is an account-wide SSH key, served by arianet-api from core.
type SSHKey struct {
	ID          int64   `json:"id"`
	Name        *string `json:"name"`
	Key         string  `json:"key"`
	Fingerprint string  `json:"fingerprint"`
	IsDefault   bool    `json:"is_default"`
	CreatedAt   string  `json:"created_at"`
}

// NameOrEmpty is the key's name.
func (k SSHKey) NameOrEmpty() string {
	if k.Name == nil {
		return ""
	}
	return *k.Name
}

// CreateSSHKeyRequest is POST /api/v1/ssh-keys.
type CreateSSHKeyRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	IsDefault *bool  `json:"is_default,omitempty"`
}

// UpdateSSHKeyRequest is PUT /api/v1/ssh-keys/{id}.
type UpdateSSHKeyRequest struct {
	Name      *string `json:"name,omitempty"`
	IsDefault *bool   `json:"is_default,omitempty"`
}

func (c *Client) GetSSHKey(ctx context.Context, id int64) (SSHKey, error) {
	var out SSHKey
	_, err := c.core(ctx, http.MethodGet, idPath("/ssh-keys", id), nil, nil, &out)
	return out, err
}

// ListSSHKeys pages through every key of the account.
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	all := []SSHKey{}
	for page := 1; page < 10000; page++ {
		var keys []SSHKey
		env, err := c.core(ctx, http.MethodGet, "/ssh-keys", url.Values{"page": {strconv.Itoa(page)}, "limit": {"100"}}, nil, &keys)
		if err != nil {
			return nil, err
		}
		all = append(all, keys...)
		if env.Pagination == nil || page >= env.Pagination.LastPage || len(keys) == 0 {
			break
		}
	}
	return all, nil
}

func (c *Client) CreateSSHKey(ctx context.Context, req CreateSSHKeyRequest) (SSHKey, error) {
	var out SSHKey
	_, err := c.core(ctx, http.MethodPost, "/ssh-keys", nil, req, &out)
	return out, err
}

func (c *Client) UpdateSSHKey(ctx context.Context, id int64, req UpdateSSHKeyRequest) (SSHKey, error) {
	var out SSHKey
	_, err := c.core(ctx, http.MethodPut, idPath("/ssh-keys", id), nil, req, &out)
	return out, err
}

func (c *Client) DeleteSSHKey(ctx context.Context, id int64) error {
	_, err := c.core(ctx, http.MethodDelete, idPath("/ssh-keys", id), nil, nil, nil)
	return err
}
