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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
)

// Compile-time interface checks for the security policy resource.
var (
	_ resource.Resource                   = &securityPolicyResource{}
	_ resource.ResourceWithImportState    = &securityPolicyResource{}
	_ resource.ResourceWithValidateConfig = &securityPolicyResource{}
)

// securityPolicyResourceModel maps the Terraform resource schema for a firewall
// policy rule. A policy rule defines what action (allow/deny) to take for
// traffic matching specific criteria between two security zones.
//
// The resource is uniquely identified by a composite key of:
//   - segment_pair:   The VRF segment pair (e.g. "0_0")
//   - source_zone_id: The source security zone
//   - dest_zone_id:   The destination security zone
//   - priority:       The rule priority within the zone pair
//
// Changing any of these four fields requires replacing the resource (destroying
// and recreating it) because the Orchestrator API uses them as the resource key.
type securityPolicyResourceModel struct {
	ID           types.String `tfsdk:"id"`
	SegmentPair  types.String `tfsdk:"segment_pair"`
	SourceZoneID types.Int64  `tfsdk:"source_zone_id"`
	DestZoneID   types.Int64  `tfsdk:"dest_zone_id"`
	Priority     types.Int64  `tfsdk:"priority"`
	Action       types.String `tfsdk:"action"`
	RuleState    types.String `tfsdk:"rule_state"`
	Logging      types.String `tfsdk:"logging"`
	LogPriority  types.String `tfsdk:"log_priority"`
	Comment      types.String `tfsdk:"comment"`
	// Match — network
	ACL        types.String `tfsdk:"acl"`
	SrcIP      types.String `tfsdk:"src_ip"`
	DstIP      types.String `tfsdk:"dst_ip"`
	EitherIP   types.String `tfsdk:"either_ip"`
	SrcPort    types.String `tfsdk:"src_port"`
	DstPort    types.String `tfsdk:"dst_port"`
	EitherPort types.String `tfsdk:"either_port"`
	Protocol   types.String `tfsdk:"protocol"`
	// Match — application
	Application types.String `tfsdk:"application"`
	AppGroup    types.String `tfsdk:"app_group"`
	// Match — DNS/domain
	SrcDNS    types.String `tfsdk:"src_dns"`
	DstDNS    types.String `tfsdk:"dst_dns"`
	EitherDNS types.String `tfsdk:"either_dns"`
	// Match — geo location
	SrcGeo    types.String `tfsdk:"src_geo"`
	DstGeo    types.String `tfsdk:"dst_geo"`
	EitherGeo types.String `tfsdk:"either_geo"`
	// Match — service
	SrcService    types.String `tfsdk:"src_service"`
	DstService    types.String `tfsdk:"dst_service"`
	EitherService types.String `tfsdk:"either_service"`
	// Match — address groups (references to arubasdwan_ip_address_group)
	SrcAddressGroup    types.String `tfsdk:"src_address_group"`
	DstAddressGroup    types.String `tfsdk:"dst_address_group"`
	EitherAddressGroup types.String `tfsdk:"either_address_group"`
	// Match — other
	DSCP    types.String `tfsdk:"dscp"`
	VLAN    types.String `tfsdk:"vlan"`
	Overlay types.String `tfsdk:"overlay"`
}

type securityPolicyResource struct {
	client *client.Client
}

func NewSecurityPolicyResource() resource.Resource {
	return &securityPolicyResource{}
}

func (r *securityPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_policy"
}

