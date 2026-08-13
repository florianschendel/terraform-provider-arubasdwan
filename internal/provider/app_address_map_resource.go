package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &appAddressMapResource{}
	_ resource.ResourceWithImportState = &appAddressMapResource{}
	_ resource.ResourceWithModifyPlan  = &appAddressMapResource{}
)

// appAddressMapResourceModel maps the resource schema data.
type appAddressMapResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	IPStart     types.String `tfsdk:"ip_start"`
	IPEnd       types.String `tfsdk:"ip_end"`
	Description types.String `tfsdk:"description"`
	Country     types.String `tfsdk:"country"`
	CountryCode types.String `tfsdk:"country_code"`
	Org         types.String `tfsdk:"org"`
	Priority    types.Int64  `tfsdk:"priority"`
	ServiceID   types.Int64  `tfsdk:"service_id"`
}

type appAddressMapResource struct {
	client *client.Client
}

func NewAppAddressMapResource() resource.Resource {
	return &appAddressMapResource{}
}

func (r *appAddressMapResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_address_map"
}

func (r *appAddressMapResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an address map in the Aruba SD-WAN Orchestrator: an IPv4 address range classified " +
			"as a named application, so security policies and overlay ACLs can match traffic to that range by " +
			"name. The Orchestrator API calls these entries IP intelligence classifications " +
			"(/gms/rest/applicationDefinition/ipIntelligenceClassification).\n\n" +
			"The address range is the identifier: creating a resource for a range that already exists would " +
			"overwrite that definition, so it is rejected — import the existing entry instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Identifier of the entry: the address for a single-address range, otherwise " +
					"\"<ip_start>-<ip_end>\".",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the application. Letters, digits, hyphens, underscores, and periods only; " +
					"at most 31 characters. Policies and overlay ACLs reference applications by this name.",
				Required: true,
			},
			"ip_start": schema.StringAttribute{
				Description: "First IPv4 address of the range, in dotted notation (e.g. \"10.0.13.72\"). " +
					"Changing the range replaces the resource.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ip_end": schema.StringAttribute{
				Description: "Last IPv4 address of the range, in dotted notation. Set it to the same value as " +
					"ip_start for a single host. Changing the range replaces the resource.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Description: "Description of the entry. Must not contain pipe characters or line breaks. Defaults to \"\".",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"country": schema.StringAttribute{
				Description: "Country name associated with the range. Defaults to \"\".",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"country_code": schema.StringAttribute{
				Description: "Two-letter ISO 3166-1 alpha-2 country code (e.g. \"DE\"). Defaults to \"\".",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"org": schema.StringAttribute{
				Description: "Organization associated with the range. Defaults to \"\".",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"priority": schema.Int64Attribute{
				Description: "Classification priority; a higher value takes precedence over a lower one. Defaults to 100.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(100),
			},
			"service_id": schema.Int64Attribute{
				Description: "Service ID assigned by the Orchestrator.",
				Computed:    true,
			},
		},
	}
}

func (r *appAddressMapResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// addressMapRangeExistsError builds the diagnostic for a collision with the
// entry that already covers the range.
func addressMapRangeExistsError(existing *client.AddressMap) (string, string) {
	rangeKey := client.AddressMapRangeKey(existing.IPStart, existing.IPEnd)
	return "Address map for this range already exists", fmt.Sprintf(
		"The Orchestrator already has an address map for %s (named %q). Creating this resource would "+
			"silently overwrite that definition.\n\n"+
			"If Terraform should manage the existing entry, import it:\n\n"+
			"  terraform import arubasdwan_app_address_map.<name> %s",
		rangeKey, existing.Name, rangeKey,
	)
}

// addressMapOverlapError builds the diagnostic for a range that overlaps a
// different entry without matching it exactly.
func addressMapOverlapError(planStart, planEnd string, other *client.AddressMap) (string, string) {
	return "Address map overlaps another entry", fmt.Sprintf(
		"The range %s overlaps the address map %q (%s). The Orchestrator classifies an address by the "+
			"entry covering it, so overlapping ranges leave it ambiguous which application name traffic "+
			"resolves to.\n\n"+
			"Adjust this range so it does not overlap, or remove the other entry.",
		client.AddressMapRangeKey(planStart, planEnd), other.Name,
		client.AddressMapRangeKey(other.IPStart, other.IPEnd),
	)
}

// addressMapDuplicateNameError builds the diagnostic for a name collision
// with an entry covering a different range.
func addressMapDuplicateNameError(dup *client.AddressMap, renaming bool) (string, string) {
	rangeKey := client.AddressMapRangeKey(dup.IPStart, dup.IPEnd)
	summary := "Address map name already in use"
	if renaming {
		return summary, fmt.Sprintf(
			"Another address map named %q already exists on the Orchestrator (range %s). Renaming this one "+
				"to the same name would create a duplicate. Choose a different name.",
			dup.Name, rangeKey,
		)
	}
	return summary, fmt.Sprintf(
		"An address map named %q already exists on the Orchestrator (range %s). The Orchestrator does not "+
			"enforce unique names, so applying would create a duplicate definition — and overlay ACLs and "+
			"policies reference applications by name, which makes duplicates ambiguous. Choose a different "+
			"name, or import the existing entry:\n\n"+
			"  terraform import arubasdwan_app_address_map.<name> %s",
		dup.Name, rangeKey, rangeKey,
	)
}

// validateRange reports configuration errors the API would reject anyway.
func validateRange(plan appAddressMapResourceModel, diags interface {
	AddAttributeError(path.Path, string, string)
}) bool {
	start, err := client.ParseIPv4(plan.IPStart.ValueString())
	if err != nil {
		diags.AddAttributeError(path.Root("ip_start"), "Invalid IPv4 address", err.Error())
		return false
	}
	end, err := client.ParseIPv4(plan.IPEnd.ValueString())
	if err != nil {
		diags.AddAttributeError(path.Root("ip_end"), "Invalid IPv4 address", err.Error())
		return false
	}
	if end < start {
		diags.AddAttributeError(path.Root("ip_end"), "Range ends before it starts",
			fmt.Sprintf("ip_end %s is lower than ip_start %s. Use the same value for both to classify a single host.",
				plan.IPEnd.ValueString(), plan.IPStart.ValueString()))
		return false
	}
	return true
}

// ModifyPlan runs the range and duplicate-name checks already at plan time,
// so a plan does not promise a create the apply would refuse. Range changes
// replace the resource, so the create leg covers them as well. When the
// listing fails, planning proceeds and the apply-time guard decides.
func (r *appAddressMapResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return
	}
	var plan appAddressMapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Name.IsUnknown() || plan.Name.IsNull() ||
		plan.IPStart.IsUnknown() || plan.IPStart.IsNull() ||
		plan.IPEnd.IsUnknown() || plan.IPEnd.IsNull() {
		return
	}
	if !validateRange(plan, &resp.Diagnostics) {
		return
	}

	renaming := false
	checkRange := true
	planKey := client.AddressMapRangeKey(plan.IPStart.ValueString(), plan.IPEnd.ValueString())
	ignoreRanges := []string{planKey}
	if !req.State.Raw.IsNull() {
		var state appAddressMapResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		stateKey := client.AddressMapRangeKey(state.IPStart.ValueString(), state.IPEnd.ValueString())
		if planKey == stateKey && plan.Name.ValueString() == state.Name.ValueString() {
			return
		}
		renaming = true
		// The state's own entry never counts as a collision.
		checkRange = planKey != stateKey
		ignoreRanges = append(ignoreRanges, stateKey)
	}

	existing, err := r.client.GetAddressMaps()
	if err != nil {
		resp.Diagnostics.AddWarning(
			"Could not check address maps during planning",
			"Listing address maps failed: "+err.Error()+
				"\n\nThe checks run again during apply.",
		)
		return
	}

	if checkRange {
		for i := range existing {
			if client.AddressMapRangeKey(existing[i].IPStart, existing[i].IPEnd) == planKey {
				summary, detail := addressMapRangeExistsError(&existing[i])
				resp.Diagnostics.AddAttributeError(path.Root("ip_start"), summary, detail)
				return
			}
		}
	}
	// A partial overlap cannot be resolved by importing, so it is reported
	// separately from an exact match. The resource's own range is excluded.
	if other := client.FindOverlappingAddressMap(existing,
		plan.IPStart.ValueString(), plan.IPEnd.ValueString(), ignoreRanges...); other != nil {
		summary, detail := addressMapOverlapError(plan.IPStart.ValueString(), plan.IPEnd.ValueString(), other)
		resp.Diagnostics.AddAttributeError(path.Root("ip_start"), summary, detail)
		return
	}
	if dup := client.FindAddressMapByName(existing, plan.Name.ValueString(), ignoreRanges...); dup != nil {
		summary, detail := addressMapDuplicateNameError(dup, renaming)
		resp.Diagnostics.AddAttributeError(path.Root("name"), summary, detail)
	}
}

// Create creates the resource and sets the initial Terraform state.
//
// The Orchestrator keys address maps by their range and treats the create
// call as an upsert: posting an existing range would silently overwrite its
// definition. Names are not enforced to be unique either, and overlay ACLs
// and policies reference applications by name. Both collisions are therefore
// rejected.
func (r *appAddressMapResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appAddressMapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validateRange(plan, &resp.Diagnostics) {
		return
	}

	rangeKey := client.AddressMapRangeKey(plan.IPStart.ValueString(), plan.IPEnd.ValueString())

	existing, err := r.client.GetAddressMaps()
	if err != nil {
		resp.Diagnostics.AddError("Error checking existing address maps", "Could not list address maps: "+err.Error())
		return
	}
	for i := range existing {
		if client.AddressMapRangeKey(existing[i].IPStart, existing[i].IPEnd) == rangeKey {
			summary, detail := addressMapRangeExistsError(&existing[i])
			resp.Diagnostics.AddError(summary, detail)
			return
		}
	}
	if other := client.FindOverlappingAddressMap(existing,
		plan.IPStart.ValueString(), plan.IPEnd.ValueString(), rangeKey); other != nil {
		summary, detail := addressMapOverlapError(plan.IPStart.ValueString(), plan.IPEnd.ValueString(), other)
		resp.Diagnostics.AddError(summary, detail)
		return
	}
	if dup := client.FindAddressMapByName(existing, plan.Name.ValueString(), rangeKey); dup != nil {
		summary, detail := addressMapDuplicateNameError(dup, false)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	def := client.AddressMap{
		Name:        plan.Name.ValueString(),
		IPStart:     plan.IPStart.ValueString(),
		IPEnd:       plan.IPEnd.ValueString(),
		Description: plan.Description.ValueString(),
		Country:     plan.Country.ValueString(),
		CountryCode: plan.CountryCode.ValueString(),
		Org:         plan.Org.ValueString(),
		Priority:    int(plan.Priority.ValueInt64()),
	}
	if err := r.client.CreateAddressMap(def); err != nil {
		resp.Diagnostics.AddError("Error creating address map", "Could not create address map: "+err.Error())
		return
	}

	created, err := r.client.GetAddressMap(def.IPStart, def.IPEnd)
	if err != nil {
		resp.Diagnostics.AddError("Address map created but could not be read back", err.Error())
		return
	}
	if created == nil {
		resp.Diagnostics.AddError("Address map created but not found",
			fmt.Sprintf("The Orchestrator does not report an address map for %s after creating it.", rangeKey))
		return
	}

	applyAddressMapToModel(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *appAddressMapResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appAddressMapResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.client.GetAddressMap(state.IPStart.ValueString(), state.IPEnd.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading address map", "Could not read address map: "+err.Error())
		return
	}
	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyAddressMapToModel(&state, found)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *appAddressMapResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan appAddressMapResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validateRange(plan, &resp.Diagnostics) {
		return
	}

	rangeKey := client.AddressMapRangeKey(plan.IPStart.ValueString(), plan.IPEnd.ValueString())

	// A rename must not collide with another entry's name.
	existing, err := r.client.GetAddressMaps()
	if err != nil {
		resp.Diagnostics.AddError("Error checking existing address maps", "Could not list address maps: "+err.Error())
		return
	}
	if other := client.FindOverlappingAddressMap(existing,
		plan.IPStart.ValueString(), plan.IPEnd.ValueString(), rangeKey); other != nil {
		summary, detail := addressMapOverlapError(plan.IPStart.ValueString(), plan.IPEnd.ValueString(), other)
		resp.Diagnostics.AddError(summary, detail)
		return
	}
	if dup := client.FindAddressMapByName(existing, plan.Name.ValueString(), rangeKey); dup != nil {
		summary, detail := addressMapDuplicateNameError(dup, true)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	def := client.AddressMap{
		Name:        plan.Name.ValueString(),
		IPStart:     plan.IPStart.ValueString(),
		IPEnd:       plan.IPEnd.ValueString(),
		Description: plan.Description.ValueString(),
		Country:     plan.Country.ValueString(),
		CountryCode: plan.CountryCode.ValueString(),
		Org:         plan.Org.ValueString(),
		Priority:    int(plan.Priority.ValueInt64()),
	}
	if err := r.client.UpdateAddressMap(def); err != nil {
		resp.Diagnostics.AddError("Error updating address map", "Could not update address map: "+err.Error())
		return
	}

	updated, err := r.client.GetAddressMap(def.IPStart, def.IPEnd)
	if err != nil {
		resp.Diagnostics.AddError("Address map updated but could not be read back", err.Error())
		return
	}
	if updated == nil {
		resp.Diagnostics.AddError("Address map updated but not found",
			fmt.Sprintf("The Orchestrator does not report an address map for %s after updating it.", rangeKey))
		return
	}

	applyAddressMapToModel(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *appAddressMapResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state appAddressMapResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteAddressMap(state.IPStart.ValueString(), state.IPEnd.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			"Error deleting address map",
			"Could not delete address map: "+err.Error()+
				"\n\nThe Orchestrator refuses to delete an entry that is still referenced by an "+
				"application group; remove the reference first.",
		)
		return
	}
}

// ImportState imports an entry by its range: a single address, or
// "<ip_start>-<ip_end>" for a wider range.
func (r *appAddressMapResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ipStart, ipEnd, found := strings.Cut(req.ID, "-")
	if !found {
		ipEnd = ipStart
	}
	ipStart, ipEnd = strings.TrimSpace(ipStart), strings.TrimSpace(ipEnd)

	if _, err := client.ParseIPv4(ipStart); err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf(
			"Import an address map by its range: either a single address such as \"10.0.13.72\", "+
				"or \"<ip_start>-<ip_end>\" such as \"10.0.1.0-10.0.1.255\". %s", err))
		return
	}
	if _, err := client.ParseIPv4(ipEnd); err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf(
			"The end of the range is not a valid IPv4 address. %s", err))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ip_start"), ipStart)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ip_end"), ipEnd)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), client.AddressMapRangeKey(ipStart, ipEnd))...)
}

// applyAddressMapToModel copies the Orchestrator's view into the model.
func applyAddressMapToModel(model *appAddressMapResourceModel, def *client.AddressMap) {
	model.ID = types.StringValue(client.AddressMapRangeKey(def.IPStart, def.IPEnd))
	model.Name = types.StringValue(def.Name)
	model.IPStart = types.StringValue(def.IPStart)
	model.IPEnd = types.StringValue(def.IPEnd)
	model.Description = types.StringValue(def.Description)
	model.Country = types.StringValue(def.Country)
	model.CountryCode = types.StringValue(def.CountryCode)
	model.Org = types.StringValue(def.Org)
	model.Priority = types.Int64Value(int64(def.Priority))
	model.ServiceID = types.Int64Value(int64(def.ServiceID))
}
