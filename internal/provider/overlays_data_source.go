package provider

import (
	"context"
	"fmt"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &overlaysDataSource{}
	_ datasource.DataSourceWithConfigure = &overlaysDataSource{}
)

// overlayACLEntryDSModel maps one entry of an overlay's built-in ACL.
type overlayACLEntryDSModel struct {
	Sequence           types.Int64  `tfsdk:"sequence"`
	Permit             types.Bool   `tfsdk:"permit"`
	MatchAll           types.Bool   `tfsdk:"match_all"`
	Comment            types.String `tfsdk:"comment"`
	Application        types.String `tfsdk:"application"`
	AppGroup           types.String `tfsdk:"app_group"`
	SrcIP              types.String `tfsdk:"src_ip"`
	DstIP              types.String `tfsdk:"dst_ip"`
	EitherIP           types.String `tfsdk:"either_ip"`
	SrcPort            types.String `tfsdk:"src_port"`
	DstPort            types.String `tfsdk:"dst_port"`
	EitherPort         types.String `tfsdk:"either_port"`
	Protocol           types.String `tfsdk:"protocol"`
	DSCP               types.String `tfsdk:"dscp"`
	SrcDNS             types.String `tfsdk:"src_dns"`
	DstDNS             types.String `tfsdk:"dst_dns"`
	EitherDNS          types.String `tfsdk:"either_dns"`
	SrcService         types.String `tfsdk:"src_service"`
	DstService         types.String `tfsdk:"dst_service"`
	EitherService      types.String `tfsdk:"either_service"`
	SrcAddressGroup    types.String `tfsdk:"src_address_group"`
	DstAddressGroup    types.String `tfsdk:"dst_address_group"`
	EitherAddressGroup types.String `tfsdk:"either_address_group"`
	SrcVRF             types.String `tfsdk:"src_vrf"`
	DstVRF             types.String `tfsdk:"dst_vrf"`
}

// overlayDSModel maps one Business Intent Overlay.
type overlayDSModel struct {
	ID             types.Int64              `tfsdk:"id"`
	Name           types.String             `tfsdk:"name"`
	MatchType      types.String             `tfsdk:"match_type"`
	InterfaceLabel types.String             `tfsdk:"interface_label"`
	ACLName        types.String             `tfsdk:"acl_name"`
	ACLEntries     []overlayACLEntryDSModel `tfsdk:"acl_entries"`
	ACLRaw         types.String             `tfsdk:"acl_raw"`
}

type overlaysDataSourceModel struct {
	Overlays []overlayDSModel `tfsdk:"overlays"`
}

type overlaysDataSource struct {
	client *client.Client
}

func NewOverlaysDataSource() datasource.DataSource {
	return &overlaysDataSource{}
}

func (d *overlaysDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_overlays"
}

