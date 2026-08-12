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
	_ resource.ResourceWithModifyPlan  = &appPortProtocolResource{}
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
// findPortProtocolByName returns the first classification whose name matches
// (compared case-insensitively) on a port/protocol other than the ignored
// pairs; nil when there is none.
func findPortProtocolByName(defs []client.PortProtocolClassification, name string, ignorePairs ...[2]int) *client.PortProtocolClassification {
	for i := range defs {
		ignored := false
		for _, pair := range ignorePairs {
			if defs[i].Port == pair[0] && defs[i].Protocol == pair[1] {
				ignored = true
				break
			}
		}
		if !ignored && strings.EqualFold(defs[i].Name, name) {
			return &defs[i]
		}
	}
	return nil
}

// portProtocolPairExistsError builds the diagnostic for a collision with the
// classification that already covers the port/protocol pair.
func portProtocolPairExistsError(existing *client.PortProtocolClassification) (string, string) {
	return "Port/protocol classification for this pair already exists", fmt.Sprintf(
		"The Orchestrator already has a classification for port %d and protocol %d "+
			"(named %q). Creating this resource would silently overwrite that definition.\n\n"+
			"If Terraform should manage the existing definition, import it:\n\n"+
			"  terraform import arubasdwan_app_port_protocol.<name> %s",
		existing.Port, existing.Protocol, existing.Name, appDefID(existing.Port, existing.Protocol),
	)
}

// portProtocolDuplicateNameError builds the diagnostic for a name collision
// with an existing classification on another port/protocol pair.
func portProtocolDuplicateNameError(dup *client.PortProtocolClassification, renaming bool) (string, string) {
	summary := "Port/protocol classification name already in use"
	if renaming {
		return summary, fmt.Sprintf(
			"Another port/protocol classification named %q already exists on the Orchestrator "+
				"(port %d, protocol %d). Renaming this one to the same name would create a duplicate. "+
				"Choose a different name.",
			dup.Name, dup.Port, dup.Protocol,
		)
	}
	return summary, fmt.Sprintf(
		"A port/protocol classification named %q already exists on the Orchestrator "+
			"(port %d, protocol %d). The Orchestrator does not enforce unique names, so applying "+
			"would create a duplicate definition — and overlay ACLs and policies reference "+
			"applications by name, which makes duplicates ambiguous. Choose a different name, or "+
			"import the existing definition:\n\n"+
			"  terraform import arubasdwan_app_port_protocol.<name> %s",
		dup.Name, dup.Port, dup.Protocol, appDefID(dup.Port, dup.Protocol),
	)
}

// ModifyPlan runs the pair and duplicate-name checks already at plan time,
// so a plan does not promise a create or rename that the apply would refuse.
// Only plans that create the resource or change its name, port, or protocol
// call the API; when the listing fails, planning proceeds and the apply-time
// guard decides.
func (r *appPortProtocolResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return
	}
	var plan appPortProtocolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Port.IsUnknown() || plan.Port.IsNull() || plan.Protocol.IsUnknown() || plan.Protocol.IsNull() ||
		plan.Name.IsUnknown() || plan.Name.IsNull() {
		return
	}
	port := int(plan.Port.ValueInt64())
	protocol := int(plan.Protocol.ValueInt64())

	renaming := false
	checkPair := true
	ignorePairs := [][2]int{{port, protocol}}
	if !req.State.Raw.IsNull() {
		var state appPortProtocolResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		statePort := int(state.Port.ValueInt64())
		stateProtocol := int(state.Protocol.ValueInt64())
		if port == statePort && protocol == stateProtocol && plan.Name.ValueString() == state.Name.ValueString() {
			return
		}
		renaming = true
		// The state's own entry never counts as a collision.
		checkPair = port != statePort || protocol != stateProtocol
		ignorePairs = append(ignorePairs, [2]int{statePort, stateProtocol})
	}

	existing, err := r.client.GetPortProtocolClassifications()
	if err != nil {
		resp.Diagnostics.AddWarning(
			"Could not check port/protocol classification names during planning",
			"Listing port/protocol classifications failed: "+err.Error()+
				"\n\nThe duplicate check runs again during apply.",
		)
		return
	}
	if checkPair {
		for i := range existing {
			if existing[i].Port == port && existing[i].Protocol == protocol {
				summary, detail := portProtocolPairExistsError(&existing[i])
				resp.Diagnostics.AddAttributeError(path.Root("port"), summary, detail)
				return
			}
		}
	}
	if dup := findPortProtocolByName(existing, plan.Name.ValueString(), ignorePairs...); dup != nil {
		summary, detail := portProtocolDuplicateNameError(dup, renaming)
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
	}
}

// Create creates the resource and sets the initial Terraform state.
//
// The Orchestrator keys port/protocol classifications by the port and
// protocol pair and treats the create call as an upsert: posting an existing
// pair would silently overwrite its definition. Names are not enforced to be
// unique either, and overlay ACLs and policies reference applications by
// name. Both collisions are therefore rejected.
func (r *appPortProtocolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appPortProtocolResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	existing, err := r.client.GetPortProtocolClassifications()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error checking existing port/protocol classifications",
			"Could not list port/protocol classifications: "+err.Error(),
		)
		return
	}
	port := int(plan.Port.ValueInt64())
	protocol := int(plan.Protocol.ValueInt64())
	for i := range existing {
		if existing[i].Port == port && existing[i].Protocol == protocol {
			summary, detail := portProtocolPairExistsError(&existing[i])
			resp.Diagnostics.AddAttributeError(path.Root("port"), summary, detail)
			return
		}
	}
	if dup := findPortProtocolByName(existing, plan.Name.ValueString(), [2]int{port, protocol}); dup != nil {
		summary, detail := portProtocolDuplicateNameError(dup, false)
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
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

	err = r.client.CreatePortProtocolClassification(def)
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

	// Renaming must not collide with another definition either.
	existing, err := r.client.GetPortProtocolClassifications()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error checking existing port/protocol classifications",
			"Could not list port/protocol classifications: "+err.Error(),
		)
		return
	}
	if dup := findPortProtocolByName(existing, plan.Name.ValueString(), [2]int{int(plan.Port.ValueInt64()), int(plan.Protocol.ValueInt64())}); dup != nil {
		summary, detail := portProtocolDuplicateNameError(dup, true)
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
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

	err = r.client.UpdatePortProtocolClassification(def)
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
