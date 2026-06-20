package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &appPortProtocolResource{}
	_ resource.ResourceWithImportState = &appPortProtocolResource{}
)

// appPortProtocolResourceModel maps the resource schema data.
type appPortProtocolResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Port        types.Int64  `tfsdk:"port"`
	Protocol    types.Int64  `tfsdk:"protocol"`
	Description types.String `tfsdk:"description"`
	Priority    types.Int64  `tfsdk:"confidence"`
	Disabled    types.Bool   `tfsdk:"disabled"`
}

// appPortProtocolResource is the resource implementation.
type appPortProtocolResource struct {
	client *client.Client
}

// NewAppPortProtocolResource returns a new resource instance.
func NewAppPortProtocolResource() resource.Resource {
	return &appPortProtocolResource{}
}

// Metadata returns the resource type name.
func (r *appPortProtocolResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_port_protocol"
}

// Schema defines the schema for the resource.
func (r *appPortProtocolResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a port/protocol application classification in the Aruba SD-WAN Orchestrator. " +
			"Uses the /gms/rest/applicationDefinition/portProtocolClassification API endpoints.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Composite identifier: port_protocol (e.g. \"8443_6\").",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the port/protocol classification.",
				Required:    true,
			},
			"port": schema.Int64Attribute{
				Description: "The port number for this port/protocol classification.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"protocol": schema.Int64Attribute{
				Description: "The protocol number (e.g. 6 for TCP, 17 for UDP).",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Description: "A description for the port/protocol classification. Defaults to \"\".",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"confidence": schema.Int64Attribute{
				Description: "The confidence level of the application classification (0-100). Defaults to 50.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(50),
			},
			"disabled": schema.BoolAttribute{
				Description: "Whether the port/protocol classification is disabled. Defaults to false.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *appPortProtocolResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// appDefID builds the composite ID from port and protocol.
func appDefID(port, protocol int) string {
	return fmt.Sprintf("%d_%d", port, protocol)
}

// parseAppDefID parses a composite ID "port_protocol" into port and protocol integers.
func parseAppDefID(id string) (port, protocol int, err error) {
	parts := strings.SplitN(id, "_", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid port/protocol classification ID %q: expected port_protocol format", id)
	}
	port, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port in ID %q: %w", id, err)
	}
	protocol, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid protocol in ID %q: %w", id, err)
	}
	return port, protocol, nil
}

// Create creates the resource and sets the initial Terraform state.
func (r *appPortProtocolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appPortProtocolResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	def := client.PortProtocolClassification{
		Name:        plan.Name.ValueString(),
		Port:        int(plan.Port.ValueInt64()),
		Protocol:    int(plan.Protocol.ValueInt64()),
		Description: plan.Description.ValueString(),
		Priority:    int(plan.Priority.ValueInt64()),
		Disabled:    plan.Disabled.ValueBool(),
	}

	err := r.client.CreatePortProtocolClassification(def)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating port/protocol classification",
			"Could not create port/protocol classification, unexpected error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(appDefID(def.Port, def.Protocol))

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Read refreshes the Terraform state with the latest data.
func (r *appPortProtocolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appPortProtocolResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	port, protocol, err := parseAppDefID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error parsing port/protocol classification ID",
			err.Error(),
		)
		return
	}

	def, err := r.client.GetPortProtocolClassification(port, protocol)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading port/protocol classification",
			"Could not read port/protocol classification "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	// If the definition no longer exists on the Orchestrator, remove it from state.
	if def == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(appDefID(def.Port, def.Protocol))
	state.Name = types.StringValue(def.Name)
	state.Port = types.Int64Value(int64(def.Port))
	state.Protocol = types.Int64Value(int64(def.Protocol))
	state.Description = types.StringValue(def.Description)
	state.Priority = types.Int64Value(int64(def.Priority))
	state.Disabled = types.BoolValue(def.Disabled)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *appPortProtocolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan appPortProtocolResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	def := client.PortProtocolClassification{
		Name:        plan.Name.ValueString(),
		Port:        int(plan.Port.ValueInt64()),
		Protocol:    int(plan.Protocol.ValueInt64()),
		Description: plan.Description.ValueString(),
		Priority:    int(plan.Priority.ValueInt64()),
		Disabled:    plan.Disabled.ValueBool(),
	}

	err := r.client.UpdatePortProtocolClassification(def)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating port/protocol classification",
			"Could not update port/protocol classification, unexpected error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(appDefID(def.Port, def.Protocol))

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *appPortProtocolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state appPortProtocolResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	port, protocol, err := parseAppDefID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error parsing port/protocol classification ID",
			err.Error(),
		)
		return
	}

	err = r.client.DeletePortProtocolClassification(port, protocol)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting port/protocol classification",
			"Could not delete port/protocol classification, unexpected error: "+err.Error(),
		)
		return
	}
}

// ImportState imports a resource by its "port_protocol" ID (e.g. "8443_6").
func (r *appPortProtocolResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	_, _, err := parseAppDefID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error importing port/protocol classification",
			fmt.Sprintf("Invalid import ID %q. Expected format: port_protocol (e.g. 8443_6)", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