func (r *securityPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a security policy rule in the Aruba SD-WAN Orchestrator. " +
			"Uses the /vrf/config/securityPolicies API endpoints.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Composite identifier: segment_pair/src_dst/priority.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"segment_pair": schema.StringAttribute{
				Description: "The segment pair identifier (e.g. \"0_0\").",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_zone_id": schema.Int64Attribute{
				Description: "The source security zone ID.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"dest_zone_id": schema.Int64Attribute{
				Description: "The destination security zone ID.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"priority": schema.Int64Attribute{
				Description: "The rule priority (20000-65535, lower number = higher priority).",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
				Validators: []validator.Int64{
					int64validator.Between(20000, 65535),
				},
			},
			"action": schema.StringAttribute{
				Description: "The action to take: \"allow\" or \"deny\".",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("allow", "deny"),
				},
			},
			"rule_state": schema.StringAttribute{
				Description: "Whether the rule is enabled: \"enable\" or \"disable\". " +
					"If omitted, the existing Orchestrator value is preserved (default for new rules: \"enable\").",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("enable", "disable"),
				},
			},
			"logging": schema.StringAttribute{
				Description: "Whether logging is enabled for this rule: \"enable\" or \"disable\". " +
					"If omitted, the existing Orchestrator value is preserved (default for new rules: \"disable\"). " +
					"When set to \"enable\", log_priority must also be explicitly set in the configuration.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("enable", "disable"),
				},
			},
			"log_priority": schema.StringAttribute{
				Description: "The syslog priority level (\"0\"–\"7\"). " +
					"If omitted, the existing Orchestrator value is preserved (default for new rules: \"0\"). " +
					"Must be set explicitly when logging is \"enable\".",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("0", "1", "2", "3", "4", "5", "6", "7"),
				},
			},
			"comment": schema.StringAttribute{
				Description: "A comment for the policy rule.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"acl": schema.StringAttribute{
				Description: "ACL class name to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"src_ip": schema.StringAttribute{
				Description: "Source IP address or range to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"dst_ip": schema.StringAttribute{
				Description: "Destination IP address or range to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"src_port": schema.StringAttribute{
				Description: "Source port or range to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"dst_port": schema.StringAttribute{
				Description: "Destination port or range to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"protocol": schema.StringAttribute{
				Description: "Protocol to match (e.g. \"tcp\", \"udp\", \"ip\").",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"either_ip": schema.StringAttribute{
				Description: "Either source or destination IP to match (mutually exclusive with src_ip/dst_ip).",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"either_port": schema.StringAttribute{
				Description: "Either source or destination port to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"application": schema.StringAttribute{
				Description: "Application name to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"app_group": schema.StringAttribute{
				Description: "Application group to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"src_dns": schema.StringAttribute{
				Description: "Source DNS/domain pattern to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"dst_dns": schema.StringAttribute{
				Description: "Destination DNS/domain pattern to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"either_dns": schema.StringAttribute{
				Description: "Either DNS/domain pattern to match (supports wildcards, e.g. \"*google.com\").",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"src_geo": schema.StringAttribute{
				Description: "Source geo location to match (e.g. \"US\", \"DE\").",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"dst_geo": schema.StringAttribute{
				Description: "Destination geo location to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"either_geo": schema.StringAttribute{
				Description: "Either geo location to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"src_service": schema.StringAttribute{
				Description: "Source service (SaaS app name or organization) to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"dst_service": schema.StringAttribute{
				Description: "Destination service to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"either_service": schema.StringAttribute{
				Description: "Either service to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"src_address_group": schema.StringAttribute{
				Description: "Source IP address group to match. Reference an arubasdwan_ip_address_group by name.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"dst_address_group": schema.StringAttribute{
				Description: "Destination IP address group to match. Reference an arubasdwan_ip_address_group by name.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"either_address_group": schema.StringAttribute{
				Description: "Match either direction against an IP address group. Reference an arubasdwan_ip_address_group by name.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"dscp": schema.StringAttribute{
				Description: "DSCP value to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"vlan": schema.StringAttribute{
				Description: "Interface/VLAN to match (e.g. \"lan0\").",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"overlay": schema.StringAttribute{
				Description: "Overlay to match.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
		},
	}
}

// ValidateConfig enforces cross-field rules that cannot be expressed by a single
// attribute validator. Specifically: when logging is "enable", the user must set
// log_priority explicitly in the configuration (not rely on the default).
func (r *securityPolicyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg securityPolicyResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only enforce when logging is known and set to "enable".
	if cfg.Logging.IsNull() || cfg.Logging.IsUnknown() {
		return
	}
	if cfg.Logging.ValueString() != "enable" {
		return
	}

	// log_priority must be explicitly set in the configuration.
	if cfg.LogPriority.IsNull() || cfg.LogPriority.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("log_priority"),
			"Missing log_priority",
			"When logging is set to \"enable\", log_priority must also be explicitly set "+
				"to a syslog priority level (\"0\"–\"7\").",
		)
	}
}

func (r *securityPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// compositeID constructs the Terraform resource ID from the policy's unique key
// components. Format: "segment_pair/srcZone_dstZone/priority" (e.g. "0_0/20_21/1000").
// This composite ID is used to identify the resource in state and for import operations.
func compositeID(segmentPair string, srcZone, dstZone, priority int) string {
	return fmt.Sprintf("%s/%d_%d/%d", segmentPair, srcZone, dstZone, priority)
}

// parseCompositeID splits a composite ID string back into its component parts.
// Expected format: "segment_pair/srcZone_dstZone/priority" (e.g. "0_0/20_21/1000").
func parseCompositeID(id string) (segmentPair string, srcZone, dstZone, priority int, err error) {
	// Format: "0_0/20_21/1000"
	parts := strings.SplitN(id, "/", 3)
	if len(parts) != 3 {
		return "", 0, 0, 0, fmt.Errorf("invalid composite ID %q: expected segment_pair/src_dst/priority", id)
	}
	segmentPair = parts[0]

	zoneParts := strings.SplitN(parts[1], "_", 2)
	if len(zoneParts) != 2 {
		return "", 0, 0, 0, fmt.Errorf("invalid zone pair in ID %q", id)
	}
	srcZone, err = strconv.Atoi(zoneParts[0])
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("invalid source zone ID in %q: %w", id, err)
	}
	dstZone, err = strconv.Atoi(zoneParts[1])
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("invalid dest zone ID in %q: %w", id, err)
	}
	priority, err = strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("invalid priority in %q: %w", id, err)
	}
	return segmentPair, srcZone, dstZone, priority, nil
}

