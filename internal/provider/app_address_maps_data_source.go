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
	_ datasource.DataSource              = &appAddressMapsDataSource{}
	_ datasource.DataSourceWithConfigure = &appAddressMapsDataSource{}
)

type appAddressMapDSModel struct {
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

type appAddressMapsDataSourceModel struct {
	AddressMaps []appAddressMapDSModel `tfsdk:"address_maps"`
}

type appAddressMapsDataSource struct {
	client *client.Client
}

func NewAppAddressMapsDataSource() datasource.DataSource {
	return &appAddressMapsDataSource{}
}

func (d *appAddressMapsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_address_maps"
}

func (d *appAddressMapsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches the user-defined address maps from the Aruba SD-WAN Orchestrator via " +
			"GET /gms/rest/applicationDefinition?base=ipIntelligenceClassification&resourceKey=userDefined. " +
			"An address map classifies an IPv4 address range as a named application, which security policies " +
			"and overlay ACLs can then match by name.",
		Attributes: map[string]schema.Attribute{
			"address_maps": schema.ListNestedAttribute{
				Description: "List of address maps, sorted by the start address of their range.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "Name of the application.",
							Computed:    true,
						},
						"ip_start": schema.StringAttribute{
							Description: "First IPv4 address of the range, in dotted notation.",
							Computed:    true,
						},
						"ip_end": schema.StringAttribute{
							Description: "Last IPv4 address of the range, in dotted notation. Equal to ip_start for a single host.",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "Description of the entry.",
							Computed:    true,
						},
						"country": schema.StringAttribute{
							Description: "Country name associated with the range.",
							Computed:    true,
						},
						"country_code": schema.StringAttribute{
							Description: "Two-letter ISO 3166-1 alpha-2 country code.",
							Computed:    true,
						},
						"org": schema.StringAttribute{
							Description: "Organization associated with the range.",
							Computed:    true,
						},
						"priority": schema.Int64Attribute{
							Description: "Classification priority; a higher value takes precedence over a lower one.",
							Computed:    true,
						},
						"service_id": schema.Int64Attribute{
							Description: "Service ID assigned by the Orchestrator.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *appAddressMapsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *appAddressMapsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	maps, err := d.client.GetAddressMaps()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read address maps",
			"Error calling GET /gms/rest/applicationDefinition?base=ipIntelligenceClassification: "+err.Error(),
		)
		return
	}

	state := appAddressMapsDataSourceModel{AddressMaps: make([]appAddressMapDSModel, 0, len(maps))}
	for _, def := range maps {
		state.AddressMaps = append(state.AddressMaps, appAddressMapDSModel{
			Name:        types.StringValue(def.Name),
			IPStart:     types.StringValue(def.IPStart),
			IPEnd:       types.StringValue(def.IPEnd),
			Description: types.StringValue(def.Description),
			Country:     types.StringValue(def.Country),
			CountryCode: types.StringValue(def.CountryCode),
			Org:         types.StringValue(def.Org),
			Priority:    types.Int64Value(int64(def.Priority)),
			ServiceID:   types.Int64Value(int64(def.ServiceID)),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
