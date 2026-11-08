package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ariaservice/terraform-provider-arianet/internal/client"
)

var (
	_ resource.Resource                = (*sshKeyResource)(nil)
	_ resource.ResourceWithImportState = (*sshKeyResource)(nil)
)

type sshKeyResource struct{ base }

func newSSHKeyResource() resource.Resource { return &sshKeyResource{} }

type sshKeyModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An account-wide SSH key, kept by core and served by arianet-api at `/api/v1/ssh-keys`. Use its ID in `arianet_server.ssh_key_ids`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute("SSH key ID."),
			"name": schema.StringAttribute{
				Required: true, Description: "Name of the key.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"public_key": schema.StringAttribute{
				Required: true, Description: "OpenSSH public key line, e.g. `file(\"~/.ssh/id_ed25519.pub\")`. Changing it replaces the key.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"is_default": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Whether the key is the account's default key.",
			},
			"fingerprint": computedStableString("SHA256 fingerprint."),
			"created_at":  computedStableString("Creation time."),
		},
	}
}

func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// sameKey compares two key lines ignoring surrounding and repeated whitespace,
// since core normalizes what it stores.
func sameKey(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

func (r *sshKeyResource) fill(m *sshKeyModel, k client.SSHKey) {
	m.ID = idString(k.ID)
	m.Name = types.StringValue(k.NameOrEmpty())
	if m.PublicKey.IsNull() || m.PublicKey.IsUnknown() || !sameKey(m.PublicKey.ValueString(), k.Key) {
		m.PublicKey = types.StringValue(k.Key)
	}
	m.IsDefault = types.BoolValue(k.IsDefault)
	m.Fingerprint = types.StringValue(k.Fingerprint)
	m.CreatedAt = types.StringValue(k.CreatedAt)
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := client.CreateSSHKeyRequest{Name: plan.Name.ValueString(), PublicKey: strings.TrimSpace(plan.PublicKey.ValueString())}
	if plan.IsDefault.ValueBool() {
		v := true
		body.IsDefault = &v
	}
	k, err := r.client.CreateSSHKey(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating SSH key failed", err)
		return
	}
	r.fill(&plan, k)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := parseID(&resp.Diagnostics, state.ID.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	k, err := r.client.GetSSHKey(ctx, id)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading SSH key failed", err)
		return
	}
	r.fill(&state, k)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := parseID(&resp.Diagnostics, state.ID.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	var body client.UpdateSSHKeyRequest
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		body.Name = &v
	}
	if !plan.IsDefault.Equal(state.IsDefault) {
		v := plan.IsDefault.ValueBool()
		body.IsDefault = &v
	}
	plan.ID = state.ID
	if body.Name == nil && body.IsDefault == nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	k, err := r.client.UpdateSSHKey(ctx, id, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating SSH key failed", err)
		return
	}
	r.fill(&plan, k)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := parseID(&resp.Diagnostics, state.ID.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteSSHKey(ctx, id); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Deleting SSH key failed", err)
	}
}
