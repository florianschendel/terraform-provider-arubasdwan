package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &appCompoundClassificationsDataSource{}
	_ datasource.DataSourceWithConfigure = &appCompoundClassificationsDataSource{}
)

type appCompoundClassificationDSModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Confidence    types.Int64  `tfsdk:"confidence"`
	Disabled      types.Bool   `tfsdk:"disabled"`
	Protocol      types.String `tfsdk:"protocol"`
	SrcIP         types.String `tfsdk:"src_ip"`
	DstIP         types.String `tfsdk:"dst_ip"`
	EitherIP      types.String `tfsdk:"either_ip"`
	SrcPort       types.String `tfsdk:"src_port"`
	DstPort       types.String `tfsdk:"dst_port"`
	EitherPort    types.String `tfsdk:"either_port"`
	SrcDNS        types.String `tfsdk:"src_dns"`
	DstDNS        types.String `tfsdk:"dst_dns"`
	EitherDNS     types.String `tfsdk:"either_dns"`
	SrcGeo        types.String `tfsdk:"src_geo"`
	DstGeo        types.String `tfsdk:"dst_geo"`
	EitherGeo     types.String `tfsdk:"either_geo"`
	SrcService    types.String `tfsdk:"src_service"`
	DstService    types.String `tfsdk:"dst_service"`
	EitherService types.String `tfsdk:"either_service"`
	DSCP          types.String `tfsdk:"dscp"`
	VLAN          types.String `tfsdk:"vlan"`
}

type appCompoundClassificationsDataSourceModel struct {
	CompoundClassifications []appCompoundClassificationDSModel `tfsdk:"compound_classifications"`
}

type appCompoundClassificationsDataSource struct {
	client *client.Client
}

func NewAppCompoundClassificationsDataSource() datasource.DataSource {
	return &appCompoundClassificationsDataSource{}
}

func (d *appCompoundClassificationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app_compound_classifications"
}

func (d *appCompoundClassificationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches user-defined compound classification application definitions from the Aruba SD-WAN Orchestrator " +
			"via GET /gms/rest/applicationDefinition?base=compoundClassification&resourceKey=userDefined.",
		Attributes: map[string]schema.Attribute{
			"compound_classifications": schema.ListNestedAttribute{
				Description: "List of compound classification application definitions.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The numeric ID assigned by the Orchestrator (as a string).",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The name of the application.",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "The description of the application.",
							Computed:    true,
						},
						"confidence": schema.Int64Attribute{
							Description: "The confidence level (0-100).",
							Computed:    true,
						},
						"disabled": schema.BoolAttribute{
							Description: "Whether the classification is disabled.",
							Computed:    true,
						},
						"protocol":       schema.StringAttribute{Description: "Protocol match (e.g. \"tcp\", \"udp\").", Computed: true},
						"src_ip":         schema.StringAttribute{Description: "Source IP match.", Computed: true},
						"dst_ip":         schema.StringAttribute{Description: "Destination IP match.", Computed: true},
						"either_ip":      schema.StringAttribute{Description: "Either direction IP match.", Computed: true},
						"src_port":       schema.StringAttribute{Description: "Source port match.", Computed: true},
						"dst_port":       schema.StringAttribute{Description: "Destination port match.", Computed: true},
						"either_port":    schema.StringAttribute{Description: "Either direction port match.", Computed: true},
						"src_dns":        schema.StringAttribute{Description: "Source DNS match.", Computed: true},
						"dst_dns":        schema.StringAttribute{Description: "Destination DNS match.", Computed: true},
						"either_dns":     schema.StringAttribute{Description: "Either direction DNS match.", Computed: true},
						"src_geo":        schema.StringAttribute{Description: "Source geolocation match.", Computed: true},
						"dst_geo":        schema.StringAttribute{Description: "Destination geolocation match.", Computed: true},
						"either_geo":     schema.StringAttribute{Description: "Either direction geolocation match.", Computed: true},
						"src_service":    schema.StringAttribute{Description: "Source service match.", Computed: true},
						"dst_service":    schema.StringAttribute{Description: "Destination service match.", Computed: true},
						"either_service": schema.StringAttribute{Description: "Either direction service match.", Computed: true},
						"dscp":           schema.StringAttribute{Description: "DSCP match.", Computed: true},
						"vlan":           schema.StringAttribute{Description: "VLAN match.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *appCompoundClassificationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *appCompoundClassificationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	defs, err := d.client.GetCompoundClassifications()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read compound classifications",
			"Error calling GET /gms/rest/applicationDefinition: "+err.Error(),
		)
		return
	}

	state := appCompoundClassificationsDataSourceModel{
		CompoundClassifications: make([]appCompoundClassificationDSModel, 0, len(defs)),
	}
	for _, def := range defs {
		state.CompoundClassifications = append(state.CompoundClassifications, appCompoundClassificationDSModel{
			ID:            types.StringValue(strconv.Itoa(def.ID)),
			Name:          types.StringValue(def.Name),
			Description:   types.StringValue(def.Description),
			Confidence:    types.Int64Value(int64(def.Confidence)),
			Disabled:      types.BoolValue(def.Disabled),
			Protocol:      types.StringValue(def.Protocol),
			SrcIP:         types.StringValue(def.SrcIP),
			DstIP:         types.StringValue(def.DstIP),
			EitherIP:      types.StringValue(def.EitherIP),
			SrcPort:       types.StringValue(def.SrcPort),
			DstPort:       types.StringValue(def.DstPort),
			EitherPort:    types.StringValue(def.EitherPort),
			SrcDNS:        types.StringValue(def.SrcDNS),
			DstDNS:        types.StringValue(def.DstDNS),
			EitherDNS:     types.StringValue(def.EitherDNS),
			SrcGeo:        types.StringValue(def.SrcGeo),
			DstGeo:        types.StringValue(def.DstGeo),
			EitherGeo:     types.StringValue(def.EitherGeo),
			SrcService:    types.StringValue(def.SrcService),
			DstService:    types.StringValue(def.DstService),
			EitherService: types.StringValue(def.EitherService),
			DSCP:          types.StringValue(def.DSCP),
			VLAN:          types.StringValue(def.VLAN),
		})
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
