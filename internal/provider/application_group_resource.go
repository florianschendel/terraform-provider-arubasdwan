package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &applicationGroupResource{}
	_ resource.ResourceWithModifyPlan  = &applicationGroupResource{}
	_ resource.ResourceWithImportState = &applicationGroupResource{}
)

// applicationGroupResourceModel maps the resource schema data.
type applicationGroupResourceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Apps types.List   `tfsdk:"apps"`
}

// applicationGroupResource is the resource implementation.
type applicationGroupResource struct {
	client *client.Client
}

// NewApplicationGroupResource returns a new resource instance.
func NewApplicationGroupResource() resource.Resource {
	return &applicationGroupResource{}
}

// Metadata returns the resource type name.
func (r *applicationGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_group"
}

// Schema defines the schema for the resource.
func (r *applicationGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an application group (tag) in the Aruba SD-WAN Orchestrator. " +
			"Uses the /gms/rest/applicationDefinition/applicationTags API endpoints.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The identifier of the application group (same as name).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the application group. This is the key used to identify the group.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"apps": schema.ListAttribute{
				Description: "List of application names in this group.",
				Required:    true,
				ElementType: types.StringType,
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *applicationGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = apiClient
}

// applicationGroupDuplicateError builds the diagnostic for a name collision
// with an existing group.
func applicationGroupDuplicateError(existingName, plannedName string) (string, string) {
	consequence := "Groups are keyed by name, so applying would silently replace the existing group's member list."
	if existingName != plannedName {
		consequence = "Applying would add a second group whose name differs only in case, which makes references by name ambiguous."
	}
	return "Application group name already in use", fmt.Sprintf(
		"An application group named %q already exists on the Orchestrator. %s\n\n"+
			"If Terraform should manage the existing group, import it:\n\n"+
			"  terraform import arubasdwan_application_group.<name> %s\n\n"+
			"To define a separate group, choose a different name.",
		existingName, consequence, existingName,
	)
}

// ModifyPlan runs the duplicate-name check already at plan time, so a plan
// does not promise a create the apply would refuse. Name changes replace the
// resource, so the create leg covers renames as well. Only plans that create
// the resource call the API; when the listing fails, planning proceeds and
// the apply-time guard decides.
func (r *applicationGroupResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return
	}
	var plan applicationGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Name.IsUnknown() || plan.Name.IsNull() {
		return
	}

	// The state's own name never counts as a collision; a genuine rename
	// replaces the resource, and the old group is destroyed in the process.
	ignoreName := ""
	if !req.State.Raw.IsNull() {
		var state applicationGroupResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if plan.Name.ValueString() == state.Name.ValueString() {
			return
		}
		ignoreName = state.Name.ValueString()
	}

	existing, err := r.client.GetApplicationGroups()
	if err != nil {
		resp.Diagnostics.AddWarning(
			"Could not check application group names during planning",
			"Listing application groups failed: "+err.Error()+
				"\n\nThe duplicate check runs again during apply.",
		)
		return
	}
	for i := range existing {
		if existing[i].Name == ignoreName || !strings.EqualFold(existing[i].Name, plan.Name.ValueString()) {
			continue
		}
		summary, detail := applicationGroupDuplicateError(existing[i].Name, plan.Name.ValueString())
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
		return
	}
}

// Create creates the resource and sets the initial Terraform state.
//
// The Orchestrator keys application groups by name: creating a group whose
// name is already taken would silently replace the existing group's member
// list — and overlay ACLs and policies reference groups by name. Such a
// create is therefore rejected, like for the application classification
// resources.
func (r *applicationGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan applicationGroupResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	existing, err := r.client.GetApplicationGroups()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error checking existing application groups",
			"Could not list application groups: "+err.Error(),
		)
		return
	}
	for i := range existing {
		if !strings.EqualFold(existing[i].Name, plan.Name.ValueString()) {
			continue
		}
		summary, detail := applicationGroupDuplicateError(existing[i].Name, plan.Name.ValueString())
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
		return
	}

	var apps []string
	diags = plan.Apps.ElementsAs(ctx, &apps, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	group := client.ApplicationGroup{
		Name: plan.Name.ValueString(),
		Apps: apps,
	}

	err = r.client.CreateApplicationGroup(group)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating application group",
			"Could not create application group, unexpected error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(plan.Name.ValueString())

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Read refreshes the Terraform state with the latest data.
func (r *applicationGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state applicationGroupResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	group, err := r.client.GetApplicationGroup(name)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading application group",
			"Could not read application group "+name+": "+err.Error(),
		)
		return
	}

	// If the group no longer exists on the Orchestrator, remove it from state.
	if group == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(group.Name)
	state.Name = types.StringValue(group.Name)

	appsList, diags := types.ListValueFrom(ctx, types.StringType, group.Apps)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Apps = appsList

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *applicationGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan applicationGroupResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var apps []string
	diags = plan.Apps.ElementsAs(ctx, &apps, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	group := client.ApplicationGroup{
		Name: plan.Name.ValueString(),
		Apps: apps,
	}

	err := r.client.UpdateApplicationGroup(group)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating application group",
			"Could not update application group, unexpected error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(plan.Name.ValueString())

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *applicationGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state applicationGroupResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteApplicationGroup(state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting application group",
			"Could not delete application group, unexpected error: "+err.Error(),
		)
		return
	}
}

// ImportState imports a resource by its name.
func (r *applicationGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}
