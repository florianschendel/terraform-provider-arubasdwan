package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// tfStringSlice converts a slice of framework string values into plain Go
// strings, skipping null/unknown entries.
func tfStringSlice(values []types.String) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if !v.IsNull() && !v.IsUnknown() {
			out = append(out, v.ValueString())
		}
	}
	return out
}

var (
	_ datasource.DataSource              = &applianceDeploymentsDataSource{}
	_ datasource.DataSourceWithConfigure = &applianceDeploymentsDataSource{}
)

// applianceDHCPRangeDSModel maps one IP allocation range of a DHCP server.
type applianceDHCPRangeDSModel struct {
	Start types.String `tfsdk:"start"`
	End   types.String `tfsdk:"end"`
}

// applianceDHCPReservationDSModel maps one static DHCP reservation.
type applianceDHCPReservationDSModel struct {
	Hostname types.String `tfsdk:"hostname"`
	IP       types.String `tfsdk:"ip"`
	MAC      types.String `tfsdk:"mac"`
}

// applianceDHCPConfigDSModel maps the DHCP server/relay configuration of a
// LAN interface.
type applianceDHCPConfigDSModel struct {
	Mode            types.String                      `tfsdk:"mode"`
	Prefix          types.String                      `tfsdk:"prefix"`
	IPStart         types.String                      `tfsdk:"ip_start"`
	IPEnd           types.String                      `tfsdk:"ip_end"`
	Ranges          []applianceDHCPRangeDSModel       `tfsdk:"ranges"`
	Gateways        []types.String                    `tfsdk:"gateways"`
	DNSServers      []types.String                    `tfsdk:"dns_servers"`
	NTPServers      []types.String                    `tfsdk:"ntp_servers"`
	NetbiosServers  []types.String                    `tfsdk:"netbios_servers"`
	NetbiosNodeType types.String                      `tfsdk:"netbios_node_type"`
	DefaultLease    types.Int64                       `tfsdk:"default_lease"`
	MaxLease        types.Int64                       `tfsdk:"max_lease"`
	Options         map[string]types.String           `tfsdk:"options"`
	Failover        types.Bool                        `tfsdk:"failover"`
	Reservations    []applianceDHCPReservationDSModel `tfsdk:"reservations"`
	DHCPServers     []types.String                    `tfsdk:"dhcp_servers"`
	Option82        types.Bool                        `tfsdk:"option82"`
	Option82Policy  types.String                      `tfsdk:"option82_policy"`
}

// applianceLicenseDSModel maps the EC license of an appliance.
type applianceLicenseDSModel struct {
	Tier           types.String `tfsdk:"tier"`
	TierBandwidth  types.Int64  `tfsdk:"tier_bandwidth"`
	Boost          types.Bool   `tfsdk:"boost"`
	BoostBandwidth types.Int64  `tfsdk:"boost_bandwidth"`
}

// applianceInterfaceDSModel maps one configured IP interface of an appliance.
type applianceInterfaceDSModel struct {
	Name                 types.String `tfsdk:"name"`
	Type                 types.String `tfsdk:"type"`
	IPAddress            types.String `tfsdk:"ip_address"`
	PrefixLength         types.Int64  `tfsdk:"prefix_length"`
	CIDR                 types.String `tfsdk:"cidr"`
	Label                types.String `tfsdk:"label"`
	VLAN                 types.String `tfsdk:"vlan"`
	DHCP                 types.Bool   `tfsdk:"dhcp"`
	NextHop              types.String `tfsdk:"next_hop"`
	NextHopIsPrivate     types.Bool   `tfsdk:"next_hop_is_private"`
	BehindNAT            types.Bool   `tfsdk:"behind_nat"`
	PublicIP             types.String `tfsdk:"public_ip"`
	IsPrivate            types.Bool   `tfsdk:"is_private"`
	VRFID                types.Int64  `tfsdk:"vrf_id"`
	VRFName              types.String `tfsdk:"vrf_name"`
	ZoneID               types.Int64  `tfsdk:"zone_id"`
	ZoneName             types.String `tfsdk:"zone_name"`
	FirewallMode         types.String `tfsdk:"firewall_mode"`
	MaxBandwidthOutbound types.Int64  `tfsdk:"max_bandwidth_outbound"`
	MaxBandwidthInbound  types.Int64  `tfsdk:"max_bandwidth_inbound"`

	DHCPConfig *applianceDHCPConfigDSModel `tfsdk:"dhcp_config"`
}

