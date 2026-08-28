package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &appCompoundClassificationResource{}
	_ resource.ResourceWithImportState = &appCompoundClassificationResource{}
	_ resource.ResourceWithModifyPlan  = &appCompoundClassificationResource{}
)

// appCompoundClassificationResourceModel maps the resource schema data.
type appCompoundClassificationResourceModel struct {
	ID            types.String `tfsdk:"id"`
	RuleID        types.Int64  `tfsdk:"rule_id"`
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
				Description: "The name of the application, which identifies the definition.",
				Computed:    true,
			},
			"rule_id": schema.Int64Attribute{
				Description: "The numeric ID the Orchestrator currently assigns to this rule. It doubles " +
					"as the rule's position in the classification priority order, so deleting another " +
					"compound classification shifts it. Do not use it as a reference — it is reported " +
					"for diagnostics only and can change without this definition being touched.",
				Computed: true,
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
		ID:            types.StringValue(def.Name),
		RuleID:        types.Int64Value(int64(def.ID)),
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

// findCompoundByName returns the first classification whose name matches
// (compared case-insensitively), skipping entries named ignoreName; nil when
// there is none. Pass an empty ignoreName to consider every entry.
//
// The entry to skip is identified by name rather than by ID because IDs shift
// whenever another classification is deleted. A resource updating itself
// passes the name it holds in state, so its own entry does not count as a
// collision with itself.
func findCompoundByName(defs []client.CompoundClassification, name, ignoreName string) *client.CompoundClassification {
	for i := range defs {
		if ignoreName != "" && strings.EqualFold(defs[i].Name, ignoreName) {
			continue
		}
		if strings.EqualFold(defs[i].Name, name) {
			return &defs[i]
		}
	}
	return nil
}

// compoundDuplicateError builds the diagnostic for a name collision with an
// existing classification.
func compoundDuplicateError(dup *client.CompoundClassification, renaming bool) (string, string) {
	summary := "Compound classification name already in use"
	if renaming {
		return summary, fmt.Sprintf(
			"Another user-defined compound classification named %q already exists on the "+
				"Orchestrator. Renaming this one to the same name would create a duplicate. "+
				"Choose a different name.",
			dup.Name,
		)
	}
	return summary, fmt.Sprintf(
		"A user-defined compound classification named %q already exists on the Orchestrator. "+
			"The Orchestrator does not enforce unique names, so applying would create a "+
			"duplicate definition — and overlay ACLs and policies reference applications by name, "+
			"which makes duplicates ambiguous.\n\n"+
			"If Terraform should manage the existing definition, import it:\n\n"+
			"  terraform import arubasdwan_app_compound_classification.<name> %s\n\n"+
			"To define a separate application, choose a different name.",
		dup.Name, dup.Name,
	)
}

// compoundVanishedError builds the diagnostic for an entry that is no longer
// on the Orchestrator when an update is applied.
func compoundVanishedError(name string) (string, string) {
	return "Compound classification no longer exists", fmt.Sprintf(
		"No user-defined compound classification named %q exists on the Orchestrator, so there is "+
			"nothing to update. It was most likely deleted outside Terraform between the plan and "+
			"this apply.\n\nRun terraform plan again: the refresh notices the entry is gone and "+
			"plans to recreate it.",
		name,
	)
}

// ModifyPlan runs the duplicate-name check already at plan time, so a plan
// does not promise a create or rename that the apply would refuse. Only
// plans that create the resource or change its name call the API; when the
// listing fails, planning proceeds and the apply-time guard decides.
func (r *appCompoundClassificationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return
	}
	var plan appCompoundClassificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Name.IsUnknown() || plan.Name.IsNull() {
		return
	}

	renaming := false
	ignoreName := ""
	if !req.State.Raw.IsNull() {
		var state appCompoundClassificationResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if plan.Name.ValueString() == state.Name.ValueString() {
			return
		}
		renaming = true
		ignoreName = state.Name.ValueString()
	}

	existing, err := r.client.GetCompoundClassifications()
	if err != nil {
		resp.Diagnostics.AddWarning(
			"Could not check compound classification names during planning",
			"Listing compound classifications failed: "+err.Error()+
				"\n\nThe duplicate check runs again during apply.",
		)
		return
	}
	if dup := findCompoundByName(existing, plan.Name.ValueString(), ignoreName); dup != nil {
		summary, detail := compoundDuplicateError(dup, renaming)
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
	}
}

