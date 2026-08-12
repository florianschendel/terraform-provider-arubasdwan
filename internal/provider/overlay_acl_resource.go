package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &overlayACLResource{}
	_ resource.ResourceWithImportState = &overlayACLResource{}
	_ resource.ResourceWithModifyPlan  = &overlayACLResource{}
)

// optionalString maps an API string to a Terraform value, reporting an
// empty string as null so unset optional attributes stay unset.
func optionalString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// overlayACLEntryModel maps one ACL entry of the resource schema.
type overlayACLEntryModel struct {
	Sequence           types.Int64  `tfsdk:"sequence"`
	Permit             types.Bool   `tfsdk:"permit"`
	MatchAll           types.Bool   `tfsdk:"match_all"`
	Comment            types.String `tfsdk:"comment"`
	Application        types.String `tfsdk:"application"`
	AppGroup           types.String `tfsdk:"app_group"`
	SrcIP              types.String `tfsdk:"src_ip"`
	DstIP              types.String `tfsdk:"dst_ip"`
	EitherIP           types.String `tfsdk:"either_ip"`
	SrcPort            types.String `tfsdk:"src_port"`
	DstPort            types.String `tfsdk:"dst_port"`
	EitherPort         types.String `tfsdk:"either_port"`
	Protocol           types.String `tfsdk:"protocol"`
	DSCP               types.String `tfsdk:"dscp"`
	SrcDNS             types.String `tfsdk:"src_dns"`
	DstDNS             types.String `tfsdk:"dst_dns"`
	EitherDNS          types.String `tfsdk:"either_dns"`
	SrcService         types.String `tfsdk:"src_service"`
	DstService         types.String `tfsdk:"dst_service"`
	EitherService      types.String `tfsdk:"either_service"`
	SrcAddressGroup    types.String `tfsdk:"src_address_group"`
	DstAddressGroup    types.String `tfsdk:"dst_address_group"`
	EitherAddressGroup types.String `tfsdk:"either_address_group"`
	SrcVRF             types.String `tfsdk:"src_vrf"`
	DstVRF             types.String `tfsdk:"dst_vrf"`
}

// overlayACLResourceModel maps the resource schema data.
type overlayACLResourceModel struct {
	ID                types.String           `tfsdk:"id"`
	OverlayName       types.String           `tfsdk:"overlay_name"`
	OverlayID         types.Int64            `tfsdk:"overlay_id"`
	ACLName           types.String           `tfsdk:"acl_name"`
	AllowEntryRemoval types.Bool             `tfsdk:"allow_entry_removal"`
	Entries           []overlayACLEntryModel `tfsdk:"entries"`
}

type overlayACLResource struct {
	client *client.Client
}

func NewOverlayACLResource() resource.Resource {
	return &overlayACLResource{}
}

func (r *overlayACLResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_overlay_acl"
}

