package provider

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

const (
	defaultCreateTimeout = 30 * time.Minute
	defaultUpdateTimeout = 30 * time.Minute
	defaultDeleteTimeout = 20 * time.Minute
)

// keyedMutex serializes read-modify-write calls on one remote object (a
// network's route set) within one provider process.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newKeyedMutex() *keyedMutex { return &keyedMutex{locks: map[string]*sync.Mutex{}} }

func (k *keyedMutex) Lock(key string) func() {
	k.mu.Lock()
	l, ok := k.locks[key]
	if !ok {
		l = &sync.Mutex{}
		k.locks[key] = l
	}
	k.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// base is embedded by every resource and data source for Configure.
type base struct {
	client *client.Client
	locks  *keyedMutex
}

func (b *base) configure(data any, diags *diag.Diagnostics) {
	if data == nil {
		return
	}
	pd, ok := data.(*providerData)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("expected *providerData, got %T", data))
		return
	}
	b.client = pd.client
	b.locks = pd.locks
}

func (b *base) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	b.configure(req.ProviderData, &resp.Diagnostics)
}

// dsBase is base for data sources.
type dsBase struct{ base }

func (b *dsBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	b.configure(req.ProviderData, &resp.Diagnostics)
}

// ---------------------------------------------------------------- ids

func parseID(diags *diag.Diagnostics, raw string) int64 {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		diags.AddError("Invalid ID", fmt.Sprintf("%q is not a numeric arianet ID", raw))
		return 0
	}
	return id
}

func idString(id int64) types.String { return types.StringValue(strconv.FormatInt(id, 10)) }

// compositeID joins parts with "/".
func compositeID(parts ...string) string { return strings.Join(parts, "/") }

// splitID splits "a/b" into its two parts.
func splitID(diags *diag.Diagnostics, raw, format string) (string, string) {
	a, b, ok := strings.Cut(raw, "/")
	if !ok || a == "" || b == "" {
		diags.AddError("Invalid ID", fmt.Sprintf("%q is not of the form %s", raw, format))
		return "", ""
	}
	return a, b
}

// ---------------------------------------------------------------- values

func labelsValue(l map[string]string) types.Map {
	vals := map[string]attr.Value{}
	for k, v := range l {
		vals[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, vals)
}

func labelsFrom(ctx context.Context, m types.Map, diags *diag.Diagnostics) map[string]string {
	out := map[string]string{}
	if m.IsNull() || m.IsUnknown() {
		return out
	}
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out
}

func int64SetValue(ids []int64) types.Set {
	vals := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, types.Int64Value(id))
	}
	return types.SetValueMust(types.Int64Type, vals)
}

func int64ListValue(ids []int64) types.List {
	vals := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, types.Int64Value(id))
	}
	return types.ListValueMust(types.Int64Type, vals)
}

func int64sFrom(ctx context.Context, s types.Set, diags *diag.Diagnostics) []int64 {
	var out []int64
	if s.IsNull() || s.IsUnknown() {
		return out
	}
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func stringsFromSet(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	var out []string
	if s.IsNull() || s.IsUnknown() {
		return out
	}
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	sort.Strings(out)
	return out
}

func stringsFromList(ctx context.Context, l types.List, diags *diag.Diagnostics) []string {
	var out []string
	if l.IsNull() || l.IsUnknown() {
		return out
	}
	diags.Append(l.ElementsAs(ctx, &out, false)...)
	return out
}

func stringListValue(ss []string) types.List {
	vals := make([]attr.Value, 0, len(ss))
	for _, s := range ss {
		vals = append(vals, types.StringValue(s))
	}
	return types.ListValueMust(types.StringType, vals)
}

func stringSetValue(ss []string) types.Set {
	vals := make([]attr.Value, 0, len(ss))
	for _, s := range ss {
		vals = append(vals, types.StringValue(s))
	}
	return types.SetValueMust(types.StringType, vals)
}

// diffInt64 returns what is in want but not have, and in have but not want.
func diffInt64(have, want []int64) (add, remove []int64) {
	h := map[int64]bool{}
	for _, v := range have {
		h[v] = true
	}
	w := map[int64]bool{}
	for _, v := range want {
		w[v] = true
		if !h[v] {
			add = append(add, v)
		}
	}
	for _, v := range have {
		if !w[v] {
			remove = append(remove, v)
		}
	}
	return add, remove
}

func diffStrings(have, want []string) (add, remove []string) {
	h := map[string]bool{}
	for _, v := range have {
		h[v] = true
	}
	w := map[string]bool{}
	for _, v := range want {
		w[v] = true
		if !h[v] {
			add = append(add, v)
		}
	}
	for _, v := range have {
		if !w[v] {
			remove = append(remove, v)
		}
	}
	return add, remove
}

func optionalInt64(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	n := v.ValueInt64()
	return &n
}

func ptrInt64Value(p *int64) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*p)
}

func ptrStringValue(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func timeValue(t time.Time) types.String {
	if t.IsZero() {
		return types.StringValue("")
	}
	return types.StringValue(t.UTC().Format(time.RFC3339))
}

// ---------------------------------------------------------------- schema pieces

func idAttribute(desc string) schema.StringAttribute {
	return schema.StringAttribute{
		Computed:      true,
		Description:   desc,
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func labelsAttribute() schema.MapAttribute {
	return schema.MapAttribute{
		ElementType: types.StringType,
		Optional:    true,
		Computed:    true,
		Default:     mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})),
		Description: "User-defined labels (key/value pairs). Lists can be filtered by them with a label selector.",
	}
}

func protectionDeleteAttribute() schema.BoolAttribute {
	return schema.BoolAttribute{
		Optional:    true,
		Computed:    true,
		Default:     booldefault.StaticBool(false),
		Description: "Whether the resource is protected from deletion. Must be set to false before the resource can be destroyed.",
	}
}

func projectIDAttribute() schema.Int64Attribute {
	return schema.Int64Attribute{
		Optional:      true,
		Computed:      true,
		Description:   "ID of the project the resource is in. Defaults to the account's default project; changing it moves the resource.",
		PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
	}
}

func computedString(desc string) schema.StringAttribute {
	return schema.StringAttribute{Computed: true, Description: desc}
}

func computedStableString(desc string) schema.StringAttribute {
	return schema.StringAttribute{Computed: true, Description: desc, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}
}

// ---------------------------------------------------------------- diagnostics

func addAPIError(diags *diag.Diagnostics, summary string, err error) {
	diags.AddError(summary, err.Error())
}

// withTimeout returns a context bounded by d.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

// diagnosticsAlias keeps helper signatures short.
type diagnosticsAlias = diag.Diagnostics

// wait returns a function that waits for the action an API call queued, so
// a call and its wait read as one: r.wait(ctx)(r.client.X(ctx, ...)).
func (b *base) wait(ctx context.Context) func(actionID int64, err error) error {
	return func(actionID int64, err error) error {
		if err != nil {
			return err
		}
		return b.client.WaitAction(ctx, actionID)
	}
}
