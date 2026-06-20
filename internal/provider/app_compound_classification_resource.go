package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &appCompoundClassificationResource{}
	_ resource.ResourceWithImportState = &appCompoundClassificationResource{}
)

// appCompoundClassificationResourceModel maps the resource schema data.
type appCompoundClassificationResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Confidence    types.Int64  `tfsdk:"confidence"`
	Disabled      types.Bool   `tfsdk:"disabled"`
	Protocol      types.String `tfsdk:"protocol"`
	SrcIP         types.String `tfsdk:"src_ip"`
	DstIP         types.String `tfsdk:"dst_ip"`
	EitherIP      types.String `tfsdk:"either_ip"`
	SrcPort       types.String `tfsdk:"src_port"`
	DstPort       types.String `tfsdk:"dst_port"`
	EitherPort    types.String `tfsdk:"either_port"`
	SrcDNS        types.String `tfsdk:"src_dns"`
	DstDNS        types.String `tfsdk:"dst_dns"`
	EitherDNS     types.String `tfsdk:"either_dns"`
	SrcGeo        types.String `tfsdk:"src_geo"`
	DstGeo        types.String `tfsdk:"dst_geo"`
	EitherGeo     types.String `tfsdk:"either_geo"`
	SrcService    types.String `tfsdk:"src_service"`
	DstService    types.String `tfsdk:"dst_service"`
	EitherService types.String `tfsdk:"either_service"`
	DSCP          types.String `tfsdk:"dscp"`
	VLAN          types.String `tfsdk:"vlan"`
}

// appCompoundClassificationResource is the resource implementation.
type appCompoundClassificationResource struct {
	client *client.Client
}

// NewAppCompoundClassificationResource returns a new resource instance.
func NewAppCompoundClassificationResource() resource.Resource {
	return &appCompoundClassificationResource{}
}

// Metadata returns the resource type name.
func (r *appCompoundClassificationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_compound_classification"
}

// compoundMatchField is a helper to define an optional string attribute with default "".
func compoundMatchField(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Description: description,
		Optional:    true,
		Computed:    true,
		Default:     stringdefault.StaticString(""),
	}
}

