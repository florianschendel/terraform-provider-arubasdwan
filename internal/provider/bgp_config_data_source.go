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

var (
	_ datasource.DataSource              = &bgpConfigDataSource{}
	_ datasource.DataSourceWithConfigure = &bgpConfigDataSource{}
)

// bgpSystemDSModel maps the BGP system configuration of one VRF segment.
type bgpSystemDSModel struct {
	Enabled               types.Bool   `tfsdk:"enabled"`
	ASN                   types.Int64  `tfsdk:"asn"`
	RouterID              types.String `tfsdk:"router_id"`
	GracefulRestart       types.Bool   `tfsdk:"graceful_restart"`
	MaxRestartTime        types.Int64  `tfsdk:"max_restart_time"`
	StalePathTime         types.Int64  `tfsdk:"stale_path_time"`
	RedistOSPF            types.Bool   `tfsdk:"redistribute_ospf"`
	RedistOSPFFilter      types.Int64  `tfsdk:"redistribute_ospf_filter"`
	RemoteASPathAdvertise types.Bool   `tfsdk:"remote_as_path_advertise"`
}

// bgpNeighborDSModel maps one configured BGP neighbor of a VRF segment.
type bgpNeighborDSModel struct {
	IP                types.String `tfsdk:"ip"`
	RemoteAS          types.Int64  `tfsdk:"remote_as"`
	Type              types.String `tfsdk:"type"`
	Enabled           types.Bool   `tfsdk:"enabled"`
	ImportRoutes      types.Bool   `tfsdk:"import_routes"`
	ExportMap         types.Int64  `tfsdk:"export_map"`
	HoldTimer         types.Int64  `tfsdk:"hold_timer"`
	KeepAliveTimer    types.Int64  `tfsdk:"keepalive_timer"`
	MED               types.Int64  `tfsdk:"med"`
	InboundMED        types.Int64  `tfsdk:"inbound_med"`
	LocalPreference   types.Int64  `tfsdk:"local_preference"`
	ASPrependCount    types.Int64  `tfsdk:"as_prepend_count"`
	NextHopSelf       types.Bool   `tfsdk:"next_hop_self"`
	DirectlyConnected types.Bool   `tfsdk:"directly_connected"`
	BFDEnabled        types.Bool   `tfsdk:"bfd_enabled"`
	EVPN              types.Bool   `tfsdk:"evpn"`
	Password          types.String `tfsdk:"password"`
}

// bgpVRFDSModel maps the BGP configuration of one VRF segment of an appliance.
type bgpVRFDSModel struct {
	VRFID     types.Int64          `tfsdk:"vrf_id"`
	VRFName   types.String         `tfsdk:"vrf_name"`
	System    bgpSystemDSModel     `tfsdk:"system"`
	Neighbors []bgpNeighborDSModel `tfsdk:"neighbors"`
}

// bgpApplianceDSModel maps one appliance with its per-VRF BGP configuration.
type bgpApplianceDSModel struct {
	NePk     types.String    `tfsdk:"ne_pk"`
	Hostname types.String    `tfsdk:"hostname"`
	Serial   types.String    `tfsdk:"serial"`
	Model    types.String    `tfsdk:"model"`
	Site     types.String    `tfsdk:"site"`
	VRFs     []bgpVRFDSModel `tfsdk:"vrfs"`
}

// bgpConfigDataSourceModel maps the data source configuration and computed
// state.
type bgpConfigDataSourceModel struct {
	NePk                 types.String          `tfsdk:"ne_pk"`
	Cached               types.Bool            `tfsdk:"cached"`
	Models               []types.String        `tfsdk:"models"`
	ExcludeModels        []types.String        `tfsdk:"exclude_models"`
	Sites                []types.String        `tfsdk:"sites"`
	ExcludeSites         []types.String        `tfsdk:"exclude_sites"`
	HostnameRegex        types.String          `tfsdk:"hostname_regex"`
	ExcludeHostnameRegex types.String          `tfsdk:"exclude_hostname_regex"`
	Appliances           []bgpApplianceDSModel `tfsdk:"appliances"`
}

type bgpConfigDataSource struct {
	client *client.Client
}

func NewBGPConfigDataSource() datasource.DataSource {
	return &bgpConfigDataSource{}
}

func (d *bgpConfigDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bgp_config"
}

