package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

// mockAPI is an in-memory stand-in for cloud-service (behind /cloud/v1) and
// arianet-api's SSH keys (/api/v1), shaped like the real handlers.
type mockAPI struct {
	mu        sync.Mutex
	next      int64
	servers   map[int64]*client.Server
	backups   map[int64]bool
	firewalls map[int64]*mockFirewall
	networks  map[int64]*client.Network
	sshKeys   map[int64]*client.SSHKey
	actions   map[int64]int // polls so far
	requests  []string
	srv       *httptest.Server
}

type mockFirewall struct {
	fw        client.Firewall
	direct    map[int64]bool
	selectors map[string]bool
}

func newMockAPI(t *testing.T) *mockAPI {
	m := &mockAPI{
		next: 100, servers: map[int64]*client.Server{}, backups: map[int64]bool{},
		firewalls: map[int64]*mockFirewall{}, networks: map[int64]*client.Network{},
		sshKeys: map[int64]*client.SSHKey{}, actions: map[int64]int{},
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockAPI) URL() string { return m.srv.URL }

func (m *mockAPI) providerConfig() string {
	return fmt.Sprintf(`
provider "arianet" {
  endpoint      = %q
  token         = "test-token"
  poll_interval = "5ms"
}
`, m.srv.URL)
}

func (m *mockAPI) id() int64 { m.next++; return m.next }

func (m *mockAPI) action() int64 {
	id := m.id()
	m.actions[id] = 0
	return id
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not found", "code": "not_found"})
}

func page[T any](items []T) map[string]any {
	if items == nil {
		items = []T{}
	}
	return map[string]any{"data": items, "meta": map[string]any{"pagination": map[string]any{
		"page": 1, "per_page": 100, "previous_page": nil, "next_page": nil, "last_page": 1, "total_entries": len(items),
	}}}
}

func matchSelector(sel string, labels map[string]string) bool {
	for _, term := range strings.Split(sel, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(term), "=")
		if !ok || labels[k] != v {
			return false
		}
	}
	return true
}

func (m *mockAPI) firewallView(f *mockFirewall) client.Firewall {
	v := f.fw
	v.AppliedTo = []client.FirewallTarget{}
	all := map[int64]bool{}
	var direct []int64
	for id := range f.direct {
		direct = append(direct, id)
	}
	sort.Slice(direct, func(i, j int) bool { return direct[i] < direct[j] })
	for _, id := range direct {
		v.AppliedTo = append(v.AppliedTo, client.ServerTarget(id))
		all[id] = true
	}
	var sels []string
	for s := range f.selectors {
		sels = append(sels, s)
	}
	sort.Strings(sels)
	for _, s := range sels {
		v.AppliedTo = append(v.AppliedTo, client.SelectorTarget(s))
		for id, srv := range m.servers {
			if matchSelector(s, srv.Labels) {
				all[id] = true
			}
		}
	}
	v.ServerIDs = []int64{}
	for id := range all {
		v.ServerIDs = append(v.ServerIDs, id)
	}
	sort.Slice(v.ServerIDs, func(i, j int) bool { return v.ServerIDs[i] < v.ServerIDs[j] })
	if v.Rules == nil {
		v.Rules = []client.FirewallRule{}
	}
	if v.Labels == nil {
		v.Labels = map[string]string{}
	}
	return v
}

func pathID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id
}

func (m *mockAPI) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer test-token" {
		writeJSON(w, 401, map[string]any{"error": "Unauthorized", "code": "unauthorized"})
		return
	}
	mux := http.NewServeMux()
	m.routes(mux)
	mux.ServeHTTP(w, r)
}

func decodeBody(r *http.Request) map[string]any {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	return body
}

func asLabels(v any) map[string]string {
	out := map[string]string{}
	if mm, ok := v.(map[string]any); ok {
		for k, val := range mm {
			out[k] = fmt.Sprint(val)
		}
	}
	return out
}

func asInt64s(v any) []int64 {
	var out []int64
	if arr, ok := v.([]any); ok {
		for _, x := range arr {
			if f, ok := x.(float64); ok {
				out = append(out, int64(f))
			}
		}
	}
	return out
}

