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
	_ datasource.DataSource              = &applicationGroupsDataSource{}
	_ datasource.DataSourceWithConfigure = &applicationGroupsDataSource{}
)

// applicationGroupDSModel maps a single application group to the data source schema.
type applicationGroupDSModel struct {
	Name types.String `tfsdk:"name"`
	Apps types.List   `tfsdk:"apps"`
}

// applicationGroupsDataSourceModel maps the data source schema data.
type applicationGroupsDataSourceModel struct {
	ApplicationGroups []applicationGroupDSModel `tfsdk:"application_groups"`
}

// applicationGroupsDataSource is the data source implementation.
type applicationGroupsDataSource struct {
	client *client.Client
}

// NewApplicationGroupsDataSource returns a new data source instance.
func NewApplicationGroupsDataSource() datasource.DataSource {
	return &applicationGroupsDataSource{}
}

// Metadata returns the data source type name.
func (d *applicationGroupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_groups"
}

// Schema defines the schema for the data source.
func (d *applicationGroupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches the list of user-defined application groups (tags) from the Aruba SD-WAN Orchestrator " +
			"via GET /gms/rest/applicationDefinition/applicationTags?resourceKey=userDefined.",
		Attributes: map[string]schema.Attribute{
			"application_groups": schema.ListNestedAttribute{
				Description: "List of application groups configured on the Orchestrator.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "The name of the application group.",
							Computed:    true,
						},
						"apps": schema.ListAttribute{
							Description: "List of application names in this group.",
							Computed:    true,
							ElementType: types.StringType,
						},
					},
				},
			},
		},
	}
}

// Configure adds the provider configured client to the data source.
func (d *applicationGroupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *applicationGroupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state applicationGroupsDataSourceModel

	groups, err := d.client.GetApplicationGroups()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read application groups",
			"Error calling GET /gms/rest/applicationDefinition/applicationTags: "+err.Error(),
		)
		return
	}

	for _, group := range groups {
		appsList, diags := types.ListValueFrom(ctx, types.StringType, group.Apps)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		state.ApplicationGroups = append(state.ApplicationGroups, applicationGroupDSModel{
			Name: types.StringValue(group.Name),
			Apps: appsList,
		})
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
