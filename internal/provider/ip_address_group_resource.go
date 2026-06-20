package provider

import (
	"context"
	"fmt"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ipAddressGroupResource{}
	_ resource.ResourceWithImportState = &ipAddressGroupResource{}
)

// ipAddressGroupRuleModel maps a single rule inside an address group resource.
// Each rule contributes a set of included IPs/CIDRs, optionally minus excluded
// ones, plus references to other address groups.
type ipAddressGroupRuleModel struct {
	IncludedIPs    []types.String `tfsdk:"included_ips"`
	ExcludedIPs    []types.String `tfsdk:"excluded_ips"`
	IncludedGroups []types.String `tfsdk:"included_groups"`
	Comment        types.String   `tfsdk:"comment"`
}

type ipAddressGroupResourceModel struct {
	ID    types.String              `tfsdk:"id"`
	Name  types.String              `tfsdk:"name"`
	Rules []ipAddressGroupRuleModel `tfsdk:"rules"`
}

type ipAddressGroupResource struct {
	client *client.Client
}

func NewIPAddressGroupResource() resource.Resource {
	return &ipAddressGroupResource{}
}

func (r *ipAddressGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_address_group"
}

func (r *ipAddressGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an IP address group on the Aruba SD-WAN Orchestrator. " +
			"Address groups are reusable named collections of IP addresses/CIDRs and can be " +
			"referenced by ACLs and security policies. " +
			"Uses the /gms/rest/ipObjects/addressGroup API endpoints.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Same as the group name.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Unique name of the address group. Forces replacement on change.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"rules": schema.ListNestedAttribute{
				Description: "Ordered list of rules composing the group. Each rule contributes " +
					"included IPs/CIDRs, optional exclusions, and references to nested address groups.",
				Required: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"included_ips": schema.ListAttribute{
							Description: "List of included IPs or CIDRs (e.g. [\"10.0.0.0/8\", \"192.168.1.5/32\"]).",
							Optional:    true,
							ElementType: types.StringType,
						},
						"excluded_ips": schema.ListAttribute{
							Description: "List of explicitly excluded IPs or CIDRs.",
							Optional:    true,
							ElementType: types.StringType,
						},
						"included_groups": schema.ListAttribute{
							Description: "Names of other address groups to nest into this rule.",
							Optional:    true,
							ElementType: types.StringType,
						},
						"comment": schema.StringAttribute{
							Description: "Free-form comment for this rule. Defaults to \"\".",
							Optional:    true,
							Computed:    true,
							Default:     stringdefault.StaticString(""),
						},
					},
				},
			},
		},
	}
}

func (r *ipAddressGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = apiClient
}

// modelToIPAddressGroup converts the Terraform model to the client struct.
func modelToIPAddressGroup(m *ipAddressGroupResourceModel) client.IPAddressGroup {
	rules := make([]client.IPAddressGroupRule, 0, len(m.Rules))
	for _, r := range m.Rules {
		rules = append(rules, client.IPAddressGroupRule{
			IncludedIPs:    stringSliceFromTF(r.IncludedIPs),
			ExcludedIPs:    stringSliceFromTF(r.ExcludedIPs),
			IncludedGroups: stringSliceFromTF(r.IncludedGroups),
			Comment:        r.Comment.ValueString(),
		})
	}
	return client.IPAddressGroup{
		Name:  m.Name.ValueString(),
		Rules: rules,
	}
}

// ipAddressGroupToModel converts the client struct back into a Terraform model
// for storing in state.
func ipAddressGroupToModel(g *client.IPAddressGroup) ipAddressGroupResourceModel {
	rules := make([]ipAddressGroupRuleModel, 0, len(g.Rules))
	for _, r := range g.Rules {
		rules = append(rules, ipAddressGroupRuleModel{
			IncludedIPs:    stringSliceToTF(r.IncludedIPs),
			ExcludedIPs:    stringSliceToTF(r.ExcludedIPs),
			IncludedGroups: stringSliceToTF(r.IncludedGroups),
			Comment:        types.StringValue(r.Comment),
		})
	}
	return ipAddressGroupResourceModel{
		ID:    types.StringValue(g.Name),
		Name:  types.StringValue(g.Name),
		Rules: rules,
	}
}

// stringSliceFromTF flattens a slice of types.String into a plain []string,
// dropping null/unknown entries.
func stringSliceFromTF(in []types.String) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v.IsNull() || v.IsUnknown() {
			continue
		}
		out = append(out, v.ValueString())
	}
	return out
}

// stringSliceToTF wraps a []string into a slice of types.String values.
func stringSliceToTF(in []string) []types.String {
	out := make([]types.String, 0, len(in))
	for _, v := range in {
		out = append(out, types.StringValue(v))
	}
	return out
}

func (r *ipAddressGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ipAddressGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group := modelToIPAddressGroup(&plan)
	if err := r.client.CreateIPAddressGroup(group); err != nil {
		resp.Diagnostics.AddError("Error creating IP address group", err.Error())
		return
	}

	created, err := r.client.GetIPAddressGroup(group.Name)
	if err != nil {
		resp.Diagnostics.AddError("Error reading back created address group", err.Error())
		return
	}
	if created == nil {
		resp.Diagnostics.AddError("Address group not found after create",
			fmt.Sprintf("Address group %q was not returned by the Orchestrator after create.", group.Name))
		return
	}

	state := ipAddressGroupToModel(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ipAddressGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ipAddressGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.client.GetIPAddressGroup(state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading IP address group", err.Error())
		return
	}
	if group == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	newState := ipAddressGroupToModel(group)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *ipAddressGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ipAddressGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group := modelToIPAddressGroup(&plan)
	if err := r.client.UpdateIPAddressGroup(group); err != nil {
		resp.Diagnostics.AddError("Error updating IP address group", err.Error())
		return
	}

	updated, err := r.client.GetIPAddressGroup(group.Name)
	if err != nil {
		resp.Diagnostics.AddError("Error reading back updated address group", err.Error())
		return
	}
	if updated == nil {
		resp.Diagnostics.AddError("Address group not found after update",
			fmt.Sprintf("Address group %q was not returned by the Orchestrator after update.", group.Name))
		return
	}

	state := ipAddressGroupToModel(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ipAddressGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ipAddressGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteIPAddressGroup(state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error deleting IP address group", err.Error())
		return
	}
}

func (r *ipAddressGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
