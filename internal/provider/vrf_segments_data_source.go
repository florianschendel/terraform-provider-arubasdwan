package provider

import (
	"context"
	"fmt"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface checks for the VRF segments data source.
var (
	_ datasource.DataSource              = &vrfSegmentsDataSource{}
	_ datasource.DataSourceWithConfigure = &vrfSegmentsDataSource{}
)

// vrfSegmentModel maps a single VRF segment to the data source schema.
type vrfSegmentModel struct {
	ID      types.Int64  `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	Status  types.Int64  `tfsdk:"status"`
	Comment types.String `tfsdk:"comment"`
}

// vrfZoneMappingModel maps a zone-to-VRF assignment. Each zone has a different
// numeric ID in each VRF, so this mapping is essential for cross-VRF policy
// configuration.
type vrfZoneMappingModel struct {
	ZoneID   types.Int64  `tfsdk:"zone_id"`   // Zone ID specific to this VRF
	ZoneName types.String `tfsdk:"zone_name"` // Zone name (same across all VRFs)
	VRFID    types.Int64  `tfsdk:"vrf_id"`    // VRF segment ID
	VRFName  types.String `tfsdk:"vrf_name"`  // VRF segment name
}

// vrfSegmentsDataSourceModel maps the data source schema. It provides:
//   - segments:      All VRF segments configured on the Orchestrator.
//   - zone_mappings: All zone-to-VRF assignments (zone IDs per VRF).
//   - segment_pair:  Optionally computed from source_vrf + dest_vrf names.
//
// Usage in Terraform:
//
//	data "arubasdwan_vrf_segments" "vrf" {
//	  source_vrf = "Default"   # Optional: resolve VRF names to a segment pair
//	  dest_vrf   = "Guest"
//	}
//	# Use data.arubasdwan_vrf_segments.vrf.segment_pair in security policy resources.
type vrfSegmentsDataSourceModel struct {
	Segments     []vrfSegmentModel     `tfsdk:"segments"`
	ZoneMappings []vrfZoneMappingModel `tfsdk:"zone_mappings"`
	SegmentPair  types.String          `tfsdk:"segment_pair"`
	SourceVRF    types.String          `tfsdk:"source_vrf"`
	DestVRF      types.String          `tfsdk:"dest_vrf"`
}

type vrfSegmentsDataSource struct {
	client *client.Client
}

func NewVRFSegmentsDataSource() datasource.DataSource {
	return &vrfSegmentsDataSource{}
}

func (d *vrfSegmentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vrf_segments"
}

func (d *vrfSegmentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches VRF segments and zone-to-VRF mappings from the Orchestrator. " +
			"Optionally resolves a segment_pair from VRF names. " +
			"Use zone_mappings to look up the VRF-specific zone ID for security policies.",
		Attributes: map[string]schema.Attribute{
			"source_vrf": schema.StringAttribute{
				Description: "Source VRF name to resolve into a segment pair. Optional.",
				Optional:    true,
			},
			"dest_vrf": schema.StringAttribute{
				Description: "Destination VRF name to resolve into a segment pair. Optional.",
				Optional:    true,
			},
			"segment_pair": schema.StringAttribute{
				Description: "Resolved segment pair (e.g. \"0_1\"). Computed from source_vrf and dest_vrf.",
				Computed:    true,
			},
			"segments": schema.ListNestedAttribute{
				Description: "List of all VRF segments.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":      schema.Int64Attribute{Computed: true, Description: "Segment ID."},
						"name":    schema.StringAttribute{Computed: true, Description: "Segment name."},
						"status":  schema.Int64Attribute{Computed: true, Description: "Segment status."},
						"comment": schema.StringAttribute{Computed: true, Description: "Segment comment."},
					},
				},
			},
			"zone_mappings": schema.ListNestedAttribute{
				Description: "Zone-to-VRF assignments. Each zone has a unique ID per VRF. " +
					"Use this to find the correct zone_id for a given zone name and VRF.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"zone_id":   schema.Int64Attribute{Computed: true, Description: "Zone ID (unique per VRF)."},
						"zone_name": schema.StringAttribute{Computed: true, Description: "Zone name."},
						"vrf_id":    schema.Int64Attribute{Computed: true, Description: "VRF segment ID."},
						"vrf_name":  schema.StringAttribute{Computed: true, Description: "VRF segment name."},
					},
				},
			},
		},
	}
}

func (d *vrfSegmentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData))
		return
	}
	d.client = apiClient
}

// Read fetches all VRF segments and zone-to-VRF mappings from the Orchestrator.
// If both source_vrf and dest_vrf are provided, it resolves them to a segment_pair
// string (e.g. "0_1") that can be used directly in security policy resources.
func (d *vrfSegmentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config vrfSegmentsDataSourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	segments, err := d.client.GetVRFSegments()
	if err != nil {
		resp.Diagnostics.AddError("Unable to read VRF segments", err.Error())
		return
	}

	mappings, err := d.client.GetVRFZoneMappings()
	if err != nil {
		resp.Diagnostics.AddError("Unable to read VRF zone mappings", err.Error())
		return
	}

	state := vrfSegmentsDataSourceModel{
		SourceVRF: config.SourceVRF,
		DestVRF:   config.DestVRF,
	}

	// Populate segments.
	nameToID := make(map[string]int, len(segments))
	for _, seg := range segments {
		nameToID[seg.Name] = seg.ID
		state.Segments = append(state.Segments, vrfSegmentModel{
			ID:      types.Int64Value(int64(seg.ID)),
			Name:    types.StringValue(seg.Name),
			Status:  types.Int64Value(int64(seg.Status)),
			Comment: types.StringValue(seg.Comment),
		})
	}

	// Populate zone mappings.
	for _, m := range mappings {
		state.ZoneMappings = append(state.ZoneMappings, vrfZoneMappingModel{
			ZoneID:   types.Int64Value(int64(m.ZoneID)),
			ZoneName: types.StringValue(m.ZoneName),
			VRFID:    types.Int64Value(int64(m.VRFID)),
			VRFName:  types.StringValue(m.VRFName),
		})
	}

	// Resolve segment_pair from VRF names if both are provided.
	if !config.SourceVRF.IsNull() && !config.DestVRF.IsNull() {
		srcName := config.SourceVRF.ValueString()
		dstName := config.DestVRF.ValueString()

		srcID, srcOK := nameToID[srcName]
		if !srcOK {
			resp.Diagnostics.AddError("Unknown source VRF",
				fmt.Sprintf("VRF %q not found. Available: %v", srcName, vrfNames(segments)))
			return
		}
		dstID, dstOK := nameToID[dstName]
		if !dstOK {
			resp.Diagnostics.AddError("Unknown destination VRF",
				fmt.Sprintf("VRF %q not found. Available: %v", dstName, vrfNames(segments)))
			return
		}

		state.SegmentPair = types.StringValue(fmt.Sprintf("%d_%d", srcID, dstID))
	} else {
		state.SegmentPair = types.StringValue("")
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// vrfNames extracts the VRF segment names into a string slice for use in
// error messages when a VRF name lookup fails.
func vrfNames(segments []client.VRFSegment) []string {
	names := make([]string, len(segments))
	for i, s := range segments {
		names[i] = s.Name
	}
	return names
}