func (r *overlayACLResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the ACL built into a Business Intent Overlay (BIO), which selects the traffic the " +
			"overlay carries. Entries match applications, application groups, IP addresses, ports, protocols, " +
			"DSCP markings, DNS names, SaaS services, address groups, and VRF segments — so applications defined " +
			"via arubasdwan_app_dns_classification, arubasdwan_app_compound_classification, or " +
			"arubasdwan_app_port_protocol and groups from arubasdwan_application_group can be attached to an " +
			"overlay.\n\n" +
			"The overlay itself must already exist; this resource only replaces its match configuration " +
			"(PUT /gms/rest/gms/overlays/config), leaving every other overlay setting untouched.\n\n" +
			"This resource owns the overlay's COMPLETE ACL: entries missing from the configuration are removed " +
			"on apply. For an overlay that already has entries, import it first (terraform import " +
			"arubasdwan_overlay_acl.<name> <overlay_name>) so the plan shows what would be removed; creating the " +
			"resource for a populated overlay is rejected. Destroying the resource resets the overlay to " +
			"matching all traffic.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Identifier of this resource (the overlay name).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"overlay_name": schema.StringAttribute{
				Description: "Name of the overlay whose ACL is managed (e.g. \"Business\"). The overlay must already exist.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"overlay_id": schema.Int64Attribute{
				Description: "Numeric ID of the overlay, resolved from overlay_name.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"acl_name": schema.StringAttribute{
				Description: "Name of the ACL inside the overlay configuration. Defaults to the existing ACL name, " +
					"or to \"Overlay_<overlay_name>\" when the overlay has no ACL yet.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"allow_entry_removal": schema.BoolAttribute{
				Description: "Whether ACL entries that Terraform never created may be removed. Defaults to false, " +
					"which aborts plan and apply for such entries so rules added outside Terraform are never " +
					"dropped unnoticed. Removing entries this resource created itself does not need the flag. " +
					"Set it to true to confirm the removal of foreign entries.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"entries": schema.SetNestedAttribute{
				Description: "The ACL entries. Order in the configuration is irrelevant — the sequence attribute " +
					"determines the evaluation order on the appliance. Each entry needs at least one match " +
					"criterion (criteria on the same entry are combined with AND), or match_all for an explicit " +
					"catch-all. Entries missing from this set are removed from the overlay's ACL.",
				Required: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"sequence": schema.Int64Attribute{
							Description: "Sequence number determining the evaluation order within the ACL. Must be unique.",
							Required:    true,
						},
						"permit": schema.BoolAttribute{
							Description: "Whether matching traffic is carried by this overlay. Defaults to true when " +
								"omitted; set to false to exclude the matched traffic.",
							Optional: true,
							Computed: true,
						},
						"match_all": schema.BoolAttribute{
							Description: "Match all traffic. Set this instead of any match criterion to express an " +
								"explicit catch-all entry; it cannot be combined with other criteria.",
							Optional: true,
							Computed: true,
						},
						"application": schema.StringAttribute{
							Description: "Name of the application to match — built-in or user-defined, including DNS, compound, and port/protocol classifications.",
							Optional:    true,
						},
						"app_group": schema.StringAttribute{
							Description: "Name of the application group to match.",
							Optional:    true,
						},
						"src_ip": schema.StringAttribute{
							Description: "Source IP address or CIDR; comma-separated for multiple values.",
							Optional:    true,
						},
						"dst_ip": schema.StringAttribute{
							Description: "Destination IP address or CIDR; comma-separated for multiple values.",
							Optional:    true,
						},
						"either_ip": schema.StringAttribute{
							Description: "Match the IP address or CIDR in either direction; comma-separated for multiple values.",
							Optional:    true,
						},
						"src_port": schema.StringAttribute{
							Description: "Source port or range; comma-separated for multiple values.",
							Optional:    true,
						},
						"dst_port": schema.StringAttribute{
							Description: "Destination port or range; comma-separated for multiple values.",
							Optional:    true,
						},
						"either_port": schema.StringAttribute{
							Description: "Match the port or range in either direction; comma-separated for multiple values.",
							Optional:    true,
						},
						"protocol": schema.StringAttribute{
							Description: "IP protocol to match (e.g. \"tcp\", \"udp\", \"icmp\").",
							Optional:    true,
						},
						"dscp": schema.StringAttribute{
							Description: "DSCP marking to match (e.g. \"ef\").",
							Optional:    true,
						},
						"src_dns": schema.StringAttribute{
							Description: "Source DNS hostname pattern.",
							Optional:    true,
						},
						"dst_dns": schema.StringAttribute{
							Description: "Destination DNS hostname pattern.",
							Optional:    true,
						},
						"either_dns": schema.StringAttribute{
							Description: "Match the DNS hostname pattern in either direction; supports wildcards (e.g. \"*.example.com\").",
							Optional:    true,
						},
						"src_service": schema.StringAttribute{
							Description: "Source SaaS service or organization name.",
							Optional:    true,
						},
						"dst_service": schema.StringAttribute{
							Description: "Destination SaaS service or organization name.",
							Optional:    true,
						},
						"either_service": schema.StringAttribute{
							Description: "Match the SaaS service or organization name in either direction.",
							Optional:    true,
						},
						"src_address_group": schema.StringAttribute{
							Description: "Source address group name (see arubasdwan_ip_address_group).",
							Optional:    true,
						},
						"dst_address_group": schema.StringAttribute{
							Description: "Destination address group name.",
							Optional:    true,
						},
						"either_address_group": schema.StringAttribute{
							Description: "Match the address group in either direction.",
							Optional:    true,
						},
						"src_vrf": schema.StringAttribute{
							Description: "Source VRF segment ID.",
							Optional:    true,
						},
						"dst_vrf": schema.StringAttribute{
							Description: "Destination VRF segment ID.",
							Optional:    true,
						},
						"comment": schema.StringAttribute{
							Description: "Free-form comment for this entry.",
							Optional:    true,
						},
					},
				},
			},
		},
	}
}

