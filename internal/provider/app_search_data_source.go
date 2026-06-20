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
	_ datasource.DataSource              = &appSearchDataSource{}
	_ datasource.DataSourceWithConfigure = &appSearchDataSource{}
)

type appSearchDataSourceModel struct {
	Pattern      types.String `tfsdk:"pattern"`
	Limit        types.Int64  `tfsdk:"limit"`
	Applications types.List   `tfsdk:"applications"`
}

type appSearchDataSource struct {
	client *client.Client
}

func NewAppSearchDataSource() datasource.DataSource {
	return &appSearchDataSource{}
}

func (d *appSearchDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_search"
}

func (d *appSearchDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Performs a server-side wildcard search across all application definitions on the Orchestrator " +
			"(built-in + user-defined) via POST /gms/rest/applicationDefinition/applications/wildcard. " +
			"Returns the list of matching application names, which can be referenced in security policies.",
		Attributes: map[string]schema.Attribute{
			"pattern": schema.StringAttribute{
				Description: "Substring pattern to search for (case-insensitive on the Orchestrator side).",
				Required:    true,
			},
			"limit": schema.Int64Attribute{
				Description: "Maximum number of results to return. 0 means no limit. Defaults to 0.",
				Optional:    true,
			},
			"applications": schema.ListAttribute{
				Description: "List of matching application names.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (d *appSearchDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *appSearchDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg appSearchDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	limit := 0
	if !cfg.Limit.IsNull() && !cfg.Limit.IsUnknown() {
		limit = int(cfg.Limit.ValueInt64())
	}

	names, err := d.client.SearchApplications(cfg.Pattern.ValueString(), limit)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to search applications",
			"Error calling POST /gms/rest/applicationDefinition/applications/wildcard: "+err.Error(),
		)
		return
	}

	apps, diags := types.ListValueFrom(ctx, types.StringType, names)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := appSearchDataSourceModel{
		Pattern:      cfg.Pattern,
		Limit:        cfg.Limit,
		Applications: apps,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
