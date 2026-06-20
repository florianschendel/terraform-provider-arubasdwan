package provider

import (
	"context"
	"fmt"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &appDNSClassificationResource{}
	_ resource.ResourceWithImportState = &appDNSClassificationResource{}
)

// appDNSClassificationResourceModel maps the resource schema data.
type appDNSClassificationResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Domain      types.String `tfsdk:"domain"`
	Description types.String `tfsdk:"description"`
	Priority    types.Int64  `tfsdk:"confidence"`
	Disabled    types.Bool   `tfsdk:"disabled"`
}

// appDNSClassificationResource is the resource implementation.
type appDNSClassificationResource struct {
	client *client.Client
}

// NewAppDNSClassificationResource returns a new resource instance.
func NewAppDNSClassificationResource() resource.Resource {
	return &appDNSClassificationResource{}
}

// Metadata returns the resource type name.
func (r *appDNSClassificationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_dns_classification"
}

// Schema defines the schema for the resource.
func (r *appDNSClassificationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DNS domain-based application definition in the Aruba SD-WAN Orchestrator. " +
			"Uses the /gms/rest/applicationDefinition/dnsClassification API endpoints.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The domain used as identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the application.",
				Required:    true,
			},
			"domain": schema.StringAttribute{
				Description: "The DNS domain pattern (e.g. \"*.example.com\"). Used as the unique key.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Description: "A description for the application. Defaults to \"\".",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"confidence": schema.Int64Attribute{
				Description: "The confidence level of the application classification (1-100).",
				Required:    true,
			},
			"disabled": schema.BoolAttribute{
				Description: "Whether the application is disabled. Defaults to false.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *appDNSClassificationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
		return
	}

	r.client = apiClient
}

// Create creates the resource and sets the initial Terraform state.
func (r *appDNSClassificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appDNSClassificationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	def := client.DNSClassification{
		Name:        plan.Name.ValueString(),
		Domain:      plan.Domain.ValueString(),
		Description: plan.Description.ValueString(),
		Priority:    int(plan.Priority.ValueInt64()),
		Disabled:    plan.Disabled.ValueBool(),
	}

	err := r.client.CreateDNSClassification(def)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating DNS classification",
			"Could not create DNS classification, unexpected error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(def.Domain)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Read refreshes the Terraform state with the latest data.
func (r *appDNSClassificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appDNSClassificationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := state.ID.ValueString()

	def, err := r.client.GetDNSClassification(domain)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading DNS classification",
			"Could not read DNS classification "+domain+": "+err.Error(),
		)
		return
	}

	if def == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(def.Domain)
	state.Name = types.StringValue(def.Name)
	state.Domain = types.StringValue(def.Domain)
	state.Description = types.StringValue(def.Description)
	state.Priority = types.Int64Value(int64(def.Priority))
	state.Disabled = types.BoolValue(def.Disabled)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *appDNSClassificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan appDNSClassificationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	def := client.DNSClassification{
		Name:        plan.Name.ValueString(),
		Domain:      plan.Domain.ValueString(),
		Description: plan.Description.ValueString(),
		Priority:    int(plan.Priority.ValueInt64()),
		Disabled:    plan.Disabled.ValueBool(),
	}

	err := r.client.UpdateDNSClassification(def)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating DNS classification",
			"Could not update DNS classification, unexpected error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(def.Domain)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *appDNSClassificationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state appDNSClassificationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain := state.ID.ValueString()

	err := r.client.DeleteDNSClassification(domain)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting DNS classification",
			"Could not delete DNS classification, unexpected error: "+err.Error(),
		)
		return
	}
}

// ImportState imports a resource by its domain.
func (r *appDNSClassificationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