func (r *overlayACLResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = apiClient
}

// unmanagedEntriesError reports ACL entries that exist on the Orchestrator
// but are absent from the configuration, so applying would delete them.
func unmanagedEntriesError(overlayName string, sequences []string, imported bool) (string, string) {
	advice := "If those rules should be kept, add them to entries. " +
		"If this resource should own the ACL exclusively, " +
		"set allow_entry_removal = true to confirm their removal.\n\n" +
		"Terraform records which entries its applies created and treats the rest as foreign. " +
		"A freshly imported resource has no such record yet, so entries that predate the import " +
		"also count as foreign until the next apply records the configuration."
	if !imported {
		advice = "Import the overlay first so Terraform knows about them:\n\n" +
			"  terraform import arubasdwan_overlay_acl.<name> " + overlayName + "\n\n" + advice
	}
	return "Overlay ACL has entries that this configuration does not define",
		fmt.Sprintf(
			"The overlay %q has %d ACL entr%s on the Orchestrator that this configuration does not "+
				"define:\n\n  sequence %s\n\nApplying would remove %s.\n\n%s",
			overlayName, len(sequences), plural(len(sequences), "y", "ies"),
			strings.Join(sequences, "\n  sequence "),
			plural(len(sequences), "it", "them"), advice,
		)
}

// entrySummary renders an entry's action and match criteria in one line for
// diagnostics, e.g. `deny dst_ip "10.0.0.0/8"`.
func entrySummary(e overlayACLEntryModel) string {
	action := "permit"
	if !e.Permit.IsNull() && !e.Permit.IsUnknown() && !e.Permit.ValueBool() {
		action = "deny"
	}
	criteria := []struct {
		name  string
		value types.String
	}{
		{"application", e.Application}, {"app_group", e.AppGroup},
		{"src_ip", e.SrcIP}, {"dst_ip", e.DstIP}, {"either_ip", e.EitherIP},
		{"src_port", e.SrcPort}, {"dst_port", e.DstPort}, {"either_port", e.EitherPort},
		{"protocol", e.Protocol}, {"dscp", e.DSCP},
		{"src_dns", e.SrcDNS}, {"dst_dns", e.DstDNS}, {"either_dns", e.EitherDNS},
		{"src_service", e.SrcService}, {"dst_service", e.DstService}, {"either_service", e.EitherService},
		{"src_address_group", e.SrcAddressGroup}, {"dst_address_group", e.DstAddressGroup},
		{"either_address_group", e.EitherAddressGroup},
		{"src_vrf", e.SrcVRF}, {"dst_vrf", e.DstVRF},
	}
	parts := []string{}
	for _, c := range criteria {
		if !c.value.IsNull() && !c.value.IsUnknown() && c.value.ValueString() != "" {
			parts = append(parts, fmt.Sprintf("%s %q", c.name, c.value.ValueString()))
		}
	}
	if len(parts) == 0 {
		return action + " match-all"
	}
	return action + " " + strings.Join(parts, ", ")
}