// modelToPolicy converts a Terraform resource model (with types.String/types.Int64
// wrappers) into a client.SecurityPolicy struct (with plain Go types) for use
// in API calls.
//
// rule_state, logging and log_priority are intentionally Optional+Computed
// without a Default so that values set on the Orchestrator (e.g. via the UI)
// are preserved when not specified in HCL. To avoid sending empty strings to
// the API on Create, sensible defaults are applied here as a fallback.
func modelToPolicy(m *securityPolicyResourceModel) client.SecurityPolicy {
	ruleState := m.RuleState.ValueString()
	if ruleState == "" {
		ruleState = "enable"
	}
	logging := m.Logging.ValueString()
	if logging == "" {
		logging = "disable"
	}
	logPriority := m.LogPriority.ValueString()
	if logPriority == "" {
		logPriority = "0"
	}

	return client.SecurityPolicy{
		SourceZoneID:  int(m.SourceZoneID.ValueInt64()),
		DestZoneID:    int(m.DestZoneID.ValueInt64()),
		Priority:      int(m.Priority.ValueInt64()),
		Action:        m.Action.ValueString(),
		RuleState:     ruleState,
		Logging:       logging,
		LogPriority:   logPriority,
		Comment:       m.Comment.ValueString(),
		ACL:           m.ACL.ValueString(),
		SrcIP:         m.SrcIP.ValueString(),
		DstIP:         m.DstIP.ValueString(),
		EitherIP:      m.EitherIP.ValueString(),
		SrcPort:       m.SrcPort.ValueString(),
		DstPort:       m.DstPort.ValueString(),
		EitherPort:    m.EitherPort.ValueString(),
		Protocol:      m.Protocol.ValueString(),
		Application:   m.Application.ValueString(),
		AppGroup:      m.AppGroup.ValueString(),
		SrcDNS:        m.SrcDNS.ValueString(),
		DstDNS:        m.DstDNS.ValueString(),
		EitherDNS:     m.EitherDNS.ValueString(),
		SrcGeo:        m.SrcGeo.ValueString(),
		DstGeo:        m.DstGeo.ValueString(),
		EitherGeo:     m.EitherGeo.ValueString(),
		SrcService:    m.SrcService.ValueString(),
		DstService:    m.DstService.ValueString(),
		EitherService: m.EitherService.ValueString(),
		DSCP:               m.DSCP.ValueString(),
		VLAN:               m.VLAN.ValueString(),
		Overlay:            m.Overlay.ValueString(),
		SrcAddressGroup:    m.SrcAddressGroup.ValueString(),
		DstAddressGroup:    m.DstAddressGroup.ValueString(),
		EitherAddressGroup: m.EitherAddressGroup.ValueString(),
	}
}