// Create creates the resource and sets the initial Terraform state.
//
// The Orchestrator does not enforce unique application names, so creating a
// definition whose name is already taken would silently add a duplicate —
// and overlay ACLs and policies reference applications by name, which makes
// duplicates ambiguous. Such a create is therefore rejected.
func (r *appCompoundClassificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appCompoundClassificationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	existing, err := r.client.GetCompoundClassifications()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error checking existing compound classifications",
			"Could not list compound classifications: "+err.Error(),
		)
		return
	}
	if dup := findCompoundByName(existing, plan.Name.ValueString(), ""); dup != nil {
		summary, detail := compoundDuplicateError(dup, false)
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
		return
	}

	def := compoundModelFromPlan(plan)

	err = r.client.CreateCompoundClassification(&def)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating compound classification",
			"Could not create compound classification, unexpected error: "+err.Error(),
		)
		return
	}

	// Read the entry back to record the ID the Orchestrator actually assigned
	// rather than the one requested.
	created, err := r.client.GetCompoundClassificationByName(def.Name)
	if err != nil {
		resp.Diagnostics.AddError(
			"Compound classification created but could not be read back",
			"Could not list compound classifications: "+err.Error(),
		)
		return
	}
	if created == nil {
		resp.Diagnostics.AddError(
			"Compound classification created but not found",
			fmt.Sprintf("The Orchestrator does not report a compound classification named %q after creating it.", def.Name),
		)
		return
	}

	plan.ID = types.StringValue(created.Name)
	plan.RuleID = types.Int64Value(int64(created.ID))

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

	// Look the entry up by name, not by the numeric ID held in state: IDs are
	// positions in the priority order and shift when another classification is
	// deleted, so a stored ID can address a different rule entirely.
	def, err := r.client.GetCompoundClassificationByName(state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading compound classification",
			"Could not read compound classification "+state.Name.ValueString()+": "+err.Error(),
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

	var state appCompoundClassificationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	existing, err := r.client.GetCompoundClassifications()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error checking existing compound classifications",
			"Could not list compound classifications: "+err.Error(),
		)
		return
	}

	// Renaming must not collide with another definition either.
	if dup := findCompoundByName(existing, plan.Name.ValueString(), state.Name.ValueString()); dup != nil {
		summary, detail := compoundDuplicateError(dup, true)
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
		return
	}

	// The current ID is resolved from the name in state and written in one
	// serialized step inside the client: several resources of one apply run
	// their writes in parallel, and every delete renumbers the IDs the other
	// operations are about to address. An ID resolved here, outside that
	// step, could already belong to a different rule when the write lands.
	def := compoundModelFromPlan(plan)
	ruleID, found, err := r.client.UpdateCompoundClassificationByName(state.Name.ValueString(), def)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating compound classification",
			"Could not update compound classification, unexpected error: "+err.Error(),
		)
		return
	}
	if !found {
		summary, detail := compoundVanishedError(state.Name.ValueString())
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	plan.ID = types.StringValue(plan.Name.ValueString())
	plan.RuleID = types.Int64Value(int64(ruleID))

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

	// The current ID is resolved from the name and deleted in one serialized
	// step inside the client. Destroying several compound classifications in
	// one apply issues the deletes in parallel, and each delete renumbers the
	// IDs the remaining ones are about to address — resolving here, outside
	// that step, would let all deletes resolve before the first one shifts
	// the list, sending the later ones into wrong slots. found=false means
	// the entry is already gone, which is the desired end state.
	if _, err := r.client.DeleteCompoundClassificationByName(state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			"Error deleting compound classification",
			"Could not delete compound classification, unexpected error: "+err.Error(),
		)
		return
	}
}

// ImportState imports a resource by its application name. The numeric rule ID
// is deliberately not accepted: it encodes the rule's position in the priority
// order and changes whenever another classification is deleted, so an import
// by ID would bind the resource to whatever occupies that slot at the time.
func (r *appCompoundClassificationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name := strings.TrimSpace(req.ID)
	if name == "" {
		resp.Diagnostics.AddError(
			"Error importing compound classification",
			"Expected the application name as the import ID, got an empty string.",
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}