// applianceOverlayDSModel maps one Business Intent Overlay association.
type applianceOverlayDSModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

// applianceStaticRouteDSModel maps one locally configured static route.
type applianceStaticRouteDSModel struct {
	Prefix    types.String `tfsdk:"prefix"`
	NextHop   types.String `tfsdk:"next_hop"`
	Interface types.String `tfsdk:"interface"`
	Metric    types.Int64  `tfsdk:"metric"`
	VRFID     types.Int64  `tfsdk:"vrf_id"`
	VRFName   types.String `tfsdk:"vrf_name"`
	Advertise types.Bool   `tfsdk:"advertise"`
}

// applianceDeploymentDSModel maps one appliance with its interface list.
type applianceDeploymentDSModel struct {
	NePk            types.String                  `tfsdk:"ne_pk"`
	Hostname        types.String                  `tfsdk:"hostname"`
	Serial          types.String                  `tfsdk:"serial"`
	Model           types.String                  `tfsdk:"model"`
	Site            types.String                  `tfsdk:"site"`
	SoftwareVersion types.String                  `tfsdk:"software_version"`
	Mode            types.String                  `tfsdk:"mode"`
	NetworkRole     types.String                  `tfsdk:"network_role"`
	ManagementIP    types.String                  `tfsdk:"management_ip"`
	RegionID        types.Int64                   `tfsdk:"region_id"`
	RegionName      types.String                  `tfsdk:"region_name"`
	License         applianceLicenseDSModel       `tfsdk:"license"`
	SystemBWOut     types.Int64                   `tfsdk:"system_bandwidth_outbound"`
	SystemBWIn      types.Int64                   `tfsdk:"system_bandwidth_inbound"`
	TemplateGroups  []types.String                `tfsdk:"template_groups"`
	Overlays        []applianceOverlayDSModel     `tfsdk:"overlays"`
	StaticRoutes    []applianceStaticRouteDSModel `tfsdk:"static_routes"`
	Interfaces      []applianceInterfaceDSModel   `tfsdk:"interfaces"`
}

// applianceDeploymentsDataSourceModel maps the data source configuration and
// computed state.
type applianceDeploymentsDataSourceModel struct {
	NePk                 types.String                 `tfsdk:"ne_pk"`
	Cached               types.Bool                   `tfsdk:"cached"`
	Models               []types.String               `tfsdk:"models"`
	ExcludeModels        []types.String               `tfsdk:"exclude_models"`
	Sites                []types.String               `tfsdk:"sites"`
	ExcludeSites         []types.String               `tfsdk:"exclude_sites"`
	HostnameRegex        types.String                 `tfsdk:"hostname_regex"`
	ExcludeHostnameRegex types.String                 `tfsdk:"exclude_hostname_regex"`
	Appliances           []applianceDeploymentDSModel `tfsdk:"appliances"`
}

type applianceDeploymentsDataSource struct {
	client *client.Client
}

func NewApplianceDeploymentsDataSource() datasource.DataSource {
	return &applianceDeploymentsDataSource{}
}

func (d *applianceDeploymentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_appliance_deployments"
}