// Schema defines the schema for the resource.
func (r *appCompoundClassificationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a compound match-based application definition in the Aruba SD-WAN Orchestrator. " +
			"Uses the /gms/rest/applicationDefinition/compoundClassification API endpoints.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The numeric ID assigned by the Orchestrator (as a string).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the application.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "A description for the application. Defaults to \"\".",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"confidence": schema.Int64Attribute{
				Description: "The confidence level (0-100). Defaults to 100.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(100),
			},
			"disabled": schema.BoolAttribute{
				Description: "Whether the application is disabled. Defaults to false.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"protocol":       compoundMatchField("Protocol match (e.g. \"tcp\", \"udp\")."),
			"src_ip":         compoundMatchField("Source IP match (e.g. \"10.0.0.0/8\")."),
			"dst_ip":         compoundMatchField("Destination IP match."),
			"either_ip":      compoundMatchField("Either direction IP match."),
			"src_port":       compoundMatchField("Source port match."),
			"dst_port":       compoundMatchField("Destination port match."),
			"either_port":    compoundMatchField("Either direction port match."),
			"src_dns":        compoundMatchField("Source DNS match."),
			"dst_dns":        compoundMatchField("Destination DNS match."),
			"either_dns":     compoundMatchField("Either direction DNS match."),
			"src_geo":        compoundMatchField("Source geolocation match."),
			"dst_geo":        compoundMatchField("Destination geolocation match."),
			"either_geo":     compoundMatchField("Either direction geolocation match."),
			"src_service":    compoundMatchField("Source service match."),
			"dst_service":    compoundMatchField("Destination service match."),
			"either_service": compoundMatchField("Either direction service match."),
			"dscp":           compoundMatchField("DSCP match."),
			"vlan":           compoundMatchField("VLAN match."),
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *appCompoundClassificationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func compoundModelFromPlan(plan appCompoundClassificationResourceModel) client.CompoundClassification {
	return client.CompoundClassification{
		Name:          plan.Name.ValueString(),
		Description:   plan.Description.ValueString(),
		Confidence:    int(plan.Confidence.ValueInt64()),
		Disabled:      plan.Disabled.ValueBool(),
		Protocol:      plan.Protocol.ValueString(),
		SrcIP:         plan.SrcIP.ValueString(),
		DstIP:         plan.DstIP.ValueString(),
		EitherIP:      plan.EitherIP.ValueString(),
		SrcPort:       plan.SrcPort.ValueString(),
		DstPort:       plan.DstPort.ValueString(),
		EitherPort:    plan.EitherPort.ValueString(),
		SrcDNS:        plan.SrcDNS.ValueString(),
		DstDNS:        plan.DstDNS.ValueString(),
		EitherDNS:     plan.EitherDNS.ValueString(),
		SrcGeo:        plan.SrcGeo.ValueString(),
		DstGeo:        plan.DstGeo.ValueString(),
		EitherGeo:     plan.EitherGeo.ValueString(),
		SrcService:    plan.SrcService.ValueString(),
		DstService:    plan.DstService.ValueString(),
		EitherService: plan.EitherService.ValueString(),
		DSCP:          plan.DSCP.ValueString(),
		VLAN:          plan.VLAN.ValueString(),
	}
}

func compoundStateFromDef(def *client.CompoundClassification) appCompoundClassificationResourceModel {
	return appCompoundClassificationResourceModel{
		ID:            types.StringValue(strconv.Itoa(def.ID)),
		Name:          types.StringValue(def.Name),
		Description:   types.StringValue(def.Description),
		Confidence:    types.Int64Value(int64(def.Confidence)),
		Disabled:      types.BoolValue(def.Disabled),
		Protocol:      types.StringValue(def.Protocol),
		SrcIP:         types.StringValue(def.SrcIP),
		DstIP:         types.StringValue(def.DstIP),
		EitherIP:      types.StringValue(def.EitherIP),
		SrcPort:       types.StringValue(def.SrcPort),
		DstPort:       types.StringValue(def.DstPort),
		EitherPort:    types.StringValue(def.EitherPort),
		SrcDNS:        types.StringValue(def.SrcDNS),
		DstDNS:        types.StringValue(def.DstDNS),
		EitherDNS:     types.StringValue(def.EitherDNS),
		SrcGeo:        types.StringValue(def.SrcGeo),
		DstGeo:        types.StringValue(def.DstGeo),
		EitherGeo:     types.StringValue(def.EitherGeo),
		SrcService:    types.StringValue(def.SrcService),
		DstService:    types.StringValue(def.DstService),
		EitherService: types.StringValue(def.EitherService),
		DSCP:          types.StringValue(def.DSCP),
		VLAN:          types.StringValue(def.VLAN),
	}
}

// Create creates the resource and sets the initial Terraform state.
func (r *appCompoundClassificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appCompoundClassificationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	def := compoundModelFromPlan(plan)

	err := r.client.CreateCompoundClassification(&def)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating compound classification",
			"Could not create compound classification, unexpected error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(strconv.Itoa(def.ID))

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Read refreshes the Terraform state with the latest data.
func (r *appCompoundClassificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appCompoundClassificationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error parsing compound classification ID",
			fmt.Sprintf("Invalid ID %q: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	def, err := r.client.GetCompoundClassification(id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading compound classification",
			"Could not read compound classification "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	if def == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	newState := compoundStateFromDef(def)

	diags = resp.State.Set(ctx, &newState)
	resp.Diagnostics.Append(diags...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *appCompoundClassificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan appCompoundClassificationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error parsing compound classification ID",
			fmt.Sprintf("Invalid ID %q: %s", plan.ID.ValueString(), err.Error()),
		)
		return
	}

	def := compoundModelFromPlan(plan)
	def.ID = id

	err = r.client.UpdateCompoundClassification(def)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating compound classification",
			"Could not update compound classification, unexpected error: "+err.Error(),
		)
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *appCompoundClassificationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state appCompoundClassificationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error parsing compound classification ID",
			fmt.Sprintf("Invalid ID %q: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	err = r.client.DeleteCompoundClassification(id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting compound classification",
			"Could not delete compound classification, unexpected error: "+err.Error(),
		)
		return
	}
}

// ImportState imports a resource by its numeric ID (as a string).
func (r *appCompoundClassificationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	_, err := strconv.Atoi(req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error importing compound classification",
			fmt.Sprintf("Invalid import ID %q. Expected a numeric ID.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
