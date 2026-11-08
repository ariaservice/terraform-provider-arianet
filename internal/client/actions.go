package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Action statuses of cloud-service.
const (
	ActionRunning = "running"
	ActionSuccess = "success"
	ActionError   = "error"
)

// ActionResource is the resource an action works on.
type ActionResource struct {
	Type string `json:"type"`
	ID   int64  `json:"id"`
}

// ActionErrorInfo is the error of a failed action.
type ActionErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Action is GET /actions/{id}.
type Action struct {
	ID         int64            `json:"id"`
	Command    string           `json:"command"`
	Status     string           `json:"status"`
	Progress   int              `json:"progress"`
	Resource   ActionResource   `json:"resource"`
	Error      *ActionErrorInfo `json:"error"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt *time.Time       `json:"finished_at"`
}

// ActionFailedError is an action that ended in status error.
type ActionFailedError struct {
	Action Action
}

func (e *ActionFailedError) Error() string {
	msg := fmt.Sprintf("action %d (%s on %s %d) failed", e.Action.ID, e.Action.Command, e.Action.Resource.Type, e.Action.Resource.ID)
	if e.Action.Error != nil {
		if e.Action.Error.Code != "" {
			msg += " [" + e.Action.Error.Code + "]"
		}
		if e.Action.Error.Message != "" {
			msg += ": " + e.Action.Error.Message
		}
	}
	return msg
}

// GetAction reads one action.
func (c *Client) GetAction(ctx context.Context, id int64) (Action, error) {
	var out one[Action]
	err := c.Cloud(ctx, http.MethodGet, idPath("/actions", id), nil, nil, &out)
	return out.Data, err
}

// WaitAction polls an action until it succeeds or fails, or ctx ends. An id
// of 0 means the API had nothing to do and returns at once.
func (c *Client) WaitAction(ctx context.Context, id int64) error {
	if id <= 0 {
		return nil
	}
	interval := c.pollInterval
	for {
		a, err := c.GetAction(ctx, id)
		if err != nil {
			return fmt.Errorf("waiting for action %d: %w", id, err)
		}
		switch a.Status {
		case ActionSuccess:
			return nil
		case ActionError:
			return &ActionFailedError{Action: a}
		}
		if err := c.sleep(ctx, interval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("timed out waiting for action %d (%s, %d%%)", id, a.Command, a.Progress)
			}
			return err
		}
		interval = interval * 3 / 2
		if interval > maxPollInterval {
			interval = maxPollInterval
		}
	}
}

// WaitActions waits for every non-zero action in turn.
func (c *Client) WaitActions(ctx context.Context, ids ...int64) error {
	for _, id := range ids {
		if err := c.WaitAction(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
