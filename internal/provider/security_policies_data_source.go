package provider

import (
	"context"
	"fmt"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface checks for the security policies data source.
var (
	_ datasource.DataSource              = &securityPoliciesDataSource{}
	_ datasource.DataSourceWithConfigure = &securityPoliciesDataSource{}
)

// securityPolicyDSModel maps a single security policy to the data source schema.
// It contains the same fields as the resource model but without the composite ID
// and segment_pair (since those are provided as input to the data source).
type securityPolicyDSModel struct {
	SourceZoneID  types.Int64  `tfsdk:"source_zone_id"`
	DestZoneID    types.Int64  `tfsdk:"dest_zone_id"`
	Priority      types.Int64  `tfsdk:"priority"`
	Action        types.String `tfsdk:"action"`
	RuleState     types.String `tfsdk:"rule_state"`
	Logging       types.String `tfsdk:"logging"`
	LogPriority   types.String `tfsdk:"log_priority"`
	Comment       types.String `tfsdk:"comment"`
	ACL           types.String `tfsdk:"acl"`
	SrcIP         types.String `tfsdk:"src_ip"`
	DstIP         types.String `tfsdk:"dst_ip"`
	EitherIP      types.String `tfsdk:"either_ip"`
	SrcPort       types.String `tfsdk:"src_port"`
	DstPort       types.String `tfsdk:"dst_port"`
	EitherPort    types.String `tfsdk:"either_port"`
	Protocol      types.String `tfsdk:"protocol"`
	Application   types.String `tfsdk:"application"`
	AppGroup      types.String `tfsdk:"app_group"`
	SrcDNS        types.String `tfsdk:"src_dns"`
	DstDNS        types.String `tfsdk:"dst_dns"`
	EitherDNS     types.String `tfsdk:"either_dns"`
	SrcGeo        types.String `tfsdk:"src_geo"`
	DstGeo        types.String `tfsdk:"dst_geo"`
	EitherGeo     types.String `tfsdk:"either_geo"`
	SrcService    types.String `tfsdk:"src_service"`
	DstService    types.String `tfsdk:"dst_service"`
	EitherService types.String `tfsdk:"either_service"`
	DSCP               types.String `tfsdk:"dscp"`
	VLAN               types.String `tfsdk:"vlan"`
	Overlay            types.String `tfsdk:"overlay"`
	SrcAddressGroup    types.String `tfsdk:"src_address_group"`
	DstAddressGroup    types.String `tfsdk:"dst_address_group"`
	EitherAddressGroup types.String `tfsdk:"either_address_group"`
}

// securityPoliciesDataSourceModel maps the data source schema. Users provide a
// segment_pair as input, and the data source returns all policies for that pair.
//
// Usage in Terraform:
//
//	data "arubasdwan_security_policies" "default" {
//	  segment_pair = "0_0"  # Default VRF to Default VRF
//	}
//	output "policies" { value = data.arubasdwan_security_policies.default.security_policies }
type securityPoliciesDataSourceModel struct {
	SegmentPair      types.String            `tfsdk:"segment_pair"`
	SecurityPolicies []securityPolicyDSModel `tfsdk:"security_policies"`
}

type securityPoliciesDataSource struct {
	client *client.Client
}

func NewSecurityPoliciesDataSource() datasource.DataSource {
	return &securityPoliciesDataSource{}
}

func (d *securityPoliciesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_policies"
}

func (d *securityPoliciesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	policyAttrs := map[string]schema.Attribute{
		"source_zone_id": schema.Int64Attribute{Computed: true, Description: "Source security zone ID."},
		"dest_zone_id":   schema.Int64Attribute{Computed: true, Description: "Destination security zone ID."},
		"priority":       schema.Int64Attribute{Computed: true, Description: "Rule priority."},
		"action":         schema.StringAttribute{Computed: true, Description: "Action: allow or deny."},
		"rule_state":     schema.StringAttribute{Computed: true, Description: "Rule state: enable or disable."},
		"logging":        schema.StringAttribute{Computed: true, Description: "Logging: enable or disable."},
		"log_priority":   schema.StringAttribute{Computed: true, Description: "Logging priority level."},
		"comment":        schema.StringAttribute{Computed: true, Description: "Rule comment."},
		"acl":            schema.StringAttribute{Computed: true, Description: "ACL class name."},
		"src_ip":         schema.StringAttribute{Computed: true, Description: "Source IP match."},
		"dst_ip":         schema.StringAttribute{Computed: true, Description: "Destination IP match."},
		"either_ip":      schema.StringAttribute{Computed: true, Description: "Either IP match."},
		"src_port":       schema.StringAttribute{Computed: true, Description: "Source port match."},
		"dst_port":       schema.StringAttribute{Computed: true, Description: "Destination port match."},
		"either_port":    schema.StringAttribute{Computed: true, Description: "Either port match."},
		"protocol":       schema.StringAttribute{Computed: true, Description: "Protocol match."},
		"application":    schema.StringAttribute{Computed: true, Description: "Application match."},
		"app_group":      schema.StringAttribute{Computed: true, Description: "Application group match."},
		"src_dns":        schema.StringAttribute{Computed: true, Description: "Source DNS match."},
		"dst_dns":        schema.StringAttribute{Computed: true, Description: "Destination DNS match."},
		"either_dns":     schema.StringAttribute{Computed: true, Description: "Either DNS match."},
		"src_geo":        schema.StringAttribute{Computed: true, Description: "Source geo location match."},
		"dst_geo":        schema.StringAttribute{Computed: true, Description: "Destination geo location match."},
		"either_geo":     schema.StringAttribute{Computed: true, Description: "Either geo location match."},
		"src_service":    schema.StringAttribute{Computed: true, Description: "Source service match."},
		"dst_service":    schema.StringAttribute{Computed: true, Description: "Destination service match."},
		"either_service": schema.StringAttribute{Computed: true, Description: "Either service match."},
		"dscp":                 schema.StringAttribute{Computed: true, Description: "DSCP match."},
		"vlan":                 schema.StringAttribute{Computed: true, Description: "Interface/VLAN match."},
		"overlay":              schema.StringAttribute{Computed: true, Description: "Overlay match."},
		"src_address_group":    schema.StringAttribute{Computed: true, Description: "Source IP address group reference."},
		"dst_address_group":    schema.StringAttribute{Computed: true, Description: "Destination IP address group reference."},
		"either_address_group": schema.StringAttribute{Computed: true, Description: "Either direction IP address group reference."},
	}

	resp.Schema = schema.Schema{
		Description: "Fetches security policies for a segment pair from the Orchestrator.",
		Attributes: map[string]schema.Attribute{
			"segment_pair": schema.StringAttribute{
				Description: "The segment pair to query (e.g. \"0_0\").",
				Required:    true,
			},
			"security_policies": schema.ListNestedAttribute{
				Description: "List of security policy rules.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: policyAttrs,
				},
			},
		},
	}
}

func (d *securityPoliciesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData))
		return
	}
	d.client = apiClient
}

func (d *securityPoliciesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config securityPoliciesDataSourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	segmentPair := config.SegmentPair.ValueString()
	policies, err := d.client.GetSecurityPolicies(segmentPair)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read security policies",
			fmt.Sprintf("Error calling GET /vrf/config/securityPolicies?map=%s: %s", segmentPair, err.Error()))
		return
	}

	state := securityPoliciesDataSourceModel{
		SegmentPair: config.SegmentPair,
	}

	for _, p := range policies {
		state.SecurityPolicies = append(state.SecurityPolicies, securityPolicyDSModel{
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
		})
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
