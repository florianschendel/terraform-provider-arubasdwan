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
	_ datasource.DataSource              = &vrrpInstancesDataSource{}
	_ datasource.DataSourceWithConfigure = &vrrpInstancesDataSource{}
)

// vrrpInstanceDSModel maps one VRRP instance of an appliance.
type vrrpInstanceDSModel struct {
	GroupID           types.Int64  `tfsdk:"group_id"`
	Interface         types.String `tfsdk:"interface"`
	VirtualIP         types.String `tfsdk:"virtual_ip"`
	Priority          types.Int64  `tfsdk:"priority"`
	Enabled           types.Bool   `tfsdk:"enabled"`
	Preempt           types.Bool   `tfsdk:"preempt"`
	Holddown          types.Int64  `tfsdk:"holddown"`
	AdvTimer          types.Int64  `tfsdk:"advertisement_timer"`
	Description       types.String `tfsdk:"description"`
	Auth              types.String `tfsdk:"auth"`
	State             types.String `tfsdk:"state"`
	MasterIP          types.String `tfsdk:"master_ip"`
	MasterTransitions types.Int64  `tfsdk:"master_transitions"`
	Uptime            types.String `tfsdk:"uptime"`
	VirtualMAC        types.String `tfsdk:"virtual_mac"`
	VIPOwner          types.Bool   `tfsdk:"vip_owner"`
	PacketTrace       types.Bool   `tfsdk:"packet_trace"`
}

// vrrpApplianceDSModel maps one appliance with its VRRP instances.
type vrrpApplianceDSModel struct {
	NePk          types.String          `tfsdk:"ne_pk"`
	Hostname      types.String          `tfsdk:"hostname"`
	Serial        types.String          `tfsdk:"serial"`
	Model         types.String          `tfsdk:"model"`
	Site          types.String          `tfsdk:"site"`
	VRRPInstances []vrrpInstanceDSModel `tfsdk:"vrrp_instances"`
}

// vrrpInstancesDataSourceModel maps the data source configuration and
// computed state.
type vrrpInstancesDataSourceModel struct {
	NePk                 types.String           `tfsdk:"ne_pk"`
	Cached               types.Bool             `tfsdk:"cached"`
	Models               []types.String         `tfsdk:"models"`
	ExcludeModels        []types.String         `tfsdk:"exclude_models"`
	Sites                []types.String         `tfsdk:"sites"`
	ExcludeSites         []types.String         `tfsdk:"exclude_sites"`
	HostnameRegex        types.String           `tfsdk:"hostname_regex"`
	ExcludeHostnameRegex types.String           `tfsdk:"exclude_hostname_regex"`
	Appliances           []vrrpApplianceDSModel `tfsdk:"appliances"`
}

type vrrpInstancesDataSource struct {
	client *client.Client
}

func NewVRRPInstancesDataSource() datasource.DataSource {
	return &vrrpInstancesDataSource{}
}

func (d *vrrpInstancesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vrrp_instances"
}