func (m *mockAPI) routes(mux *http.ServeMux) {
	const c = "/cloud/v1"

	mux.HandleFunc("GET "+c+"/actions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		polls, ok := m.actions[id]
		if !ok {
			notFound(w)
			return
		}
		m.actions[id] = polls + 1
		status := "running"
		if polls >= 1 {
			status = "success"
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"id": id, "command": "x", "status": status, "progress": 50 * (polls + 1)}})
	})

	// catalog
	mux.HandleFunc("GET "+c+"/datacenters", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []client.Datacenter{
			{Code: "eu-central-1", Name: "Frankfurt", Country: "DE", City: "Frankfurt", Status: "active", Orderable: true, Features: []string{"ipv6", "private_networks"}, IPv6: true},
			{Code: "tr-ist-1", Name: "Istanbul", Country: "TR", City: "Istanbul", Status: "active", Orderable: true, Features: []string{}},
		}})
	})
	mux.HandleFunc("GET "+c+"/images", func(w http.ResponseWriter, r *http.Request) {
		all := []client.Image{
			{ID: "ubuntu-24.04", Image: "ubuntu-24.04", Type: "system", Name: "Ubuntu 24.04", Datacenters: []string{"eu-central-1", "tr-ist-1"}, MinDiskGB: 10, Status: "available", Labels: map[string]string{}},
			{ID: "debian-12", Image: "debian-12", Type: "system", Name: "Debian 12", Datacenters: []string{"eu-central-1"}, MinDiskGB: 10, Status: "available", Labels: map[string]string{}},
			{ID: "docker", Image: "docker", Type: "app", Name: "Docker", Datacenters: []string{"eu-central-1"}, MinDiskGB: 20, Status: "available", Labels: map[string]string{}},
		}
		q := r.URL.Query()
		var out []client.Image
		for _, img := range all {
			if t := q.Get("type"); t != "" && img.Type != t {
				continue
			}
			if dc := q.Get("datacenter"); dc != "" && !strings.Contains(strings.Join(img.Datacenters, ","), dc) {
				continue
			}
			out = append(out, img)
		}
		writeJSON(w, 200, page(out))
	})
	mux.HandleFunc("GET "+c+"/plans", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []map[string]any{
			{"id": 1, "datacenter": "eu-central-1", "name": "CX11", "vcpu": 1, "ram_mb": 2048, "disk_gb": 20, "traffic_gb": nil, "hourly_price": 0.01, "monthly_price": 5, "currency": "EUR", "is_active": true, "availability": map[string]any{"available": true, "known": true}},
			{"id": 2, "datacenter": "eu-central-1", "name": "CX21", "vcpu": 2, "ram_mb": 4096, "disk_gb": 40, "traffic_gb": 20000, "hourly_price": 0.02, "monthly_price": 10, "currency": "EUR", "is_active": true, "availability": map[string]any{"available": false, "known": true, "reason": "ram"}},
		}})
	})
	mux.HandleFunc("GET "+c+"/pricing", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []map[string]any{
			{"datacenter": "eu-central-1", "sku": "volume_gb", "unit": "gb", "hourly_price": 0.0001, "monthly_price": "0.05", "currency": "EUR", "is_active": true},
			{"datacenter": "eu-central-1", "sku": "backup", "unit": "percent", "percent": 20, "currency": "EUR"},
		}})
	})
	mux.HandleFunc("GET "+c+"/load-balancer-types", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []client.LoadBalancerType{{Code: "lb11", Name: "LB11", MaxServices: 5, MaxTargets: 25, MaxConnections: 10000}}})
	})

	// servers
	mux.HandleFunc("GET "+c+"/servers", func(w http.ResponseWriter, r *http.Request) {
		var out []client.Server
		sel := r.URL.Query().Get("label_selector")
		for _, s := range m.servers {
			if sel == "" || matchSelector(sel, s.Labels) {
				out = append(out, *s)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		writeJSON(w, 200, page(out))
	})
	mux.HandleFunc("POST "+c+"/servers", func(w http.ResponseWriter, r *http.Request) {
		b := decodeBody(r)
		name, _ := b["name"].(string)
		if name == "" {
			writeJSON(w, 422, map[string]any{"error": "The name may contain letters, digits, dots and dashes, up to 63 characters.", "code": "invalid_name"})
			return
		}
		id := m.id()
		project := int64(1)
		if p, ok := b["project_id"].(float64); ok {
			project = int64(p)
		}
		s := &client.Server{
			ID: id, Name: name, Status: "running", Datacenter: fmt.Sprint(b["datacenter"]), PlanID: int64(b["plan_id"].(float64)),
			DiskGB: 40, Image: fmt.Sprint(b["image"]), IPv4: fmt.Sprintf("203.0.113.%d", id%250), Labels: asLabels(b["labels"]),
			ProjectID: project, CreatedAt: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC),
		}
		m.servers[id] = s
		if v, _ := b["backups"].(bool); v {
			m.backups[id] = true
		}
		for _, fid := range asInt64s(b["firewall_ids"]) {
			if f, ok := m.firewalls[fid]; ok {
				f.direct[id] = true
			}
		}
		writeJSON(w, 201, map[string]any{"data": s, "action_id": m.action(), "root_password": "s3cret", "servers": []any{}})
	})
	mux.HandleFunc("GET "+c+"/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		s, ok := m.servers[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, 200, map[string]any{"data": s})
	})
	mux.HandleFunc("PATCH "+c+"/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		s, ok := m.servers[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		b := decodeBody(r)
		if v, ok := b["name"].(string); ok {
			s.Name = v
		}
		if v, ok := b["labels"]; ok {
			s.Labels = asLabels(v)
		}
		if v, ok := b["protection_delete"].(bool); ok {
			s.ProtectionDelete = v
		}
		if v, ok := b["protection_rebuild"].(bool); ok {
			s.ProtectionRebuild = v
		}
		if v, ok := b["project_id"].(float64); ok {
			s.ProjectID = int64(v)
		}
		writeJSON(w, 200, map[string]any{"data": s})
	})
	mux.HandleFunc("DELETE "+c+"/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		s, ok := m.servers[id]
		if !ok {
			notFound(w)
			return
		}
		if s.ProtectionDelete {
			writeJSON(w, 423, map[string]any{"error": "The server is protected.", "code": "protected"})
			return
		}
		delete(m.servers, id)
		for _, f := range m.firewalls {
			delete(f.direct, id)
		}
		writeJSON(w, 202, map[string]any{"action_id": m.action()})
	})
	mux.HandleFunc("POST "+c+"/servers/{id}/actions/{op}", func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		s, ok := m.servers[id]
		if !ok {
			notFound(w)
			return
		}
		b := decodeBody(r)
		switch r.PathValue("op") {
		case "change_plan":
			s.PlanID = int64(b["plan_id"].(float64))
		case "enable_backup":
			m.backups[id] = true
		case "disable_backup":
			delete(m.backups, id)
		default:
			notFound(w)
			return
		}
		writeJSON(w, 202, map[string]any{"action_id": m.action()})
	})
	mux.HandleFunc("GET "+c+"/servers/{id}/backups", func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		if _, ok := m.servers[id]; !ok {
			notFound(w)
			return
		}
		var plan any
		if m.backups[id] {
			plan = map[string]any{"server_id": id, "window_hour": 2, "retention": 7}
		}
		writeJSON(w, 200, map[string]any{"data": []any{}, "plan": plan, "slots": 7})
	})

	// firewalls
	mux.HandleFunc("GET "+c+"/firewalls", func(w http.ResponseWriter, r *http.Request) {
		var out []client.Firewall
		for _, f := range m.firewalls {
			out = append(out, m.firewallView(f))
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		writeJSON(w, 200, page(out))
	})
	decodeRules := func(v any) []client.FirewallRule {
		raw, _ := json.Marshal(v)
		var rules []client.FirewallRule
		_ = json.Unmarshal(raw, &rules)
		return rules
	}
	mux.HandleFunc("POST "+c+"/firewalls", func(w http.ResponseWriter, r *http.Request) {
		b := decodeBody(r)
		id := m.id()
		f := &mockFirewall{
			fw: client.Firewall{ID: id, Name: fmt.Sprint(b["name"]), Rules: decodeRules(b["rules"]), Labels: asLabels(b["labels"]),
				ProjectID: 1, CreatedAt: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)},
			direct: map[int64]bool{}, selectors: map[string]bool{},
		}
		for _, sid := range asInt64s(b["server_ids"]) {
			f.direct[sid] = true
		}
		m.firewalls[id] = f
		var action int64
		if len(f.direct) > 0 {
			action = m.action()
		}
		writeJSON(w, 201, map[string]any{"data": m.firewallView(f), "action_id": action})
	})
	mux.HandleFunc("GET "+c+"/firewalls/{id}", func(w http.ResponseWriter, r *http.Request) {
		f, ok := m.firewalls[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, 200, map[string]any{"data": m.firewallView(f)})
	})
	mux.HandleFunc("PUT "+c+"/firewalls/{id}", func(w http.ResponseWriter, r *http.Request) {
		f, ok := m.firewalls[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		b := decodeBody(r)
		f.fw.Name = fmt.Sprint(b["name"])
		f.fw.Rules = decodeRules(b["rules"])
		writeJSON(w, 200, map[string]any{"data": m.firewallView(f), "action_id": m.action()})
	})
	mux.HandleFunc("PATCH "+c+"/firewalls/{id}", func(w http.ResponseWriter, r *http.Request) {
		f, ok := m.firewalls[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		b := decodeBody(r)
		if v, ok := b["labels"]; ok {
			f.fw.Labels = asLabels(v)
		}
		if v, ok := b["protection_delete"].(bool); ok {
			f.fw.ProtectionDelete = v
		}
		writeJSON(w, 200, map[string]any{"data": m.firewallView(f)})
	})
	mux.HandleFunc("DELETE "+c+"/firewalls/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		if _, ok := m.firewalls[id]; !ok {
			notFound(w)
			return
		}
		delete(m.firewalls, id)
		writeJSON(w, 202, map[string]any{"action_id": m.action()})
	})
	mux.HandleFunc("POST "+c+"/firewalls/{id}/actions/{op}", func(w http.ResponseWriter, r *http.Request) {
		f, ok := m.firewalls[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		var body struct {
			ApplyTo    []client.FirewallTarget `json:"apply_to"`
			RemoveFrom []client.FirewallTarget `json:"remove_from"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		apply := r.PathValue("op") == "apply_to_resources"
		for _, t := range append(body.ApplyTo, body.RemoveFrom...) {
			switch {
			case t.Type == "server" && t.Server != nil:
				if apply {
					f.direct[t.Server.ID] = true
				} else {
					delete(f.direct, t.Server.ID)
				}
			case t.Type == "label_selector" && t.LabelSelector != nil:
				if apply {
					f.selectors[t.LabelSelector.Selector] = true
				} else {
					delete(f.selectors, t.LabelSelector.Selector)
				}
			default:
				writeJSON(w, 422, map[string]any{"error": "Each target is a server {id} or a label_selector {selector}.", "code": "invalid_target"})
				return
			}
		}
		writeJSON(w, 202, map[string]any{"action_id": m.action()})
	})

	// networks
	mux.HandleFunc("POST "+c+"/networks", func(w http.ResponseWriter, r *http.Request) {
		b := decodeBody(r)
		id := m.id()
		ipRange := fmt.Sprint(b["ip_range"])
		subnet := ipRange
		if s, ok := b["subnet_ip_range"].(string); ok && s != "" {
			subnet = s
		}
		n := &client.Network{ID: id, Name: fmt.Sprint(b["name"]), Datacenter: fmt.Sprint(b["datacenter"]), IPRange: ipRange,
			Status: "available", Labels: asLabels(b["labels"]), ProjectID: 1, Routes: []client.Route{}, Servers: []client.NetworkServer{},
			Subnets:   []client.Subnet{{ID: m.id(), IPRange: subnet, Status: "available", Gateway: strings.TrimSuffix(subnet, "0/24") + "1"}},
			CreatedAt: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
		m.networks[id] = n
		writeJSON(w, 201, map[string]any{"data": n, "action_id": m.action()})
	})
	mux.HandleFunc("GET "+c+"/networks", func(w http.ResponseWriter, r *http.Request) {
		var out []client.Network
		for _, n := range m.networks {
			out = append(out, *n)
		}
		writeJSON(w, 200, page(out))
	})
	mux.HandleFunc("GET "+c+"/networks/{id}", func(w http.ResponseWriter, r *http.Request) {
		n, ok := m.networks[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, 200, map[string]any{"data": n})
	})
	mux.HandleFunc("PATCH "+c+"/networks/{id}", func(w http.ResponseWriter, r *http.Request) {
		n, ok := m.networks[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		b := decodeBody(r)
		if v, ok := b["name"].(string); ok {
			n.Name = v
		}
		if v, ok := b["labels"]; ok {
			n.Labels = asLabels(v)
		}
		if v, ok := b["protection_delete"].(bool); ok {
			n.ProtectionDelete = v
		}
		writeJSON(w, 200, map[string]any{"data": n})
	})
	mux.HandleFunc("DELETE "+c+"/networks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		if _, ok := m.networks[id]; !ok {
			notFound(w)
			return
		}
		delete(m.networks, id)
		writeJSON(w, 202, map[string]any{"action_id": m.action()})
	})
	mux.HandleFunc("POST "+c+"/networks/{id}/subnets", func(w http.ResponseWriter, r *http.Request) {
		n, ok := m.networks[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		b := decodeBody(r)
		sn := client.Subnet{ID: m.id(), IPRange: fmt.Sprint(b["ip_range"]), Status: "available", Gateway: "10.0.9.1"}
		n.Subnets = append(n.Subnets, sn)
		writeJSON(w, 202, map[string]any{"data": sn, "action_id": m.action()})
	})
	mux.HandleFunc("DELETE "+c+"/networks/{id}/subnets/{subnet}", func(w http.ResponseWriter, r *http.Request) {
		n, ok := m.networks[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		sid := pathID(r, "subnet")
		kept := []client.Subnet{}
		for _, sn := range n.Subnets {
			if sn.ID != sid {
				kept = append(kept, sn)
			}
		}
		n.Subnets = kept
		writeJSON(w, 202, map[string]any{"action_id": m.action()})
	})
	mux.HandleFunc("PUT "+c+"/networks/{id}/routes", func(w http.ResponseWriter, r *http.Request) {
		n, ok := m.networks[pathID(r, "id")]
		if !ok {
			notFound(w)
			return
		}
		var body struct {
			Routes []client.Route `json:"routes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Routes == nil {
			body.Routes = []client.Route{}
		}
		n.Routes = body.Routes
		writeJSON(w, 202, map[string]any{"action_id": m.action()})
	})

	// SSH keys (arianet-api envelope)
	const a = "/api/v1"
	mux.HandleFunc("GET "+a+"/ssh-keys", func(w http.ResponseWriter, r *http.Request) {
		var out []client.SSHKey
		for _, k := range m.sshKeys {
			out = append(out, *k)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		writeJSON(w, 200, map[string]any{"success": true, "data": out,
			"pagination": map[string]any{"current_page": 1, "per_page": 100, "total": len(out), "last_page": 1}})
	})
	mux.HandleFunc("GET "+a+"/ssh-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		k, ok := m.sshKeys[pathID(r, "id")]
		if !ok {
			writeJSON(w, 404, map[string]any{"success": false, "error": map[string]any{"code": "NOT_FOUND", "message": "SSH key not found"}})
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": k})
	})
	mux.HandleFunc("POST "+a+"/ssh-keys", func(w http.ResponseWriter, r *http.Request) {
		b := decodeBody(r)
		name := fmt.Sprint(b["name"])
		key := strings.Join(strings.Fields(fmt.Sprint(b["public_key"])), " ")
		isDefault, _ := b["is_default"].(bool)
		k := &client.SSHKey{ID: m.id(), Name: &name, Key: key, Fingerprint: "SHA256:mock", IsDefault: isDefault, CreatedAt: "2026-11-08T10:00:00Z"}
		m.sshKeys[k.ID] = k
		writeJSON(w, 201, map[string]any{"success": true, "data": k})
	})
	mux.HandleFunc("PUT "+a+"/ssh-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		k, ok := m.sshKeys[pathID(r, "id")]
		if !ok {
			writeJSON(w, 404, map[string]any{"success": false, "error": map[string]any{"code": "NOT_FOUND", "message": "SSH key not found"}})
			return
		}
		b := decodeBody(r)
		if v, ok := b["name"].(string); ok {
			k.Name = &v
		}
		if v, ok := b["is_default"].(bool); ok {
			k.IsDefault = v
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": k})
	})
	mux.HandleFunc("DELETE "+a+"/ssh-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		if _, ok := m.sshKeys[id]; !ok {
			writeJSON(w, 404, map[string]any{"success": false, "error": map[string]any{"code": "NOT_FOUND", "message": "SSH key not found"}})
			return
		}
		delete(m.sshKeys, id)
		writeJSON(w, 200, map[string]any{"success": true, "message": "SSH key deleted successfully"})
	})
}