func (d *applianceDeploymentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches all appliances with their deployment configuration from the Aruba SD-WAN Orchestrator: " +
			"hostname, serial number, and every configured IP interface (mgmt0/mgmt1, WAN, LAN, VLAN sub-interfaces, " +
			"and loopback interfaces) including interface label, VRF segment, security zone, firewall mode, and " +
			"bandwidth limits. For WAN interfaces behind NAT (typically carrying private addresses) the public IP " +
			"discovered by the Orchestrator is included. Each appliance also reports its SD-WAN region, the applied " +
			"template groups, the Business Intent Overlays (BIO) it is associated with, its locally configured " +
			"static routes, the configured EC license, and the system bandwidth. LAN interfaces with DHCP " +
			"server or relay enabled include the full DHCP configuration.",
		Attributes: map[string]schema.Attribute{
			"ne_pk": schema.StringAttribute{
				Description: "Optional appliance primary key (e.g. \"3.NE\") to fetch a single appliance. " +
					"If omitted, all appliances are returned.",
				Optional: true,
			},
			"cached": schema.BoolAttribute{
				Description: "Whether to read deployment data from the Orchestrator database (true, default) or " +
					"live from each appliance (false). Live reads are slower and fail for unreachable appliances.",
				Optional: true,
			},
			"models": schema.ListAttribute{
				Description: "Only include appliances whose model is in this list (case-insensitive exact match, " +
					"e.g. [\"EC-S-B\"]). Filters apply before any per-appliance data is fetched.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"exclude_models": schema.ListAttribute{
				Description: "Exclude appliances whose model is in this list (case-insensitive exact match, " +
					"e.g. [\"EC-V\"]).",
				Optional:    true,
				ElementType: types.StringType,
			},
			"sites": schema.ListAttribute{
				Description: "Only include appliances whose site is in this list (case-insensitive exact match).",
				Optional:    true,
				ElementType: types.StringType,
			},
			"exclude_sites": schema.ListAttribute{
				Description: "Exclude appliances whose site is in this list (case-insensitive exact match).",
				Optional:    true,
				ElementType: types.StringType,
			},
			"hostname_regex": schema.StringAttribute{
				Description: "Only include appliances whose hostname matches this RE2 regular expression " +
					"(e.g. \"^br-\" or \"(?i)^(ber|ham)-\").",
				Optional: true,
			},
			"exclude_hostname_regex": schema.StringAttribute{
				Description: "Exclude appliances whose hostname matches this RE2 regular expression " +
					"(e.g. \"(?i)^(lab|test)-\").",
				Optional: true,
			},
			"appliances": schema.ListNestedAttribute{
				Description: "List of appliances with their deployment details, sorted by hostname.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"ne_pk": schema.StringAttribute{
							Description: "Orchestrator primary key of the appliance (e.g. \"3.NE\").",
							Computed:    true,
						},
						"hostname": schema.StringAttribute{
							Description: "Hostname of the appliance.",
							Computed:    true,
						},
						"serial": schema.StringAttribute{
							Description: "Hardware serial number of the appliance.",
							Computed:    true,
						},
						"model": schema.StringAttribute{
							Description: "Appliance model (e.g. \"EC-S-B\").",
							Computed:    true,
						},
						"site": schema.StringAttribute{
							Description: "Site name the appliance is tagged with in the Orchestrator.",
							Computed:    true,
						},
						"software_version": schema.StringAttribute{
							Description: "ECOS software version running on the appliance.",
							Computed:    true,
						},
						"mode": schema.StringAttribute{
							Description: "Deployment mode of the appliance (e.g. \"inline-router\").",
							Computed:    true,
						},
						"network_role": schema.StringAttribute{
							Description: "Network role as reported by the Orchestrator (e.g. \"0\" = spoke, \"1\" = hub).",
							Computed:    true,
						},
						"management_ip": schema.StringAttribute{
							Description: "IP address the Orchestrator uses to manage the appliance.",
							Computed:    true,
						},
						"region_id": schema.Int64Attribute{
							Description: "SD-WAN region ID the appliance belongs to (0 = Default region or regions not used).",
							Computed:    true,
						},
						"region_name": schema.StringAttribute{
							Description: "Resolved SD-WAN region name; empty if regions are not used or the name cannot be resolved.",
							Computed:    true,
						},
						"license": schema.SingleNestedAttribute{
							Description: "EC license of the appliance: from the deployment configuration (sysConfig licence), " +
								"filled in with the Orchestrator's portal license assignment for fields the deployment " +
								"omits; all values empty/zero when no license information is available.",
							Computed: true,
							Attributes: map[string]schema.Attribute{
								"tier": schema.StringAttribute{
									Description: "Tier (throughput) license name; empty if none.",
									Computed:    true,
								},
								"tier_bandwidth": schema.Int64Attribute{
									Description: "Tier bandwidth as reported by the Orchestrator, passed through unchanged; 0 if not set.",
									Computed:    true,
								},
								"boost": schema.BoolAttribute{
									Description: "True if the WAN optimization (Boost) license is enabled.",
									Computed:    true,
								},
								"boost_bandwidth": schema.Int64Attribute{
									Description: "Boost bandwidth as reported by the Orchestrator, passed through unchanged; 0 if not set.",
									Computed:    true,
								},
							},
						},
						"system_bandwidth_outbound": schema.Int64Attribute{
							Description: "System maximum outbound bandwidth in Kbps from the deployment (0 = not set).",
							Computed:    true,
						},
						"system_bandwidth_inbound": schema.Int64Attribute{
							Description: "System maximum inbound bandwidth in Kbps from the deployment (0 = not set; " +
								"requires inbound shaping to be enabled).",
							Computed: true,
						},
						"template_groups": schema.ListAttribute{
							Description: "Names of the template groups applied to the appliance, sorted alphabetically.",
							Computed:    true,
							ElementType: types.StringType,
						},
						"overlays": schema.ListNestedAttribute{
							Description: "Business Intent Overlays (BIO) the appliance is associated with, sorted by overlay ID.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"id": schema.StringAttribute{
										Description: "Numeric overlay ID as reported by the Orchestrator (e.g. \"1\").",
										Computed:    true,
									},
									"name": schema.StringAttribute{
										Description: "Overlay name (e.g. \"RealTime\"); empty if it cannot be resolved.",
										Computed:    true,
									},
								},
							},
						},
						"static_routes": schema.ListNestedAttribute{
							Description: "Locally configured (static) routes of the appliance from its subnet table " +
								"across all VRF segments, sorted by VRF segment and prefix. Learned and " +
								"system-generated routes are excluded.",
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"prefix": schema.StringAttribute{
										Description: "Destination prefix in CIDR notation (e.g. \"10.20.0.0/16\").",
										Computed:    true,
									},
									"next_hop": schema.StringAttribute{
										Description: "Next hop IP address; empty if not applicable.",
										Computed:    true,
									},
									"interface": schema.StringAttribute{
										Description: "Egress interface name; empty if not applicable.",
										Computed:    true,
									},
									"metric": schema.Int64Attribute{
										Description: "Route metric (lower value = higher priority).",
										Computed:    true,
									},
									"vrf_id": schema.Int64Attribute{
										Description: "VRF segment ID the route belongs to (0 = Default).",
										Computed:    true,
									},
									"vrf_name": schema.StringAttribute{
										Description: "Resolved VRF segment name; empty if it cannot be resolved.",
										Computed:    true,
									},
									"advertise": schema.BoolAttribute{
										Description: "True if the route is advertised to SD-WAN peers.",
										Computed:    true,
									},
								},
							},
						},
						"interfaces": schema.ListNestedAttribute{
							Description: "All configured IP addresses of the appliance, one entry per address: " +
								"management interfaces first, then WAN/LAN (including VLAN sub-interfaces) in " +
								"deployment order, then loopback interfaces. The interface name is NOT unique " +
								"within this list — a dual-stack interface appears once per address family " +
								"(IPv4 and IPv6). When building maps keyed by name, group with the HCL ellipsis " +
								"operator (for i in interfaces : i.name => i.cidr...) or include the cidr in the key.",
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"name": schema.StringAttribute{
										Description: "Interface name (e.g. \"mgmt0\", \"wan0\", \"wan0.100\", \"lan0\", \"loopback100\").",
										Computed:    true,
									},
									"type": schema.StringAttribute{
										Description: "Interface type: \"mgmt\", \"wan\", \"lan\", \"loopback\", or \"other\".",
										Computed:    true,
									},
									"ip_address": schema.StringAttribute{
										Description: "Configured IP address of the interface (without prefix length).",
										Computed:    true,
									},
									"prefix_length": schema.Int64Attribute{
										Description: "Network mask as prefix length (e.g. 24).",
										Computed:    true,
									},
									"cidr": schema.StringAttribute{
										Description: "IP address in CIDR notation (e.g. \"10.1.2.3/24\").",
										Computed:    true,
									},
									"label": schema.StringAttribute{
										Description: "Interface label name (e.g. \"INET1\", \"MPLS\"); empty if no label is assigned.",
										Computed:    true,
									},
									"vlan": schema.StringAttribute{
										Description: "VLAN ID for VLAN sub-interfaces; empty otherwise.",
										Computed:    true,
									},
									"dhcp": schema.BoolAttribute{
										Description: "True if the interface obtains its address dynamically (DHCP/SLAAC). " +
											"For DHCP WAN interfaces the current address seen by the Orchestrator is reported.",
										Computed: true,
									},
									"next_hop": schema.StringAttribute{
										Description: "Configured next hop / gateway IP address of the interface (WAN gateway for " +
											"WAN interfaces, management gateway for mgmt interfaces). For DHCP WAN interfaces " +
											"the current gateway seen by the Orchestrator is reported. Empty for loopback " +
											"interfaces and when no next hop is configured.",
										Computed: true,
									},
									"next_hop_is_private": schema.BoolAttribute{
										Description: "True if next_hop is private or otherwise not globally routable (same ranges " +
											"as is_private); false when next_hop is empty.",
										Computed: true,
									},
									"behind_nat": schema.BoolAttribute{
										Description: "True if the Orchestrator considers this WAN interface to be behind a NAT device.",
										Computed:    true,
									},
									"public_ip": schema.StringAttribute{
										Description: "Public IP address discovered by the Orchestrator for this WAN interface " +
											"(the \"discovered IP\" of interfaces behind NAT); empty if none was resolved.",
										Computed: true,
									},
									"is_private": schema.BoolAttribute{
										Description: "True if ip_address is private or otherwise not globally routable: " +
											"RFC1918, carrier-grade NAT (RFC6598), link-local, and loopback for IPv4; " +
											"unique local (RFC4193, ULA), link-local, and loopback for IPv6.",
										Computed: true,
									},
									"vrf_id": schema.Int64Attribute{
										Description: "VRF segment ID the interface is assigned to (0 = Default). " +
											"Always 0 for management interfaces; loopback interfaces report their segment " +
											"on Orchestrator 9.7.0 and later (0 on older versions, which do not expose it).",
										Computed: true,
									},
									"vrf_name": schema.StringAttribute{
										Description: "Resolved VRF segment name (e.g. \"Default\"); empty for management " +
											"interfaces, for loopback interfaces on Orchestrator versions before 9.7.0, or " +
											"if the name cannot be resolved.",
										Computed: true,
									},
									"zone_id": schema.Int64Attribute{
										Description: "Security zone ID assigned to the interface (0 = no zone).",
										Computed:    true,
									},
									"zone_name": schema.StringAttribute{
										Description: "Resolved security zone name; empty if no zone is assigned or the name cannot be resolved.",
										Computed:    true,
									},
									"firewall_mode": schema.StringAttribute{
										Description: "Firewall mode of WAN interfaces: \"allow-all\", \"hardened\", \"stateful\", " +
											"or \"stateful-snat\"; empty for non-WAN interfaces.",
										Computed: true,
									},
									"max_bandwidth_outbound": schema.Int64Attribute{
										Description: "Maximum outbound (LAN to WAN) bandwidth of the interface in Kbps, from the " +
											"per-interface shaper in the deployment or, when no shaper is configured there, from " +
											"the Orchestrator's resolved interface view; 0 if not set (WAN interfaces only).",
										Computed: true,
									},
									"max_bandwidth_inbound": schema.Int64Attribute{
										Description: "Maximum inbound (WAN to LAN) bandwidth of the interface in Kbps, from the " +
											"per-interface shaper in the deployment or, when no shaper is configured there, from " +
											"the Orchestrator's resolved interface view; 0 if not set (WAN interfaces only).",
										Computed: true,
									},
									"dhcp_config": schema.SingleNestedAttribute{
										Description: "DHCP server or relay configuration of the LAN interface; null when " +
											"neither is enabled. The mode attribute selects which field group is populated.",
										Computed: true,
										Attributes: map[string]schema.Attribute{
											"mode": schema.StringAttribute{
												Description: "Either \"server\" or \"relay\".",
												Computed:    true,
											},
											"prefix": schema.StringAttribute{
												Description: "DHCP subnet in CIDR notation (server mode).",
												Computed:    true,
											},
											"ip_start": schema.StringAttribute{
												Description: "First allocatable IP address (server mode).",
												Computed:    true,
											},
											"ip_end": schema.StringAttribute{
												Description: "Last allocatable IP address (server mode).",
												Computed:    true,
											},
											"ranges": schema.ListNestedAttribute{
												Description: "Additional IP allocation ranges (server mode).",
												Computed:    true,
												NestedObject: schema.NestedAttributeObject{
													Attributes: map[string]schema.Attribute{
														"start": schema.StringAttribute{
															Description: "First IP address of the range.",
															Computed:    true,
														},
														"end": schema.StringAttribute{
															Description: "Last IP address of the range.",
															Computed:    true,
														},
													},
												},
											},
											"gateways": schema.ListAttribute{
												Description: "Gateway IP addresses handed out to clients (server mode).",
												Computed:    true,
												ElementType: types.StringType,
											},
											"dns_servers": schema.ListAttribute{
												Description: "DNS server IP addresses handed out to clients (server mode).",
												Computed:    true,
												ElementType: types.StringType,
											},
											"ntp_servers": schema.ListAttribute{
												Description: "NTP server IP addresses handed out to clients (server mode).",
												Computed:    true,
												ElementType: types.StringType,
											},
											"netbios_servers": schema.ListAttribute{
												Description: "NetBIOS name server IP addresses (server mode).",
												Computed:    true,
												ElementType: types.StringType,
											},
											"netbios_node_type": schema.StringAttribute{
												Description: "NetBIOS node type: \"B\", \"P\", \"M\", or \"H\" (server mode).",
												Computed:    true,
											},
											"default_lease": schema.Int64Attribute{
												Description: "Default lease time in seconds (server mode).",
												Computed:    true,
											},
											"max_lease": schema.Int64Attribute{
												Description: "Maximum lease time in seconds (server mode).",
												Computed:    true,
											},
											"options": schema.MapAttribute{
												Description: "Additional DHCP options keyed by option ID (server mode).",
												Computed:    true,
												ElementType: types.StringType,
											},
											"failover": schema.BoolAttribute{
												Description: "True if DHCP failover is enabled (server mode).",
												Computed:    true,
											},
											"reservations": schema.ListNestedAttribute{
												Description: "Static IP reservations, sorted by hostname (server mode).",
												Computed:    true,
												NestedObject: schema.NestedAttributeObject{
													Attributes: map[string]schema.Attribute{
														"hostname": schema.StringAttribute{
															Description: "Hostname of the reservation.",
															Computed:    true,
														},
														"ip": schema.StringAttribute{
															Description: "Reserved IP address.",
															Computed:    true,
														},
														"mac": schema.StringAttribute{
															Description: "MAC address of the host.",
															Computed:    true,
														},
													},
												},
											},
											"dhcp_servers": schema.ListAttribute{
												Description: "Destination DHCP server IP addresses (relay mode).",
												Computed:    true,
												ElementType: types.StringType,
											},
											"option82": schema.BoolAttribute{
												Description: "True if DHCP option 82 is enabled (relay mode).",
												Computed:    true,
											},
											"option82_policy": schema.StringAttribute{
												Description: "Option 82 policy: \"append\", \"replace\", \"forward\", or \"discard\" (relay mode).",
												Computed:    true,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *applianceDeploymentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *applianceDeploymentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config applianceDeploymentsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Default to cached reads from the Orchestrator database.
	cached := true
	if !config.Cached.IsNull() {
		cached = config.Cached.ValueBool()
	}

	// Assemble the appliance filter; it is applied before any per-appliance
	// data is fetched.
	filter := client.ApplianceFilter{
		NePk:          config.NePk.ValueString(),
		Models:        tfStringSlice(config.Models),
		ExcludeModels: tfStringSlice(config.ExcludeModels),
		Sites:         tfStringSlice(config.Sites),
		ExcludeSites:  tfStringSlice(config.ExcludeSites),
	}
	if pattern := config.HostnameRegex.ValueString(); pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("hostname_regex"),
				"Invalid regular expression",
				fmt.Sprintf("Cannot compile %q: %s", pattern, err),
			)
			return
		}
		filter.HostnameRegex = re
	}
	if pattern := config.ExcludeHostnameRegex.ValueString(); pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("exclude_hostname_regex"),
				"Invalid regular expression",
				fmt.Sprintf("Cannot compile %q: %s", pattern, err),
			)
			return
		}
		filter.ExcludeHostnameRegex = re
	}

	deployments, warnings, err := d.client.GetApplianceDeployments(filter, cached)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read appliance deployments",
			"Error reading appliance deployment data from the Orchestrator: "+err.Error(),
		)
		return
	}

	// Auxiliary data (loopbacks, discovered IPs, label names) may be
	// partially unavailable; surface this without failing the read.
	for _, w := range warnings {
		resp.Diagnostics.AddWarning("Incomplete appliance deployment data", w)
	}

	state := config
	state.Appliances = make([]applianceDeploymentDSModel, 0, len(deployments))
	for _, dep := range deployments {
		interfaces := make([]applianceInterfaceDSModel, 0, len(dep.Interfaces))
		for _, iface := range dep.Interfaces {
			var dhcpConfig *applianceDHCPConfigDSModel
			if cfg := iface.DHCPConfig; cfg != nil {
				ranges := make([]applianceDHCPRangeDSModel, 0, len(cfg.Ranges))
				for _, r := range cfg.Ranges {
					ranges = append(ranges, applianceDHCPRangeDSModel{
						Start: types.StringValue(r.Start),
						End:   types.StringValue(r.End),
					})
				}
				reservations := make([]applianceDHCPReservationDSModel, 0, len(cfg.Reservations))
				for _, r := range cfg.Reservations {
					reservations = append(reservations, applianceDHCPReservationDSModel{
						Hostname: types.StringValue(r.Hostname),
						IP:       types.StringValue(r.IP),
						MAC:      types.StringValue(r.MAC),
					})
				}
				options := make(map[string]types.String, len(cfg.Options))
				for id, value := range cfg.Options {
					options[id] = types.StringValue(value)
				}
				dhcpConfig = &applianceDHCPConfigDSModel{
					Mode:            types.StringValue(cfg.Mode),
					Prefix:          types.StringValue(cfg.Prefix),
					IPStart:         types.StringValue(cfg.IPStart),
					IPEnd:           types.StringValue(cfg.IPEnd),
					Ranges:          ranges,
					Gateways:        stringSliceToTF(cfg.Gateways),
					DNSServers:      stringSliceToTF(cfg.DNSServers),
					NTPServers:      stringSliceToTF(cfg.NTPServers),
					NetbiosServers:  stringSliceToTF(cfg.NetbiosServers),
					NetbiosNodeType: types.StringValue(cfg.NetbiosNodeType),
					DefaultLease:    types.Int64Value(cfg.DefaultLease),
					MaxLease:        types.Int64Value(cfg.MaxLease),
					Options:         options,
					Failover:        types.BoolValue(cfg.Failover),
					Reservations:    reservations,
					DHCPServers:     stringSliceToTF(cfg.DHCPServers),
					Option82:        types.BoolValue(cfg.Option82),
					Option82Policy:  types.StringValue(cfg.Option82Policy),
				}
			}
			interfaces = append(interfaces, applianceInterfaceDSModel{
				Name:                 types.StringValue(iface.Name),
				Type:                 types.StringValue(iface.Type),
				IPAddress:            types.StringValue(iface.IPAddress),
				PrefixLength:         types.Int64Value(int64(iface.PrefixLength)),
				CIDR:                 types.StringValue(iface.CIDR),
				Label:                types.StringValue(iface.Label),
				VLAN:                 types.StringValue(iface.VLAN),
				DHCP:                 types.BoolValue(iface.DHCP),
				NextHop:              types.StringValue(iface.NextHop),
				NextHopIsPrivate:     types.BoolValue(iface.NextHopIsPrivate),
				BehindNAT:            types.BoolValue(iface.BehindNAT),
				PublicIP:             types.StringValue(iface.PublicIP),
				IsPrivate:            types.BoolValue(iface.IsPrivate),
				VRFID:                types.Int64Value(int64(iface.VRFID)),
				VRFName:              types.StringValue(iface.VRFName),
				ZoneID:               types.Int64Value(int64(iface.ZoneID)),
				ZoneName:             types.StringValue(iface.ZoneName),
				FirewallMode:         types.StringValue(iface.FirewallMode),
				MaxBandwidthOutbound: types.Int64Value(iface.MaxBandwidthOutbound),
				MaxBandwidthInbound:  types.Int64Value(iface.MaxBandwidthInbound),
				DHCPConfig:           dhcpConfig,
			})
		}
		templateGroups := make([]types.String, 0, len(dep.TemplateGroups))
		for _, group := range dep.TemplateGroups {
			templateGroups = append(templateGroups, types.StringValue(group))
		}

		overlays := make([]applianceOverlayDSModel, 0, len(dep.Overlays))
		for _, overlay := range dep.Overlays {
			overlays = append(overlays, applianceOverlayDSModel{
				ID:   types.StringValue(overlay.ID),
				Name: types.StringValue(overlay.Name),
			})
		}

		staticRoutes := make([]applianceStaticRouteDSModel, 0, len(dep.StaticRoutes))
		for _, route := range dep.StaticRoutes {
			staticRoutes = append(staticRoutes, applianceStaticRouteDSModel{
				Prefix:    types.StringValue(route.Prefix),
				NextHop:   types.StringValue(route.NextHop),
				Interface: types.StringValue(route.Interface),
				Metric:    types.Int64Value(int64(route.Metric)),
				VRFID:     types.Int64Value(int64(route.VRFID)),
				VRFName:   types.StringValue(route.VRFName),
				Advertise: types.BoolValue(route.Advertise),
			})
		}

		state.Appliances = append(state.Appliances, applianceDeploymentDSModel{
			NePk:            types.StringValue(dep.NePk),
			Hostname:        types.StringValue(dep.HostName),
			Serial:          types.StringValue(dep.Serial),
			Model:           types.StringValue(dep.Model),
			Site:            types.StringValue(dep.Site),
			SoftwareVersion: types.StringValue(dep.SoftwareVersion),
			Mode:            types.StringValue(dep.Mode),
			NetworkRole:     types.StringValue(dep.NetworkRole),
			ManagementIP:    types.StringValue(dep.IP),
			RegionID:        types.Int64Value(int64(dep.RegionID)),
			RegionName:      types.StringValue(dep.RegionName),
			License: applianceLicenseDSModel{
				Tier:           types.StringValue(dep.License.Tier),
				TierBandwidth:  types.Int64Value(dep.License.TierBandwidth),
				Boost:          types.BoolValue(dep.License.Boost),
				BoostBandwidth: types.Int64Value(dep.License.BoostBandwidth),
			},
			SystemBWOut:    types.Int64Value(dep.SystemBandwidthOutbound),
			SystemBWIn:     types.Int64Value(dep.SystemBandwidthInbound),
			TemplateGroups: templateGroups,
			Overlays:       overlays,
			StaticRoutes:   staticRoutes,
			Interfaces:     interfaces,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