// describeSequences appends each sequence's entry summary, so diagnostics
// show what a rule does instead of only its number.
func describeSequences(sequences []string, summaries map[string]string) []string {
	out := make([]string, 0, len(sequences))
	for _, seq := range sequences {
		if summary, ok := summaries[seq]; ok {
			out = append(out, fmt.Sprintf("%s (%s)", seq, summary))
			continue
		}
		out = append(out, seq)
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// missingSequences returns the sequence numbers present in existing but not
// in configured, as sorted strings.
func missingSequences(existing []client.OverlayACLEntry, configured []overlayACLEntryModel) []string {
	planned := make(map[int]bool, len(configured))
	for _, entry := range configured {
		planned[int(entry.Sequence.ValueInt64())] = true
	}
	missing := []int{}
	for _, entry := range existing {
		if !planned[entry.Sequence] {
			missing = append(missing, entry.Sequence)
		}
	}
	sort.Ints(missing)
	out := make([]string, 0, len(missing))
	for _, seq := range missing {
		out = append(out, strconv.Itoa(seq))
	}
	return out
}

// splitRemovals separates disappearing sequence numbers into entries this
// resource created (deliberate removals) and entries Terraform never saw.
// Without a record (state written by a provider version that predates it)
// every entry counts as managed, so upgrades plan cleanly.
func splitRemovals(disappearing []string, managed map[int]bool, recorded bool) (deliberate, foreign []string) {
	for _, seq := range disappearing {
		n, err := strconv.Atoi(seq)
		if !recorded || (err == nil && managed[n]) {
			deliberate = append(deliberate, seq)
			continue
		}
		foreign = append(foreign, seq)
	}
	return deliberate, foreign
}

// managedSequencesKey names the private-state entry that records which ACL
// sequence numbers this resource wrote during its last apply. It lets the
// removal guard tell a deliberate deletion (an entry Terraform created and
// the user has since dropped from the configuration) from a foreign rule
// that appeared on the Orchestrator without Terraform's knowledge.
const managedSequencesKey = "managed_sequences"

// readManagedSequences returns the recorded sequence numbers, and whether a
// record exists at all.
func readManagedSequences(ctx context.Context, private interface {
	GetKey(context.Context, string) ([]byte, diag.Diagnostics)
}, diags *diag.Diagnostics) (map[int]bool, bool) {
	raw, d := private.GetKey(ctx, managedSequencesKey)
	diags.Append(d...)
	if len(raw) == 0 {
		return nil, false
	}
	var sequences []int
	if err := json.Unmarshal(raw, &sequences); err != nil {
		// Unreadable record: treat as absent rather than failing the run.
		return nil, false
	}
	managed := make(map[int]bool, len(sequences))
	for _, seq := range sequences {
		managed[seq] = true
	}
	return managed, true
}

// writeManagedSequences records the sequence numbers of the given entries.
func writeManagedSequences(ctx context.Context, private interface {
	SetKey(context.Context, string, []byte) diag.Diagnostics
}, entries []overlayACLEntryModel, diags *diag.Diagnostics) {
	sequences := make([]int, 0, len(entries))
	for _, entry := range entries {
		sequences = append(sequences, int(entry.Sequence.ValueInt64()))
	}
	sort.Ints(sequences)
	encoded, err := json.Marshal(sequences)
	if err != nil {
		diags.AddError("Unable to record managed ACL entries", err.Error())
		return
	}
	diags.Append(private.SetKey(ctx, managedSequencesKey, encoded)...)
}

// entriesToModel converts the ACL entries reported by the Orchestrator into
// the resource model, so every computed attribute holds a known value.
func entriesToModel(overlay *client.Overlay) []overlayACLEntryModel {
	entries := make([]overlayACLEntryModel, 0, len(overlay.ACLEntries))
	for _, entry := range overlay.ACLEntries {
		entries = append(entries, overlayACLEntryModel{
			Sequence:           types.Int64Value(int64(entry.Sequence)),
			Permit:             types.BoolValue(entry.Permit),
			MatchAll:           types.BoolValue(entry.MatchAll()),
			Comment:            optionalString(entry.Comment),
			Application:        optionalString(entry.Match.Application),
			AppGroup:           optionalString(entry.Match.AppGroup),
			SrcIP:              optionalString(entry.Match.SrcIP),
			DstIP:              optionalString(entry.Match.DstIP),
			EitherIP:           optionalString(entry.Match.EitherIP),
			SrcPort:            optionalString(entry.Match.SrcPort),
			DstPort:            optionalString(entry.Match.DstPort),
			EitherPort:         optionalString(entry.Match.EitherPort),
			Protocol:           optionalString(entry.Match.Protocol),
			DSCP:               optionalString(entry.Match.DSCP),
			SrcDNS:             optionalString(entry.Match.SrcDNS),
			DstDNS:             optionalString(entry.Match.DstDNS),
			EitherDNS:          optionalString(entry.Match.EitherDNS),
			SrcService:         optionalString(entry.Match.SrcService),
			DstService:         optionalString(entry.Match.DstService),
			EitherService:      optionalString(entry.Match.EitherService),
			SrcAddressGroup:    optionalString(entry.Match.SrcAddressGroup),
			DstAddressGroup:    optionalString(entry.Match.DstAddressGroup),
			EitherAddressGroup: optionalString(entry.Match.EitherAddressGroup),
			SrcVRF:             optionalString(entry.Match.SrcVRF),
			DstVRF:             optionalString(entry.Match.DstVRF),
		})
	}
	return entries
}

// ModifyPlan guards against deleting ACL entries that Terraform never
// created. Entries the resource wrote itself may be removed by dropping
// them from the configuration; entries that appeared on the Orchestrator
// without Terraform's knowledge abort the run unless allow_entry_removal
// confirms their removal.
func (r *overlayACLResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Only relevant for updates: creation is guarded separately, and
	// destruction resets the ACL on purpose.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var state, plan overlayACLResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The refreshed state mirrors the Orchestrator.
	onOrchestrator := make([]client.OverlayACLEntry, 0, len(state.Entries))
	summaries := make(map[string]string, len(state.Entries))
	for _, entry := range state.Entries {
		onOrchestrator = append(onOrchestrator, client.OverlayACLEntry{Sequence: int(entry.Sequence.ValueInt64())})
		summaries[strconv.FormatInt(entry.Sequence.ValueInt64(), 10)] = entrySummary(entry)
	}
	disappearing := missingSequences(onOrchestrator, plan.Entries)
	if len(disappearing) == 0 {
		return
	}

	managed, recorded := readManagedSequences(ctx, req.Private, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	deliberate, foreign := splitRemovals(disappearing, managed, recorded)

	if len(foreign) > 0 && !plan.AllowEntryRemoval.ValueBool() {
		summary, detail := unmanagedEntriesError(plan.OverlayName.ValueString(), describeSequences(foreign, summaries), true)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	// Deliberate removals are expected; surface them because Terraform's
	// plan summary counts resources, not entries of a nested set.
	all := append(append([]string{}, deliberate...), foreign...)
	sort.Strings(all)
	note := "These entries were created by this resource and are no longer in the configuration."
	if len(foreign) > 0 {
		note = "allow_entry_removal is set, so entries created outside Terraform are removed as well."
	}
	resp.Diagnostics.AddWarning(
		fmt.Sprintf("Overlay ACL entries will be removed from %q", plan.OverlayName.ValueString()),
		fmt.Sprintf(
			"Applying removes %d ACL entr%s:\n\n  sequence %s\n\n%s Terraform's plan summary counts "+
				"resources, not individual ACL entries, so the removals appear only in the detailed diff "+
				"above and the resource is reported as a single in-place update.",
			len(all), plural(len(all), "y", "ies"),
			strings.Join(describeSequences(all, summaries), "\n  sequence "), note,
		),
	)
}

// toClientEntries validates the configured entries and converts them into
// the client model. Validation errors are reported against the offending
// attribute so users see which entry is at fault.
func toClientEntries(entries []overlayACLEntryModel, diags *diag.Diagnostics) []client.OverlayACLEntry {
	seen := map[int64]bool{}
	out := make([]client.OverlayACLEntry, 0, len(entries))

	for _, entry := range entries {
		entryPath := path.Root("entries")

		match := client.OverlayACLMatch{
			Application:        entry.Application.ValueString(),
			AppGroup:           entry.AppGroup.ValueString(),
			SrcIP:              entry.SrcIP.ValueString(),
			DstIP:              entry.DstIP.ValueString(),
			EitherIP:           entry.EitherIP.ValueString(),
			SrcPort:            entry.SrcPort.ValueString(),
			DstPort:            entry.DstPort.ValueString(),
			EitherPort:         entry.EitherPort.ValueString(),
			Protocol:           entry.Protocol.ValueString(),
			DSCP:               entry.DSCP.ValueString(),
			SrcDNS:             entry.SrcDNS.ValueString(),
			DstDNS:             entry.DstDNS.ValueString(),
			EitherDNS:          entry.EitherDNS.ValueString(),
			SrcService:         entry.SrcService.ValueString(),
			DstService:         entry.DstService.ValueString(),
			EitherService:      entry.EitherService.ValueString(),
			SrcAddressGroup:    entry.SrcAddressGroup.ValueString(),
			DstAddressGroup:    entry.DstAddressGroup.ValueString(),
			EitherAddressGroup: entry.EitherAddressGroup.ValueString(),
			SrcVRF:             entry.SrcVRF.ValueString(),
			DstVRF:             entry.DstVRF.ValueString(),
		}

		if entry.MatchAll.ValueBool() && !match.IsEmpty() {
			diags.AddAttributeError(entryPath, "Conflicting match configuration",
				"match_all cannot be combined with other match criteria. Remove the criteria or unset match_all.")
			continue
		}
		if !entry.MatchAll.ValueBool() && match.IsEmpty() {
			diags.AddAttributeError(entryPath, "Missing match criterion",
				"An ACL entry must set at least one match criterion (for example application or app_group). "+
					"To match all traffic on purpose, set match_all = true.")
			continue
		}

		seq := entry.Sequence.ValueInt64()
		if seen[seq] {
			diags.AddAttributeError(entryPath, "Duplicate sequence number",
				fmt.Sprintf("Sequence %d is used by more than one entry. Sequence numbers must be unique within an ACL.", seq))
			continue
		}
		seen[seq] = true

		// An omitted permit means "carry this traffic".
		permit := true
		if !entry.Permit.IsNull() && !entry.Permit.IsUnknown() {
			permit = entry.Permit.ValueBool()
		}

		out = append(out, client.OverlayACLEntry{
			Sequence: int(seq),
			Permit:   permit,
			Comment:  entry.Comment.ValueString(),
			Match:    match,
		})
	}
	return out
}

// applyEntries resolves the overlay by name and writes the ACL entries.
//
// On create, an overlay that already carries ACL entries is rejected: this
// resource owns the whole ACL, so adopting a populated overlay without
// importing it first would silently drop every entry not present in the
// configuration — and Terraform could not show that in the plan, because
// the existing entries are not in state yet.
func (r *overlayACLResource) applyEntries(plan *overlayACLResourceModel, diags *diag.Diagnostics, isCreate bool, managed map[int]bool, recorded bool) bool {
	entries := toClientEntries(plan.Entries, diags)
	if diags.HasError() {
		return false
	}

	overlay, err := r.client.GetOverlayByName(plan.OverlayName.ValueString())
	if err != nil {
		diags.AddError("Unable to read overlays", "Error calling GET /gms/rest/gms/overlays/config: "+err.Error())
		return false
	}
	if overlay == nil {
		diags.AddAttributeError(path.Root("overlay_name"), "Overlay not found",
			fmt.Sprintf("No overlay named %q exists on the Orchestrator. Create the overlay first.", plan.OverlayName.ValueString()))
		return false
	}

	if !isCreate && !plan.AllowEntryRemoval.ValueBool() {
		// Guard again against the live configuration: a plan run with
		// -refresh=false could otherwise slip past ModifyPlan. Same split
		// as there — removing entries this resource created is deliberate
		// and was already warned about in the plan; only foreign entries
		// abort the apply.
		if missing := missingSequences(overlay.ACLEntries, plan.Entries); len(missing) > 0 {
			if _, foreign := splitRemovals(missing, managed, recorded); len(foreign) > 0 {
				summary, detail := unmanagedEntriesError(plan.OverlayName.ValueString(), foreign, true)
				diags.AddError(summary, detail)
				return false
			}
		}
	}

	if isCreate && len(overlay.ACLEntries) > 0 {
		sequences := make([]string, 0, len(overlay.ACLEntries))
		for _, existing := range overlay.ACLEntries {
			sequences = append(sequences, strconv.Itoa(existing.Sequence))
		}
		summary, detail := unmanagedEntriesError(plan.OverlayName.ValueString(), sequences, false)
		diags.AddError(summary, detail)
		return false
	}

	updated, err := r.client.SetOverlayACL(overlay.ID, plan.ACLName.ValueString(), entries)
	if err != nil {
		diags.AddError("Unable to update overlay ACL",
			fmt.Sprintf("Error updating the ACL of overlay %q: %s", plan.OverlayName.ValueString(), err))
		return false
	}

	plan.ID = types.StringValue(updated.Name)
	plan.OverlayName = types.StringValue(updated.Name)
	plan.OverlayID = types.Int64Value(int64(updated.ID))
	plan.ACLName = types.StringValue(updated.ACLName)
	// Adopt the entries as stored, so computed attributes are known.
	plan.Entries = entriesToModel(updated)
	return true
}

func (r *overlayACLResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan overlayACLResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.applyEntries(&plan, &resp.Diagnostics, true, nil, false) {
		return
	}

	writeManagedSequences(ctx, resp.Private, plan.Entries, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *overlayACLResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state overlayACLResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	overlay, err := r.client.GetOverlayByName(state.OverlayName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read overlays", "Error calling GET /gms/rest/gms/overlays/config: "+err.Error())
		return
	}
	if overlay == nil {
		// The overlay is gone; drop the resource from state.
		resp.State.RemoveResource(ctx)
		return
	}

	// The entries Terraform knew before this refresh, kept before the
	// Orchestrator contents overwrite them below.
	knownEntries := state.Entries

	state.ID = types.StringValue(overlay.Name)
	state.OverlayName = types.StringValue(overlay.Name)
	state.OverlayID = types.Int64Value(int64(overlay.ID))
	state.ACLName = types.StringValue(overlay.ACLName)

	state.Entries = entriesToModel(overlay)

	// A state that predates the removal guard has no record of managed
	// entries yet. Seed it from the entries Terraform knew at the last
	// apply — not from the Orchestrator contents, which would count rules
	// added outside Terraform as managed and let an apply remove them
	// with only a warning. On a fresh import nothing is known yet, so the
	// record starts empty and every pre-existing entry stays foreign
	// until an apply adopts it. Existing records are left untouched: they
	// must reflect the last apply, not the current Orchestrator contents.
	if _, recorded := readManagedSequences(ctx, req.Private, &resp.Diagnostics); !recorded {
		writeManagedSequences(ctx, resp.Private, knownEntries, &resp.Diagnostics)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *overlayACLResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan overlayACLResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The managed-entries record travels with the plan's private state;
	// ModifyPlan already used it, applyEntries checks it once more against
	// the live overlay.
	managed, recorded := readManagedSequences(ctx, req.Private, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.applyEntries(&plan, &resp.Diagnostics, false, managed, recorded) {
		return
	}

	writeManagedSequences(ctx, resp.Private, plan.Entries, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete resets the overlay to matching all traffic, which is the state of a
// freshly created overlay. The overlay itself is never removed.
func (r *overlayACLResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state overlayACLResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	overlay, err := r.client.GetOverlayByName(state.OverlayName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read overlays", "Error calling GET /gms/rest/gms/overlays/config: "+err.Error())
		return
	}
	if overlay == nil {
		// Overlay already gone; nothing to reset.
		return
	}

	matchAll := []client.OverlayACLEntry{{Sequence: 1, Permit: true}}
	if _, err := r.client.SetOverlayACL(overlay.ID, state.ACLName.ValueString(), matchAll); err != nil {
		resp.Diagnostics.AddError("Unable to reset overlay ACL",
			fmt.Sprintf("Error resetting the ACL of overlay %q to match all traffic: %s", state.OverlayName.ValueString(), err))
	}
}

// ImportState imports an existing overlay ACL by overlay name.
func (r *overlayACLResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("overlay_name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	// Seed the removal guard so the first plan after an import does not
	// show a spurious change for it.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("allow_entry_removal"), false)...)
}