func (d *overlaysDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches all Business Intent Overlays (BIO) from the Aruba SD-WAN Orchestrator via " +
			"GET /gms/rest/gms/overlays/config, including how each overlay selects traffic: by LAN " +
			"interface label, by an ACL defined on the appliance, or by the ACL built into the overlay " +
			"configuration. For the built-in ACL the individual entries (application, application group, " +
			"or match-all) are reported.",
		Attributes: map[string]schema.Attribute{
			"overlays": schema.ListNestedAttribute{
				Description: "List of overlays, sorted by overlay ID.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "Numeric overlay ID assigned by the Orchestrator.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Overlay name (e.g. \"Business\").",
							Computed:    true,
						},
						"match_type": schema.StringAttribute{
							Description: "How the overlay selects traffic: \"overlay_acl\" (ACL built into the overlay), " +
								"\"appliance_acl\" (reference to an ACL defined on the appliance), " +
								"\"interface_label\" (all traffic of a LAN interface label), or empty if nothing is configured.",
							Computed: true,
						},
						"interface_label": schema.StringAttribute{
							Description: "LAN interface label the overlay matches; empty unless match_type is \"interface_label\".",
							Computed:    true,
						},
						"acl_name": schema.StringAttribute{
							Description: "Name of the matched ACL; empty unless match_type is \"overlay_acl\" or \"appliance_acl\".",
							Computed:    true,
						},
						"acl_entries": schema.ListNestedAttribute{
							Description: "Entries of the ACL built into the overlay, sorted by sequence number. " +
								"Empty unless match_type is \"overlay_acl\".",
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"sequence": schema.Int64Attribute{
										Description: "Sequence number determining the evaluation order within the ACL.",
										Computed:    true,
									},
									"permit": schema.BoolAttribute{
										Description: "True if matching traffic is carried by this overlay, false if it is denied.",
										Computed:    true,
									},
									"match_all": schema.BoolAttribute{
										Description: "True if the entry sets no match criterion and therefore matches all traffic.",
										Computed:    true,
									},
									"application": schema.StringAttribute{
										Description: "Name of the application to match — built-in or user-defined, including DNS, compound, and port/protocol classifications.",
										Computed:    true,
									},
									"app_group": schema.StringAttribute{
										Description: "Name of the application group to match.",
										Computed:    true,
									},
									"src_ip": schema.StringAttribute{
										Description: "Source IP address or CIDR; comma-separated for multiple values.",
										Computed:    true,
									},
									"dst_ip": schema.StringAttribute{
										Description: "Destination IP address or CIDR; comma-separated for multiple values.",
										Computed:    true,
									},
									"either_ip": schema.StringAttribute{
										Description: "Match the IP address or CIDR in either direction; comma-separated for multiple values.",
										Computed:    true,
									},
									"src_port": schema.StringAttribute{
										Description: "Source port or range; comma-separated for multiple values.",
										Computed:    true,
									},
									"dst_port": schema.StringAttribute{
										Description: "Destination port or range; comma-separated for multiple values.",
										Computed:    true,
									},
									"either_port": schema.StringAttribute{
										Description: "Match the port or range in either direction; comma-separated for multiple values.",
										Computed:    true,
									},
									"protocol": schema.StringAttribute{
										Description: "IP protocol to match (e.g. \"tcp\", \"udp\", \"icmp\").",
										Computed:    true,
									},
									"dscp": schema.StringAttribute{
										Description: "DSCP marking to match (e.g. \"ef\").",
										Computed:    true,
									},
									"src_dns": schema.StringAttribute{
										Description: "Source DNS hostname pattern.",
										Computed:    true,
									},
									"dst_dns": schema.StringAttribute{
										Description: "Destination DNS hostname pattern.",
										Computed:    true,
									},
									"either_dns": schema.StringAttribute{
										Description: "Match the DNS hostname pattern in either direction; supports wildcards (e.g. \"*.example.com\").",
										Computed:    true,
									},
									"src_service": schema.StringAttribute{
										Description: "Source SaaS service or organization name.",
										Computed:    true,
									},
									"dst_service": schema.StringAttribute{
										Description: "Destination SaaS service or organization name.",
										Computed:    true,
									},
									"either_service": schema.StringAttribute{
										Description: "Match the SaaS service or organization name in either direction.",
										Computed:    true,
									},
									"src_address_group": schema.StringAttribute{
										Description: "Source address group name (see arubasdwan_ip_address_group).",
										Computed:    true,
									},
									"dst_address_group": schema.StringAttribute{
										Description: "Destination address group name.",
										Computed:    true,
									},
									"either_address_group": schema.StringAttribute{
										Description: "Match the address group in either direction.",
										Computed:    true,
									},
									"src_vrf": schema.StringAttribute{
										Description: "Source VRF segment ID.",
										Computed:    true,
									},
									"dst_vrf": schema.StringAttribute{
										Description: "Destination VRF segment ID.",
										Computed:    true,
									},
									"comment": schema.StringAttribute{
										Description: "Free-form comment of the entry.",
										Computed:    true,
									},
								},
							},
						},
						"acl_raw": schema.StringAttribute{
							Description: "The built-in ACL exactly as reported by the Orchestrator (a JSON document " +
								"embedded as a string). Useful for inspecting settings this provider does not model.",
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func (d *overlaysDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	d.client = apiClient
}

func (d *overlaysDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	overlays, err := d.client.GetOverlays()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read overlays",
			"Error calling GET /gms/rest/gms/overlays/config: "+err.Error(),
		)
		return
	}

	state := overlaysDataSourceModel{Overlays: make([]overlayDSModel, 0, len(overlays))}
	for _, overlay := range overlays {
		entries := make([]overlayACLEntryDSModel, 0, len(overlay.ACLEntries))
		for _, entry := range overlay.ACLEntries {
			entries = append(entries, overlayACLEntryDSModel{
				Sequence:           types.Int64Value(int64(entry.Sequence)),
				Permit:             types.BoolValue(entry.Permit),
				MatchAll:           types.BoolValue(entry.MatchAll()),
				Comment:            types.StringValue(entry.Comment),
				Application:        types.StringValue(entry.Match.Application),
				AppGroup:           types.StringValue(entry.Match.AppGroup),
				SrcIP:              types.StringValue(entry.Match.SrcIP),
				DstIP:              types.StringValue(entry.Match.DstIP),
				EitherIP:           types.StringValue(entry.Match.EitherIP),
				SrcPort:            types.StringValue(entry.Match.SrcPort),
				DstPort:            types.StringValue(entry.Match.DstPort),
				EitherPort:         types.StringValue(entry.Match.EitherPort),
				Protocol:           types.StringValue(entry.Match.Protocol),
				DSCP:               types.StringValue(entry.Match.DSCP),
				SrcDNS:             types.StringValue(entry.Match.SrcDNS),
				DstDNS:             types.StringValue(entry.Match.DstDNS),
				EitherDNS:          types.StringValue(entry.Match.EitherDNS),
				SrcService:         types.StringValue(entry.Match.SrcService),
				DstService:         types.StringValue(entry.Match.DstService),
				EitherService:      types.StringValue(entry.Match.EitherService),
				SrcAddressGroup:    types.StringValue(entry.Match.SrcAddressGroup),
				DstAddressGroup:    types.StringValue(entry.Match.DstAddressGroup),
				EitherAddressGroup: types.StringValue(entry.Match.EitherAddressGroup),
				SrcVRF:             types.StringValue(entry.Match.SrcVRF),
				DstVRF:             types.StringValue(entry.Match.DstVRF),
			})
		}
		state.Overlays = append(state.Overlays, overlayDSModel{
			ID:             types.Int64Value(int64(overlay.ID)),
			Name:           types.StringValue(overlay.Name),
			MatchType:      types.StringValue(overlay.MatchType),
			InterfaceLabel: types.StringValue(overlay.InterfaceLabel),
			ACLName:        types.StringValue(overlay.ACLName),
			ACLEntries:     entries,
			ACLRaw:         types.StringValue(overlay.RawACL),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
