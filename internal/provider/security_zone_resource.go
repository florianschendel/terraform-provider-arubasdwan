package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface checks: ensure securityZoneResource implements both
// the Resource interface (for CRUD operations) and the ResourceWithImportState
// interface (for `terraform import` support).
var (
	_ resource.Resource                = &securityZoneResource{}
	_ resource.ResourceWithImportState = &securityZoneResource{}
)

// securityZoneResourceModel maps the Terraform resource schema to a Go struct.
// Each field corresponds to an attribute in the Terraform resource block:
//
//	resource "arubasdwan_security_zone" "example" {
//	  name = "DMZ"     # The zone name (required)
//	  # id is computed — assigned by the Orchestrator
//	}
type securityZoneResourceModel struct {
	ID   types.Int64  `tfsdk:"id"`   // Zone ID assigned by the Orchestrator (computed, read-only)
	Name types.String `tfsdk:"name"` // Zone name set by the user (required)
}

// securityZoneResource is the resource implementation. It holds a reference
// to the API client for making Orchestrator API calls.
type securityZoneResource struct {
	client *client.Client
}

// NewSecurityZoneResource is the factory function registered in provider.Resources().
// It returns a new, unconfigured resource instance. The client is set later
// in the Configure method.
func NewSecurityZoneResource() resource.Resource {
	return &securityZoneResource{}
}

// Metadata sets the resource type name. Combined with the provider type name
// ("arubasdwan"), this produces "arubasdwan_security_zone" — the name users
// use in their Terraform configuration.
func (r *securityZoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_zone"
}

// Schema defines the attributes that can be set on this resource.
//
// The "id" attribute is Computed (set by the Orchestrator) and uses
// UseStateForUnknown to preserve the ID between plan and apply — this tells
// Terraform that the ID won't change after initial creation.
//
// The "name" attribute is Required — users must provide a zone name.
func (r *securityZoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a security zone in the Aruba SD-WAN Orchestrator. " +
			"Uses the /gms/rest/zones API endpoints.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "The unique identifier of the security zone, assigned by the Orchestrator.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the security zone.",
				Required:    true,
			},
		},
	}
}

// Configure is called after the provider is configured. It retrieves the shared
// API client from ProviderData and stores it on the resource for use in CRUD methods.
func (r *securityZoneResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create handles the Terraform "create" lifecycle event. It:
//  1. Reads the planned state from the Terraform plan.
//  2. Calls the Orchestrator API to create the zone (which assigns a new ID).
//  3. Stores the created zone's ID and name into the Terraform state.
func (r *securityZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read the planned values from the Terraform configuration.
	var plan securityZoneResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call the API to create the zone. The client handles fetching the next
	// available ID, posting the complete zone set, and reading back the result.
	createdZone, err := r.client.CreateZone(plan.Name.ValueString(), "")
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating security zone",
			"Could not create security zone, unexpected error: "+err.Error(),
		)
		return
	}

	// Update the plan with the values returned by the API (especially the assigned ID).
	plan.ID = types.Int64Value(int64(createdZone.ID))
	plan.Name = types.StringValue(createdZone.Name)

	// Save the state. This persists the resource in Terraform's state file.
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Read handles the Terraform "refresh" lifecycle event. It:
//  1. Reads the current state (including the zone ID).
//  2. Fetches the zone from the Orchestrator API.
//  3. If the zone still exists, updates the state with the latest values.
//  4. If the zone was deleted externally, removes the resource from state
//     (Terraform will then plan to recreate it).
func (r *securityZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state securityZoneResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Fetch the zone by its ID from the Orchestrator.
	zone, err := r.client.GetZoneByID(int(state.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading security zone",
			"Could not read security zone ID "+strconv.FormatInt(state.ID.ValueInt64(), 10)+": "+err.Error(),
		)
		return
	}

	// If the zone no longer exists on the Orchestrator (deleted externally),
	// remove it from Terraform state. Terraform will then plan to recreate it.
	if zone == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Update state with the latest values from the Orchestrator.
	state.ID = types.Int64Value(int64(zone.ID))
	state.Name = types.StringValue(zone.Name)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Update handles the Terraform "update" lifecycle event. It:
//  1. Reads the planned (new) values from the Terraform plan.
//  2. Calls the Orchestrator API to update the zone (changing its name).
//  3. Updates the Terraform state with the confirmed values from the API.
//
// Note: Only the "name" attribute can be updated. The "id" is immutable
// (assigned by the Orchestrator during creation).
func (r *securityZoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan securityZoneResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Build the Zone struct with the updated name and existing ID.
	zone := client.Zone{
		ID:   int(plan.ID.ValueInt64()),
		Name: plan.Name.ValueString(),
	}

	// Call the API to update the zone using the read-modify-write pattern.
	updatedZone, err := r.client.UpdateZone(zone)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating security zone",
			"Could not update security zone, unexpected error: "+err.Error(),
		)
		return
	}

	// Update state with the confirmed values from the API.
	plan.ID = types.Int64Value(int64(updatedZone.ID))
	plan.Name = types.StringValue(updatedZone.Name)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Delete handles the Terraform "destroy" lifecycle event. It:
//  1. Reads the current state to get the zone ID.
//  2. Calls the Orchestrator API to delete the zone.
//  3. Terraform automatically removes the resource from state on success.
func (r *securityZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state securityZoneResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteZone(int(state.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting security zone",
			"Could not delete security zone, unexpected error: "+err.Error(),
		)
		return
	}
}

// ImportState handles the `terraform import arubasdwan_security_zone.example <id>`
// command. It parses the import ID as a numeric zone ID and sets it on the
// state. Terraform then calls Read() to populate the remaining attributes.
//
// Usage: terraform import arubasdwan_security_zone.example 20
func (r *securityZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Parse the import ID as an integer zone ID.
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error importing security zone",
			"Could not parse security zone ID '"+req.ID+"': "+err.Error(),
		)
		return
	}

	// Set the ID attribute on the state. Terraform will then call Read() to
	// fetch the zone name and other attributes from the Orchestrator.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
