package provider

import (
	"context"
	"fmt"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource              = &appPortProtocolsDataSource{}
	_ datasource.DataSourceWithConfigure = &appPortProtocolsDataSource{}
)

// appPortProtocolDSModel maps a single port/protocol classification to the data source schema.
type appPortProtocolDSModel struct {
	Name        types.String `tfsdk:"name"`
	Port        types.Int64  `tfsdk:"port"`
	Protocol    types.Int64  `tfsdk:"protocol"`
	Description types.String `tfsdk:"description"`
	Priority    types.Int64  `tfsdk:"priority"`
	Disabled    types.Bool   `tfsdk:"disabled"`
}

// appPortProtocolsDataSourceModel maps the data source schema data.
type appPortProtocolsDataSourceModel struct {
	PortProtocolClassifications []appPortProtocolDSModel `tfsdk:"port_protocol_classifications"`
}

// appPortProtocolsDataSource is the data source implementation.
type appPortProtocolsDataSource struct {
	client *client.Client
}

// NewAppPortProtocolsDataSource returns a new data source instance.
func NewAppPortProtocolsDataSource() datasource.DataSource {
	return &appPortProtocolsDataSource{}
}

// Metadata returns the data source type name.
func (d *appPortProtocolsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_port_protocols"
}

// Schema defines the schema for the data source.
func (d *appPortProtocolsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches the list of user-defined port/protocol classifications from the Aruba SD-WAN Orchestrator " +
			"via GET /gms/rest/applicationDefinition?base=portProtocolClassification&resourceKey=userDefined.",
		Attributes: map[string]schema.Attribute{
			"port_protocol_classifications": schema.ListNestedAttribute{
				Description: "List of port/protocol classifications configured on the Orchestrator.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "The name of the port/protocol classification.",
							Computed:    true,
						},
						"port": schema.Int64Attribute{
							Description: "The port number.",
							Computed:    true,
						},
						"protocol": schema.Int64Attribute{
							Description: "The protocol number (e.g. 6 for TCP, 17 for UDP).",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "The description of the port/protocol classification.",
							Computed:    true,
						},
						"priority": schema.Int64Attribute{
							Description: "The priority of the port/protocol classification.",
							Computed:    true,
						},
						"disabled": schema.BoolAttribute{
							Description: "Whether the port/protocol classification is disabled.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

// Configure adds the provider configured client to the data source.
func (d *appPortProtocolsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read refreshes the Terraform state with the latest data from the Orchestrator.
func (d *appPortProtocolsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state appPortProtocolsDataSourceModel

	defs, err := d.client.GetPortProtocolClassifications()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read port/protocol classifications",
			"Error calling GET /gms/rest/applicationDefinition: "+err.Error(),
		)
		return
	}

	for _, def := range defs {
		state.PortProtocolClassifications = append(state.PortProtocolClassifications, appPortProtocolDSModel{
			Name:        types.StringValue(def.Name),
			Port:        types.Int64Value(int64(def.Port)),
			Protocol:    types.Int64Value(int64(def.Protocol)),
			Description: types.StringValue(def.Description),
			Priority:    types.Int64Value(int64(def.Priority)),
			Disabled:    types.BoolValue(def.Disabled),
		})
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
