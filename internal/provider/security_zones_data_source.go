package provider

import (
	"context"
	"fmt"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface checks: ensure securityZonesDataSource implements both
// the DataSource interface (for Read operations) and the DataSourceWithConfigure
// interface (to receive the configured API client).
var (
	_ datasource.DataSource              = &securityZonesDataSource{}
	_ datasource.DataSourceWithConfigure = &securityZonesDataSource{}
)

// securityZoneModel maps a single security zone to the data source schema.
// This is used as the element type in the securityZonesDataSourceModel list.
type securityZoneModel struct {
	ID   types.Int64  `tfsdk:"id"`   // The zone's unique numeric ID
	Name types.String `tfsdk:"name"` // The zone's human-readable name
}

// securityZonesDataSourceModel maps the data source schema. It contains a
// single list attribute that holds all security zones retrieved from the
// Orchestrator.
//
// Usage in Terraform:
//
//	data "arubasdwan_security_zones" "all" {}
//	output "zones" { value = data.arubasdwan_security_zones.all.security_zones }
type securityZonesDataSourceModel struct {
	SecurityZones []securityZoneModel `tfsdk:"security_zones"`
}

// securityZonesDataSource is the data source implementation. It holds a
// reference to the shared API client for making Orchestrator API calls.
type securityZonesDataSource struct {
	client *client.Client
}

// NewSecurityZonesDataSource returns a new data source instance.
func NewSecurityZonesDataSource() datasource.DataSource {
	return &securityZonesDataSource{}
}

// Metadata returns the data source type name.
func (d *securityZonesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_zones"
}

// Schema defines the schema for the data source.
func (d *securityZonesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches the list of security zones from the Aruba SD-WAN Orchestrator " +
			"via GET /gms/rest/zones.",
		Attributes: map[string]schema.Attribute{
			"security_zones": schema.ListNestedAttribute{
				Description: "List of security zones configured on the Orchestrator.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Description: "The unique identifier of the security zone.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The name of the security zone.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

// Configure adds the provider configured client to the data source.
func (d *securityZonesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches all security zones from the Orchestrator via GET /gms/rest/zones
// and populates the data source state. This is called every time Terraform
// evaluates the data source (during plan and apply).
func (d *securityZonesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state securityZonesDataSourceModel

	zones, err := d.client.GetZones()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read security zones",
			"Error calling GET /gms/rest/zones: "+err.Error(),
		)
		return
	}

	for _, zone := range zones {
		zoneState := securityZoneModel{
			ID:   types.Int64Value(int64(zone.ID)),
			Name: types.StringValue(zone.Name),
		}
		state.SecurityZones = append(state.SecurityZones, zoneState)
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