func (d *vrrpInstancesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches the VRRP instances configured on the appliances from the Aruba SD-WAN Orchestrator: " +
			"per appliance the group ID, peering interface, virtual IP address, priority, preemption, and timers, " +
			"plus the operational state (Master/Backup/Init, current master IP, transitions, uptime, virtual MAC). " +
			"Appliances without VRRP configuration are included with an empty instance list.",
		Attributes: map[string]schema.Attribute{
			"ne_pk": schema.StringAttribute{
				Description: "Optional appliance primary key (e.g. \"3.NE\") to fetch a single appliance. " +
					"If omitted, all appliances are returned.",
				Optional: true,
			},
			"cached": schema.BoolAttribute{
				Description: "Whether to read VRRP data from the Orchestrator database (true, default) or " +
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
				Description: "List of appliances with their VRRP instances, sorted by hostname. Appliances " +
					"without VRRP configuration carry an empty vrrp_instances list.",
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
						"vrrp_instances": schema.ListNestedAttribute{
							Description: "VRRP instances configured on the appliance, sorted by interface and group ID; " +
								"empty if none are configured.",
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"group_id": schema.Int64Attribute{
										Description: "VRRP group ID (VRID) shared by the two peers of the group (1-255).",
										Computed:    true,
									},
									"interface": schema.StringAttribute{
										Description: "Interface the VRRP instance is peering on (e.g. \"lan0\").",
										Computed:    true,
									},
									"virtual_ip": schema.StringAttribute{
										Description: "Virtual IP address of the VRRP group.",
										Computed:    true,
									},
									"priority": schema.Int64Attribute{
										Description: "VRRP priority (1-254); the peer with the higher priority is the VRRP master.",
										Computed:    true,
									},
									"enabled": schema.BoolAttribute{
										Description: "True if the VRRP instance is administratively up.",
										Computed:    true,
									},
									"preempt": schema.BoolAttribute{
										Description: "True if the higher-priority peer takes over the master role again " +
											"when it comes back online.",
										Computed: true,
									},
									"holddown": schema.Int64Attribute{
										Description: "Holddown timer in seconds.",
										Computed:    true,
									},
									"advertisement_timer": schema.Int64Attribute{
										Description: "Time interval between VRRP advertisements in seconds.",
										Computed:    true,
									},
									"description": schema.StringAttribute{
										Description: "Description string of the VRRP instance.",
										Computed:    true,
									},
									"auth": schema.StringAttribute{
										Description: "VRRP authentication string; may be empty or masked by the Orchestrator.",
										Computed:    true,
										Sensitive:   true,
									},
									"state": schema.StringAttribute{
										Description: "Operational state of the instance: \"Master\", \"Backup\", or \"Init\" " +
											"(initializing, disabled, or interface down).",
										Computed: true,
									},
									"master_ip": schema.StringAttribute{
										Description: "Interface or local IP address of the current VRRP master.",
										Computed:    true,
									},
									"master_transitions": schema.Int64Attribute{
										Description: "Number of Master/Backup transitions of the instance; a high number " +
											"indicates a problematic VRRP configuration or environment.",
										Computed: true,
									},
									"uptime": schema.StringAttribute{
										Description: "Time elapsed since the instance entered its current state " +
											"(e.g. \"0 days 11 hrs 49 mins 41 secs\").",
										Computed: true,
									},
									"virtual_mac": schema.StringAttribute{
										Description: "MAC address the VRRP instance is using (00-00-5E-00-01-{VRID} on " +
											"hardware appliances, the interface MAC on virtual appliances).",
										Computed: true,
									},
									"vip_owner": schema.BoolAttribute{
										Description: "True if the appliance owns the virtual IP address; always false on " +
											"EdgeConnect appliances, which cannot use one of their own IP addresses as the VRRP IP.",
										Computed: true,
									},
									"packet_trace": schema.BoolAttribute{
										Description: "True if VRRP packet tracing is enabled on the instance.",
										Computed:    true,
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

func (d *vrrpInstancesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *vrrpInstancesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config vrrpInstancesDataSourceModel
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

	appliances, err := d.client.GetApplianceVRRP(filter, cached)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read VRRP instances",
			"Error reading VRRP data from the Orchestrator: "+err.Error(),
		)
		return
	}

	state := config
	state.Appliances = make([]vrrpApplianceDSModel, 0, len(appliances))
	for _, appliance := range appliances {
		instances := make([]vrrpInstanceDSModel, 0, len(appliance.Instances))
		for _, instance := range appliance.Instances {
			instances = append(instances, vrrpInstanceDSModel{
				GroupID:           types.Int64Value(int64(instance.GroupID)),
				Interface:         types.StringValue(instance.Interface),
				VirtualIP:         types.StringValue(instance.VirtualIP),
				Priority:          types.Int64Value(int64(instance.Priority)),
				Enabled:           types.BoolValue(instance.Enabled),
				Preempt:           types.BoolValue(instance.Preempt),
				Holddown:          types.Int64Value(int64(instance.Holddown)),
				AdvTimer:          types.Int64Value(int64(instance.AdvTimer)),
				Description:       types.StringValue(instance.Description),
				Auth:              types.StringValue(instance.Auth),
				State:             types.StringValue(instance.State),
				MasterIP:          types.StringValue(instance.MasterIP),
				MasterTransitions: types.Int64Value(int64(instance.MasterTransitions)),
				Uptime:            types.StringValue(instance.Uptime),
				VirtualMAC:        types.StringValue(instance.VirtualMAC),
				VIPOwner:          types.BoolValue(instance.VIPOwner),
				PacketTrace:       types.BoolValue(instance.PacketTrace),
			})
		}
		state.Appliances = append(state.Appliances, vrrpApplianceDSModel{
			NePk:          types.StringValue(appliance.NePk),
			Hostname:      types.StringValue(appliance.HostName),
			Serial:        types.StringValue(appliance.Serial),
			Model:         types.StringValue(appliance.Model),
			Site:          types.StringValue(appliance.Site),
			VRRPInstances: instances,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
