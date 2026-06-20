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
	_ datasource.DataSource              = &ipAddressGroupsDataSource{}
	_ datasource.DataSourceWithConfigure = &ipAddressGroupsDataSource{}
)

type ipAddressGroupRuleDSModel struct {
	IncludedIPs    []types.String `tfsdk:"included_ips"`
	ExcludedIPs    []types.String `tfsdk:"excluded_ips"`
	IncludedGroups []types.String `tfsdk:"included_groups"`
	Comment        types.String   `tfsdk:"comment"`
}

type ipAddressGroupDSModel struct {
	Name  types.String                `tfsdk:"name"`
	Rules []ipAddressGroupRuleDSModel `tfsdk:"rules"`
}

type ipAddressGroupsDataSourceModel struct {
	AddressGroups []ipAddressGroupDSModel `tfsdk:"address_groups"`
}

type ipAddressGroupsDataSource struct {
	client *client.Client
}

func NewIPAddressGroupsDataSource() datasource.DataSource {
	return &ipAddressGroupsDataSource{}
}

func (d *ipAddressGroupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_address_groups"
}

func (d *ipAddressGroupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches all IP address groups from the Aruba SD-WAN Orchestrator " +
			"via GET /gms/rest/ipObjects/addressGroup.",
		Attributes: map[string]schema.Attribute{
			"address_groups": schema.ListNestedAttribute{
				Description: "List of address groups.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "Name of the address group.",
							Computed:    true,
						},
						"rules": schema.ListNestedAttribute{
							Description: "Rules composing this group.",
							Computed:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"included_ips": schema.ListAttribute{
										Description: "Included IPs/CIDRs.",
										Computed:    true,
										ElementType: types.StringType,
									},
									"excluded_ips": schema.ListAttribute{
										Description: "Excluded IPs/CIDRs.",
										Computed:    true,
										ElementType: types.StringType,
									},
									"included_groups": schema.ListAttribute{
										Description: "Names of nested address groups.",
										Computed:    true,
										ElementType: types.StringType,
									},
									"comment": schema.StringAttribute{
										Description: "Free-form comment.",
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

func (d *ipAddressGroupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ipAddressGroupsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	groups, err := d.client.GetIPAddressGroups()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read IP address groups",
			"Error calling GET /gms/rest/ipObjects/addressGroup: "+err.Error(),
		)
		return
	}

	state := ipAddressGroupsDataSourceModel{
		AddressGroups: make([]ipAddressGroupDSModel, 0, len(groups)),
	}
	for _, g := range groups {
		rules := make([]ipAddressGroupRuleDSModel, 0, len(g.Rules))
		for _, r := range g.Rules {
			rules = append(rules, ipAddressGroupRuleDSModel{
				IncludedIPs:    stringSliceToTF(r.IncludedIPs),
				ExcludedIPs:    stringSliceToTF(r.ExcludedIPs),
				IncludedGroups: stringSliceToTF(r.IncludedGroups),
				Comment:        types.StringValue(r.Comment),
			})
		}
		state.AddressGroups = append(state.AddressGroups, ipAddressGroupDSModel{
			Name:  types.StringValue(g.Name),
			Rules: rules,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