func (d *bgpConfigDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches the BGP configuration of the appliances from the Aruba SD-WAN Orchestrator: " +
			"per appliance and VRF segment the system settings (local ASN, router ID, graceful restart, " +
			"OSPF redistribution) and the configured neighbors (peer IP, remote ASN, peer type, timers, " +
			"route policies, BFD). Appliances without BGP configuration are included with an empty VRF list.",
		Attributes: map[string]schema.Attribute{
			"ne_pk": schema.StringAttribute{
				Description: "Optional appliance primary key (e.g. \"3.NE\") to fetch a single appliance. " +
					"If omitted, all appliances are returned.",
				Optional: true,
			},
			"cached": schema.BoolAttribute{
				Description: "Whether to read BGP data from the Orchestrator database (true, default) or " +
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
				Description: "List of appliances with their per-VRF BGP configuration, sorted by hostname. " +
					"Appliances without BGP configuration carry an empty vrfs list.",
				Computed: true,
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
						"vrfs": schema.ListNestedAttribute{
							Description: "BGP configuration per VRF segment, sorted by VRF ID; empty if the " +
								"appliance has no BGP configuration.",
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"vrf_id": schema.Int64Attribute{
										Description: "VRF segment ID the configuration belongs to (0 = Default).",
										Computed:    true,
									},
									"vrf_name": schema.StringAttribute{
										Description: "Resolved VRF segment name (e.g. \"Default\"); empty if it cannot be resolved.",
										Computed:    true,
									},
									"system": schema.SingleNestedAttribute{
										Description: "BGP system (process) configuration of the VRF segment.",
										Computed:    true,
										Attributes: map[string]schema.Attribute{
											"enabled": schema.BoolAttribute{
												Description: "True if BGP is enabled in this VRF segment.",
												Computed:    true,
											},
											"asn": schema.Int64Attribute{
												Description: "Local autonomous system number (4-byte ASNs supported).",
												Computed:    true,
											},
											"router_id": schema.StringAttribute{
												Description: "BGP router ID.",
												Computed:    true,
											},
											"graceful_restart": schema.BoolAttribute{
												Description: "True if graceful restart is enabled.",
												Computed:    true,
											},
											"max_restart_time": schema.Int64Attribute{
												Description: "Maximum time in seconds to wait for a restarting peer before its " +
													"advertised routes are removed from the routing tables (1-3600).",
												Computed: true,
											},
											"stale_path_time": schema.Int64Attribute{
												Description: "Maximum time in seconds stale routes of a restarted peer are kept " +
													"before removal (1-3600).",
												Computed: true,
											},
											"redistribute_ospf": schema.BoolAttribute{
												Description: "True if BGP routes are redistributed to OSPF.",
												Computed:    true,
											},
											"redistribute_ospf_filter": schema.Int64Attribute{
												Description: "Filter bitmask applied to routes redistributed to OSPF.",
												Computed:    true,
											},
											"remote_as_path_advertise": schema.BoolAttribute{
												Description: "True if the remote AS path is propagated when advertising routes.",
												Computed:    true,
											},
										},
									},
									"neighbors": schema.ListNestedAttribute{
										Description: "BGP neighbors configured in the VRF segment, sorted by peer IP address; " +
											"empty if none are configured.",
										Computed: true,
										NestedObject: schema.NestedAttributeObject{
											Attributes: map[string]schema.Attribute{
												"ip": schema.StringAttribute{
													Description: "IP address of the neighbor.",
													Computed:    true,
												},
												"remote_as": schema.Int64Attribute{
													Description: "Remote autonomous system number of the neighbor (4-byte ASNs supported).",
													Computed:    true,
												},
												"type": schema.StringAttribute{
													Description: "Peer type of the neighbor (e.g. \"Branch\", \"Branch-transit\", \"PE-router\").",
													Computed:    true,
												},
												"enabled": schema.BoolAttribute{
													Description: "True if the BGP session to this neighbor is enabled.",
													Computed:    true,
												},
												"import_routes": schema.BoolAttribute{
													Description: "True if routes learned from the neighbor are imported.",
													Computed:    true,
												},
												"export_map": schema.Int64Attribute{
													Description: "Route export policies bitmask; 4294967295 means the predefined " +
														"bitmask of the peer type is unchanged.",
													Computed: true,
												},
												"hold_timer": schema.Int64Attribute{
													Description: "Hold timer in seconds: how long to wait for a KEEPALIVE or UPDATE " +
														"message before the neighbor is considered dead.",
													Computed: true,
												},
												"keepalive_timer": schema.Int64Attribute{
													Description: "Interval in seconds between KEEPALIVE messages to the neighbor.",
													Computed:    true,
												},
												"med": schema.Int64Attribute{
													Description: "Multi-Exit Discriminator for routes advertised to the neighbor.",
													Computed:    true,
												},
												"inbound_med": schema.Int64Attribute{
													Description: "Metric applied to routes received from the neighbor.",
													Computed:    true,
												},
												"local_preference": schema.Int64Attribute{
													Description: "Local preference for routes advertised to the neighbor.",
													Computed:    true,
												},
												"as_prepend_count": schema.Int64Attribute{
													Description: "Number of additional times the local AS is prepended to the AS path.",
													Computed:    true,
												},
												"next_hop_self": schema.BoolAttribute{
													Description: "True if the appliance advertises its own IP address as next hop.",
													Computed:    true,
												},
												"directly_connected": schema.BoolAttribute{
													Description: "True if the peer adjacency is treated as single hop, false for multi hop.",
													Computed:    true,
												},
												"bfd_enabled": schema.BoolAttribute{
													Description: "True if a BFD session is desired for this peer.",
													Computed:    true,
												},
												"evpn": schema.BoolAttribute{
													Description: "True if EVPN is enabled for this peer.",
													Computed:    true,
												},
												"password": schema.StringAttribute{
													Description: "MD5 password of the BGP session; may be empty or masked by the Orchestrator.",
													Computed:    true,
													Sensitive:   true,
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
		},
	}
}

func (d *bgpConfigDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *bgpConfigDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config bgpConfigDataSourceModel
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

	appliances, warnings, err := d.client.GetApplianceBGP(filter, cached)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read BGP configuration",
			"Error reading BGP data from the Orchestrator: "+err.Error(),
		)
		return
	}
	for _, w := range warnings {
		resp.Diagnostics.AddWarning("Incomplete BGP data", w)
	}

	state := config
	state.Appliances = make([]bgpApplianceDSModel, 0, len(appliances))
	for _, appliance := range appliances {
		vrfs := make([]bgpVRFDSModel, 0, len(appliance.VRFs))
		for _, vrf := range appliance.VRFs {
			neighbors := make([]bgpNeighborDSModel, 0, len(vrf.Neighbors))
			for _, neighbor := range vrf.Neighbors {
				neighbors = append(neighbors, bgpNeighborDSModel{
					IP:                types.StringValue(neighbor.IP),
					RemoteAS:          types.Int64Value(neighbor.RemoteAS),
					Type:              types.StringValue(neighbor.Type),
					Enabled:           types.BoolValue(neighbor.Enabled),
					ImportRoutes:      types.BoolValue(neighbor.ImportRoutes),
					ExportMap:         types.Int64Value(neighbor.ExportMap),
					HoldTimer:         types.Int64Value(int64(neighbor.HoldTimer)),
					KeepAliveTimer:    types.Int64Value(int64(neighbor.KeepAliveTimer)),
					MED:               types.Int64Value(int64(neighbor.MED)),
					InboundMED:        types.Int64Value(int64(neighbor.InboundMED)),
					LocalPreference:   types.Int64Value(int64(neighbor.LocalPreference)),
					ASPrependCount:    types.Int64Value(int64(neighbor.ASPrependCount)),
					NextHopSelf:       types.BoolValue(neighbor.NextHopSelf),
					DirectlyConnected: types.BoolValue(neighbor.DirectlyConnected),
					BFDEnabled:        types.BoolValue(neighbor.BFDEnabled),
					EVPN:              types.BoolValue(neighbor.EVPN),
					Password:          types.StringValue(neighbor.Password),
				})
			}
			vrfs = append(vrfs, bgpVRFDSModel{
				VRFID:   types.Int64Value(int64(vrf.VRFID)),
				VRFName: types.StringValue(vrf.VRFName),
				System: bgpSystemDSModel{
					Enabled:               types.BoolValue(vrf.System.Enabled),
					ASN:                   types.Int64Value(vrf.System.ASN),
					RouterID:              types.StringValue(vrf.System.RouterID),
					GracefulRestart:       types.BoolValue(vrf.System.GracefulRestart),
					MaxRestartTime:        types.Int64Value(int64(vrf.System.MaxRestartTime)),
					StalePathTime:         types.Int64Value(int64(vrf.System.StalePathTime)),
					RedistOSPF:            types.BoolValue(vrf.System.RedistOSPF),
					RedistOSPFFilter:      types.Int64Value(int64(vrf.System.RedistOSPFFilter)),
					RemoteASPathAdvertise: types.BoolValue(vrf.System.RemoteASPathAdvertise),
				},
				Neighbors: neighbors,
			})
		}
		state.Appliances = append(state.Appliances, bgpApplianceDSModel{
			NePk:     types.StringValue(appliance.NePk),
			Hostname: types.StringValue(appliance.HostName),
			Serial:   types.StringValue(appliance.Serial),
			Model:    types.StringValue(appliance.Model),
			Site:     types.StringValue(appliance.Site),
			VRFs:     vrfs,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