// policyToModel converts a client.SecurityPolicy struct (from the API) back into
// a Terraform resource model (with types.String/types.Int64 wrappers) for storing
// in Terraform state. It also constructs the composite ID.
func policyToModel(p *client.SecurityPolicy, segmentPair string) securityPolicyResourceModel {
	return securityPolicyResourceModel{
		ID:            types.StringValue(compositeID(segmentPair, p.SourceZoneID, p.DestZoneID, p.Priority)),
		SegmentPair:   types.StringValue(segmentPair),
		SourceZoneID:  types.Int64Value(int64(p.SourceZoneID)),
		DestZoneID:    types.Int64Value(int64(p.DestZoneID)),
		Priority:      types.Int64Value(int64(p.Priority)),
		Action:        types.StringValue(p.Action),
		RuleState:     types.StringValue(p.RuleState),
		Logging:       types.StringValue(p.Logging),
		LogPriority:   types.StringValue(p.LogPriority),
		Comment:       types.StringValue(p.Comment),
		ACL:           types.StringValue(p.ACL),
		SrcIP:         types.StringValue(p.SrcIP),
		DstIP:         types.StringValue(p.DstIP),
		EitherIP:      types.StringValue(p.EitherIP),
		SrcPort:       types.StringValue(p.SrcPort),
		DstPort:       types.StringValue(p.DstPort),
		EitherPort:    types.StringValue(p.EitherPort),
		Protocol:      types.StringValue(p.Protocol),
		Application:   types.StringValue(p.Application),
		AppGroup:      types.StringValue(p.AppGroup),
		SrcDNS:        types.StringValue(p.SrcDNS),
		DstDNS:        types.StringValue(p.DstDNS),
		EitherDNS:     types.StringValue(p.EitherDNS),
		SrcGeo:        types.StringValue(p.SrcGeo),
		DstGeo:        types.StringValue(p.DstGeo),
		EitherGeo:     types.StringValue(p.EitherGeo),
		SrcService:    types.StringValue(p.SrcService),
		DstService:    types.StringValue(p.DstService),
		EitherService: types.StringValue(p.EitherService),
		DSCP:               types.StringValue(p.DSCP),
		VLAN:               types.StringValue(p.VLAN),
		Overlay:            types.StringValue(p.Overlay),
		SrcAddressGroup:    types.StringValue(p.SrcAddressGroup),
		DstAddressGroup:    types.StringValue(p.DstAddressGroup),
		EitherAddressGroup: types.StringValue(p.EitherAddressGroup),
	}
}

func (r *securityPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan securityPolicyResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy := modelToPolicy(&plan)
	created, err := r.client.CreateSecurityPolicy(plan.SegmentPair.ValueString(), policy)
	if err != nil {
		resp.Diagnostics.AddError("Error creating security policy", err.Error())
		return
	}

	state := policyToModel(created, plan.SegmentPair.ValueString())
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *securityPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state securityPolicyResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	segmentPair, srcZone, dstZone, priority, err := parseCompositeID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error parsing security policy ID", err.Error())
		return
	}

	policy, err := r.client.GetSecurityPolicy(segmentPair, srcZone, dstZone, priority)
	if err != nil {
		resp.Diagnostics.AddError("Error reading security policy", err.Error())
		return
	}

	if policy == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	newState := policyToModel(policy, segmentPair)
	diags = resp.State.Set(ctx, newState)
	resp.Diagnostics.Append(diags...)
}

func (r *securityPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan securityPolicyResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy := modelToPolicy(&plan)
	updated, err := r.client.UpdateSecurityPolicy(plan.SegmentPair.ValueString(), policy)
	if err != nil {
		resp.Diagnostics.AddError("Error updating security policy", err.Error())
		return
	}

	state := policyToModel(updated, plan.SegmentPair.ValueString())
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *securityPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state securityPolicyResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	segmentPair, srcZone, dstZone, priority, err := parseCompositeID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error parsing security policy ID", err.Error())
		return
	}

	err = r.client.DeleteSecurityPolicy(segmentPair, srcZone, dstZone, priority)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting security policy", err.Error())
		return
	}
}

func (r *securityPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import ID format: "0_0/20_21/1000"
	_, _, _, _, err := parseCompositeID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error importing security policy",
			fmt.Sprintf("Invalid import ID %q. Expected format: segment_pair/srcZone_dstZone/priority (e.g. 0_0/20_21/1000)", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
