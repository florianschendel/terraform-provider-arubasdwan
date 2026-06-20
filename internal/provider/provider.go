// Package provider implements the Terraform provider for managing Aruba EdgeConnect
// SD-WAN Orchestrator resources.
//
// The provider follows the HashiCorp Terraform Plugin Framework architecture:
//   - A single Provider implementation that handles configuration and client creation.
//   - Multiple Resource implementations for managing mutable Orchestrator objects
//     (security zones, policies, application definitions, etc.).
//   - Multiple DataSource implementations for reading Orchestrator state.
//
// All resources and data sources share a single *client.Client instance that is
// created during the provider's Configure phase and passed via ProviderData.
package provider

import (
	"context"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time check: ensure arubasdwanProvider implements the provider.Provider
// interface. If a required method is missing, this line will cause a build error.
var (
	_ provider.Provider = &arubasdwanProvider{}
)

// arubasdwanProviderModel maps the Terraform provider configuration block to a Go struct.
// Each field corresponds to an attribute in the provider { } block of the user's
// Terraform configuration:
//
//	provider "arubasdwan" {
//	  orchestrator_url = "https://orchestrator.example.com"
//	  api_key          = "secret-key"
//	  insecure         = true
//	}
type arubasdwanProviderModel struct {
	// OrchestratorURL is the base URL of the Aruba SD-WAN Orchestrator REST API
	// (e.g. "https://192.168.64.2"). All API calls are made relative to this URL.
	OrchestratorURL types.String `tfsdk:"orchestrator_url"`

	// APIKey is the authentication token sent in the X-Auth-Token header with
	// every API request. It is marked as Sensitive so Terraform will redact it
	// in plan output and state files.
	APIKey types.String `tfsdk:"api_key"`

	// Insecure controls whether TLS certificate verification is skipped when
	// connecting to the Orchestrator. This is useful for lab environments with
	// self-signed certificates but should be avoided in production.
	Insecure types.Bool `tfsdk:"insecure"`
}

// arubasdwanProvider is the top-level provider implementation. It holds the
// provider version string (injected at build time) and implements all required
// methods of the provider.Provider interface.
type arubasdwanProvider struct {
	version string
}

// New returns a factory function that Terraform calls to create a fresh provider
// instance. The version parameter is typically set via ldflags during the build
// and is reported in provider metadata for diagnostics and registry display.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &arubasdwanProvider{
			version: version,
		}
	}
}

// Metadata sets the provider type name ("arubasdwan") and version. The type name
// is used as a prefix for all resource and data source type names. For example,
// a resource with type name "security_zone" becomes "arubasdwan_security_zone".
func (p *arubasdwanProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "arubasdwan"
	resp.Version = p.version
}

// Schema defines the configuration attributes that users must or may set in
// their provider block. These attributes are used in Configure() to create
// the API client.
func (p *arubasdwanProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for managing Aruba EdgeConnect SD-WAN Orchestrator resources.",
		Attributes: map[string]schema.Attribute{
			"orchestrator_url": schema.StringAttribute{
				Description: "The URL of the Aruba SD-WAN Orchestrator. Example: https://192.168.64.2",
				Required:    true,
			},
			"api_key": schema.StringAttribute{
				Description: "The API key for authenticating with the Orchestrator.",
				Required:    true,
				Sensitive:   true,
			},
			"insecure": schema.BoolAttribute{
				Description: "Whether to skip TLS certificate verification. Defaults to false.",
				Optional:    true,
			},
		},
	}
}

// Configure is called by Terraform during the initialization phase. It reads the
// provider configuration values (orchestrator_url, api_key, insecure), creates
// an HTTP client configured for the Orchestrator API, and stores it in
// resp.DataSourceData and resp.ResourceData so that all resources and data sources
// can access it.
//
// If any required configuration value is unknown (i.e. depends on another resource
// that hasn't been created yet), this method returns an error because the client
// cannot be created without concrete values.
func (p *arubasdwanProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	// Deserialize the provider configuration block into the model struct.
	var config arubasdwanProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate that the orchestrator URL is known (not a deferred reference).
	if config.OrchestratorURL.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown Orchestrator URL",
			"The provider cannot create the Aruba SD-WAN API client as there is an unknown configuration value for the Orchestrator URL.",
		)
		return
	}

	// Validate that the API key is known.
	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown API Key",
			"The provider cannot create the Aruba SD-WAN API client as there is an unknown configuration value for the API key.",
		)
		return
	}

	// Extract the concrete Go values from the Terraform type wrappers.
	orchestratorURL := config.OrchestratorURL.ValueString()
	apiKey := config.APIKey.ValueString()

	// Default insecure to false if not explicitly set.
	insecure := false
	if !config.Insecure.IsNull() {
		insecure = config.Insecure.ValueBool()
	}

	// Create the API client. This configures the HTTP transport with TLS settings
	// and stores the API key for use in all subsequent requests.
	apiClient := client.NewClient(orchestratorURL, apiKey, insecure)

	// Make the client available to all data sources and resources via ProviderData.
	// Each resource/data source retrieves it in its own Configure method.
	resp.DataSourceData = apiClient
	resp.ResourceData = apiClient
}

// DataSources returns the list of data source factory functions that this provider
// supports. Each factory function creates a new data source instance. The data
// sources provide read-only access to Orchestrator state:
//
//   - SecurityZones:      Lists all security zones.
//   - SecurityPolicies:   Lists all firewall policies for a given segment pair.
//   - AppPortProtocols:   Lists all port/protocol application classifications.
//   - ApplicationGroups:  Lists all application groups (tags).
//   - VRFSegments:        Lists all VRF segments and zone-to-VRF mappings.
func (p *arubasdwanProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewSecurityZonesDataSource,
		NewSecurityPoliciesDataSource,
		NewAppPortProtocolsDataSource,
		NewAppDNSClassificationsDataSource,
		NewAppCompoundClassificationsDataSource,
		NewAppSearchDataSource,
		NewApplicationGroupsDataSource,
		NewIPAddressGroupsDataSource,
		NewVRFSegmentsDataSource,
	}
}

// Resources returns the list of resource factory functions that this provider
// supports. Each factory function creates a new resource instance. Resources
// support full CRUD lifecycle management:
//
//   - SecurityZone:              Manages a single firewall security zone.
//   - SecurityPolicy:            Manages a single firewall policy rule.
//   - AppPortProtocol:           Manages a port/protocol application classification.
//   - ApplicationGroup:          Manages an application group (tag).
//   - AppDNSClassification:      Manages a DNS domain-based application classification.
//   - AppCompoundClassification: Manages a compound (multi-criteria) application classification.
func (p *arubasdwanProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSecurityZoneResource,
		NewSecurityPolicyResource,
		NewAppPortProtocolResource,
		NewApplicationGroupResource,
		NewAppDNSClassificationResource,
		NewAppCompoundClassificationResource,
		NewIPAddressGroupResource,
	}
}
