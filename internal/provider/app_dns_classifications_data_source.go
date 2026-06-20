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
	_ datasource.DataSource              = &appDNSClassificationsDataSource{}
	_ datasource.DataSourceWithConfigure = &appDNSClassificationsDataSource{}
)

type appDNSClassificationDSModel struct {
	Name        types.String `tfsdk:"name"`
	Domain      types.String `tfsdk:"domain"`
	Description types.String `tfsdk:"description"`
	Priority    types.Int64  `tfsdk:"priority"`
	Disabled    types.Bool   `tfsdk:"disabled"`
}

type appDNSClassificationsDataSourceModel struct {
	DNSClassifications []appDNSClassificationDSModel `tfsdk:"dns_classifications"`
}

type appDNSClassificationsDataSource struct {
	client *client.Client
}

func NewAppDNSClassificationsDataSource() datasource.DataSource {
	return &appDNSClassificationsDataSource{}
}

func (d *appDNSClassificationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_dns_classifications"
}

func (d *appDNSClassificationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches user-defined DNS classification application definitions from the Aruba SD-WAN Orchestrator " +
			"via GET /gms/rest/applicationDefinition?base=dnsClassification&resourceKey=userDefined.",
		Attributes: map[string]schema.Attribute{
			"dns_classifications": schema.ListNestedAttribute{
				Description: "List of DNS classification application definitions.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "The name of the application.",
							Computed:    true,
						},
						"domain": schema.StringAttribute{
							Description: "The DNS domain pattern (e.g. \"*.example.com\").",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "The description of the application.",
							Computed:    true,
						},
						"priority": schema.Int64Attribute{
							Description: "The confidence/priority of the classification.",
							Computed:    true,
						},
						"disabled": schema.BoolAttribute{
							Description: "Whether the classification is disabled.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *appDNSClassificationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *appDNSClassificationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	defs, err := d.client.GetDNSClassifications()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read DNS classifications",
			"Error calling GET /gms/rest/applicationDefinition: "+err.Error(),
		)
		return
	}

	state := appDNSClassificationsDataSourceModel{
		DNSClassifications: make([]appDNSClassificationDSModel, 0, len(defs)),
	}
	for _, def := range defs {
		state.DNSClassifications = append(state.DNSClassifications, appDNSClassificationDSModel{
			Name:        types.StringValue(def.Name),
			Domain:      types.StringValue(def.Domain),
			Description: types.StringValue(def.Description),
			Priority:    types.Int64Value(int64(def.Priority)),
			Disabled:    types.BoolValue(def.Disabled),
		})
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
