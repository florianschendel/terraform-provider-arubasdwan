// Package client implements the HTTP API client for the Aruba EdgeConnect SD-WAN
// Orchestrator REST API.
//
// The client handles all communication with the Orchestrator, including:
//   - Authentication via the X-Auth-Token header
//   - TLS configuration (with optional certificate verification skip)
//   - JSON serialization/deserialization of request and response bodies
//   - Concurrency safety via per-resource-type mutexes
//
// The Orchestrator API follows a "full payload replacement" pattern for many
// resources: to add, update, or delete a single item, the client must first
// fetch the complete set, modify it locally, and POST the entire set back.
// This is particularly true for security zones, security policies, and
// application groups.
package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
)

// policyKey uniquely identifies a security policy rule within a Terraform run.
// It is used by the policyRegistry to detect duplicate policy definitions
// before they are sent to the Orchestrator API.
type policyKey struct {
	SegmentPair string // The VRF segment pair (e.g. "0_0" for Default-to-Default)
	SrcZone     int    // Source security zone ID
	DstZone     int    // Destination security zone ID
	Priority    int    // Rule priority (lower number = higher priority)
}

// Client holds the configuration and state for the Aruba SD-WAN Orchestrator
// API client. It is created once during the provider's Configure phase and
// shared across all resources and data sources.
//
// The client uses separate mutexes for different resource types to serialize
// write operations. This prevents race conditions when Terraform creates or
// modifies multiple resources of the same type concurrently (e.g. when
// creating multiple security zones in parallel).
type Client struct {
	BaseURL    string       // The base URL of the Orchestrator (e.g. "https://192.168.64.2")
	APIKey     string       // The API key sent in the X-Auth-Token header
	HTTPClient *http.Client // The underlying HTTP client with TLS configuration

	// Mutexes that serialize write operations per resource type. The Orchestrator
	// API uses a read-modify-write pattern (fetch all, modify, POST all back),
	// so concurrent writes to the same resource type could cause data loss.
	zoneMu     sync.Mutex // Serializes security zone create/update/delete operations
	policyMu   sync.Mutex // Serializes security policy create/update/delete operations
	appDefMu   sync.Mutex // Serializes application definition (port/protocol, DNS, compound, IP intel) operations
	appGroupMu sync.Mutex // Serializes application group create/update/delete operations
	ipObjectMu sync.Mutex // Serializes IP object (address group, service group) operations

	// policyRegistry tracks policies created during the current Terraform run
	// to detect duplicates before they hit the API. The key uniquely identifies
	// a policy; the value is a human-readable description for error messages.
	policyRegistry map[policyKey]string
}

// Zone represents a security zone in the Orchestrator.
// Security zones are used to segment the network and define firewall policy
// boundaries. Each zone has a unique numeric ID assigned by the Orchestrator
// and a user-defined name (e.g. "LAN", "WAN", "DMZ").
//
// The API returns zones as a JSON object keyed by zone ID:
//
//	{"0": {"name": "Default"}, "20": {"name": "LAN"}, "21": {"name": "WAN"}}
//
// The ID is parsed from the JSON object key, not from within the value.
type Zone struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ZoneEEEnable represents the End-to-End Zone Based Firewall configuration.
// When enabled, firewall policies are enforced across the entire SD-WAN fabric,
// not just at individual appliances.
type ZoneEEEnable struct {
	Enable bool `json:"enable"`
}

// NextZoneID represents the response from the /gms/rest/zones/nextId endpoint.
// The Orchestrator assigns zone IDs sequentially, and this endpoint returns the
// next available ID for a new zone.
type NextZoneID struct {
	NextID int `json:"nextId"`
}

// VrfSegmentZonesMap represents the raw VRF segment-to-zones mapping response.
// This is used internally for VRF-aware zone operations.
type VrfSegmentZonesMap map[string]interface{}

// VrfZonesMap represents the raw VRF firewall zones mapping response.
type VrfZonesMap map[string]interface{}

// NewClient creates a new Orchestrator API client with the given configuration.
//
// Parameters:
//   - baseURL:  The base URL of the Orchestrator (e.g. "https://192.168.64.2").
//   - apiKey:   The API key for authentication (sent as X-Auth-Token header).
//   - insecure: If true, TLS certificate verification is skipped. This is useful
//     for lab environments with self-signed certificates.
//
// The returned client is safe for concurrent use by multiple goroutines thanks
// to the internal mutexes that serialize write operations.
func NewClient(baseURL, apiKey string, insecure bool) *Client {
	// Configure the HTTP transport with TLS settings. When insecure is true,
	// the client will accept any certificate presented by the server.
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecure,
		},
	}

	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Transport: transport,
		},
	}
}

// doRequest performs an authenticated HTTP request to the Orchestrator API.
// It handles JSON marshaling of the request body and returns the raw response
// body, HTTP status code, and any error that occurred.
//
// All API requests include:
//   - Content-Type: application/json
//   - X-Auth-Token: <apiKey>  (for authentication)
//
// Parameters:
//   - method: HTTP method (GET, POST, DELETE)
//   - path:   API path relative to the base URL (e.g. "/gms/rest/zones")
//   - body:   Request body to marshal as JSON (nil for GET/DELETE without body)
//
// Returns:
//   - []byte: The raw response body
//   - int:    The HTTP status code
//   - error:  Any error during request creation, execution, or body reading
func (c *Client) doRequest(method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader

	// Marshal the request body to JSON if provided.
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("error marshaling request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	// Construct the full URL by combining the base URL with the API path.
	url := fmt.Sprintf("%s%s", c.BaseURL, path)
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating request: %w", err)
	}

	// Set required headers for Orchestrator API authentication and content type.
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", c.APIKey)

	// Execute the HTTP request.
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error executing request: %w", err)
	}
	defer resp.Body.Close()

	// Read the entire response body for JSON parsing.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("error reading response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// ===========================================================================
// Security Zones — GET /gms/rest/zones
// ===========================================================================

// zonesAPIResponse represents the JSON wire format for the zones endpoint.
// The API returns zones as an object where keys are zone IDs (as strings)
// and values contain the zone name:
//
//	{"0": {"name": "Default"}, "20": {"name": "LAN"}, "21": {"name": "WAN"}}
type zonesAPIResponse map[string]struct {
	Name string `json:"name"`
}

// zonesToAPIPayload converts a slice of Zone structs into the JSON object format
// expected by the Orchestrator's POST /zones endpoint.
// The output format matches the GET response: {"<id>": {"name": "..."}, ...}
func zonesToAPIPayload(zones []Zone) map[string]struct {
	Name string `json:"name"`
} {
	payload := make(map[string]struct {
		Name string `json:"name"`
	}, len(zones))
	for _, z := range zones {
		payload[strconv.Itoa(z.ID)] = struct {
			Name string `json:"name"`
		}{Name: z.Name}
	}
	return payload
}

// GetZones retrieves all security zones from the Orchestrator.
//
// API endpoint: GET /gms/rest/zones
//
// The response is a JSON object keyed by zone ID. This method parses the
// object keys as integer IDs and returns a sorted slice of Zone structs.
// The returned slice is sorted by zone ID in ascending order.
func (c *Client) GetZones() ([]Zone, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/zones", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /zones returned status %d: %s", statusCode, string(respBody))
	}

	// Parse the JSON object: keys are zone IDs, values contain the zone name.
	var raw zonesAPIResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling zones response: %w", err)
	}

	// Convert the map entries into a slice of Zone structs.
	zones := make([]Zone, 0, len(raw))
	for idStr, z := range raw {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue // Skip entries with non-numeric keys (shouldn't happen in practice)
		}
		zones = append(zones, Zone{ID: id, Name: z.Name})
	}

	// Sort zones by ID for consistent ordering (Go maps have random iteration order).
	sort.Slice(zones, func(i, j int) bool { return zones[i].ID < zones[j].ID })

	return zones, nil
}

// GetZoneByID retrieves a single security zone by its numeric ID.
// It fetches all zones and scans for the matching ID.
//
// Returns:
//   - *Zone: The matching zone, or nil if no zone with that ID exists.
//   - error: Any error from the API call.
//
// Note: Returns (nil, nil) when the zone does not exist — this is intentional
// and allows the Terraform Read method to detect deleted resources.
func (c *Client) GetZoneByID(id int) (*Zone, error) {
	zones, err := c.GetZones()
	if err != nil {
		return nil, err
	}

	for _, z := range zones {
		if z.ID == id {
			return &z, nil
		}
	}

	return nil, nil
}

// ===========================================================================
// Security Zones — POST /gms/rest/zones
// ===========================================================================

// CreateOrUpdateZones sends the complete set of zones to the Orchestrator.
// The API uses a "full replacement" model: the POST body must contain ALL zones,
// not just the ones being added or modified. Any zone present on the Orchestrator
// but missing from the POST body will be deleted.
//
// Parameters:
//   - zones: The complete set of zones that should exist after the operation.
//   - deleteDependencies: If true, zones can be deleted even if they are
//     referenced by security policies. If false, the API will reject the
//     request if a zone to be deleted is still in use.
//
// API endpoint: POST /gms/rest/zones?deleteDependencies=<bool>
func (c *Client) CreateOrUpdateZones(zones []Zone, deleteDependencies bool) ([]Zone, error) {
	payload := zonesToAPIPayload(zones)
	path := fmt.Sprintf("/gms/rest/zones?deleteDependencies=%t", deleteDependencies)
	respBody, statusCode, err := c.doRequest("POST", path, payload)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated && statusCode != http.StatusNoContent {
		return nil, fmt.Errorf("POST /zones returned status %d: %s", statusCode, string(respBody))
	}

	return zones, nil
}

// CreateZone creates a single new security zone on the Orchestrator.
//
// This method follows the read-modify-write pattern:
//  1. Fetch the next available zone ID from the Orchestrator.
//  2. Fetch all existing zones.
//  3. Append the new zone to the list.
//  4. POST the complete zone set back to the Orchestrator.
//  5. Read back the created zone to confirm it was saved correctly.
//
// The operation is serialized via zoneMu to prevent race conditions when
// multiple zones are being created concurrently by Terraform.
func (c *Client) CreateZone(name string, _ ...string) (*Zone, error) {
	c.zoneMu.Lock()
	defer c.zoneMu.Unlock()

	// Step 1: Get the next available zone ID from the Orchestrator.
	nextID, err := c.GetNextZoneID()
	if err != nil {
		return nil, fmt.Errorf("error obtaining next zone ID: %w", err)
	}

	// Step 2: Fetch the current complete set of zones.
	existing, err := c.GetZones()
	if err != nil {
		return nil, fmt.Errorf("error fetching existing zones: %w", err)
	}

	// Step 3: Create the new zone and append it to the existing set.
	zone := Zone{
		ID:   nextID,
		Name: name,
	}

	// Step 4: POST the complete set (existing zones + new zone) back to the API.
	allZones := append(existing, zone)
	_, err = c.CreateOrUpdateZones(allZones, false)
	if err != nil {
		return nil, err
	}

	// Step 5: Read back to confirm the zone was persisted correctly.
	created, err := c.GetZoneByID(nextID)
	if err != nil {
		return nil, fmt.Errorf("zone was created but could not be read back: %w", err)
	}
	if created == nil {
		// Zone wasn't found in read-back; return the locally constructed zone
		// as a fallback (the POST succeeded, so it should exist).
		return &zone, nil
	}

	return created, nil
}

// UpdateZone updates an existing security zone's name by replacing it in the
// complete zone set and posting back.
//
// This follows the same read-modify-write pattern as CreateZone:
//  1. Fetch all existing zones.
//  2. Find and replace the target zone in the list.
//  3. POST the complete modified set back.
//  4. Read back to confirm the update.
//
// If the target zone is not found in the existing set, it is appended (upsert).
func (c *Client) UpdateZone(zone Zone) (*Zone, error) {
	c.zoneMu.Lock()
	defer c.zoneMu.Unlock()

	existing, err := c.GetZones()
	if err != nil {
		return nil, fmt.Errorf("error fetching existing zones: %w", err)
	}

	// Replace the target zone in the list by matching on ID.
	found := false
	for i, z := range existing {
		if z.ID == zone.ID {
			existing[i] = zone
			found = true
			break
		}
	}
	if !found {
		// If the zone wasn't found, treat this as an upsert by appending it.
		existing = append(existing, zone)
	}

	_, err = c.CreateOrUpdateZones(existing, false)
	if err != nil {
		return nil, err
	}

	// Read back the zone to get the authoritative state from the Orchestrator.
	updated, err := c.GetZoneByID(zone.ID)
	if err != nil {
		return nil, fmt.Errorf("zone was updated but could not be read back: %w", err)
	}
	if updated == nil {
		return &zone, nil
	}

	return updated, nil
}

// DeleteZone removes a security zone from the Orchestrator by filtering it
// out of the complete zone set and posting the remaining zones back.
//
// The deleteDependencies parameter is set to true, which allows the zone to
// be removed even if security policies reference it. Without this, the API
// would reject the deletion of a zone that is still in use.
//
// If the zone does not exist (already deleted), this method succeeds silently.
func (c *Client) DeleteZone(id int) error {
	c.zoneMu.Lock()
	defer c.zoneMu.Unlock()

	zones, err := c.GetZones()
	if err != nil {
		return fmt.Errorf("error fetching zones for deletion: %w", err)
	}

	// Build a new slice excluding the zone to be deleted.
	remaining := make([]Zone, 0, len(zones))
	found := false
	for _, z := range zones {
		if z.ID == id {
			found = true
			continue // Skip the zone to be deleted
		}
		remaining = append(remaining, z)
	}

	if !found {
		// Zone does not exist — nothing to delete. This is idempotent.
		return nil
	}

	// POST the remaining zones with deleteDependencies=true to allow removal
	// even if the zone is referenced by security policies.
	_, err = c.CreateOrUpdateZones(remaining, true)
	if err != nil {
		return fmt.Errorf("error posting remaining zones after deletion: %w", err)
	}

	return nil
}

// ===========================================================================
// Security Zones — GET /gms/rest/zones/nextId
// ===========================================================================

// GetNextZoneID retrieves the next available zone ID from the Orchestrator.
// This endpoint returns a monotonically increasing ID that the Orchestrator
// reserves for the next zone creation.
//
// API endpoint: GET /gms/rest/zones/nextId
// Response format: {"nextId": 22}
func (c *Client) GetNextZoneID() (int, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/zones/nextId", nil)
	if err != nil {
		return 0, err
	}

	if statusCode != http.StatusOK {
		return 0, fmt.Errorf("GET /zones/nextId returned status %d: %s", statusCode, string(respBody))
	}

	var result NextZoneID
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("error unmarshaling nextId response: %w (body: %s)", err, string(respBody))
	}

	return result.NextID, nil
}

// ===========================================================================
// End-to-End Zone Based Firewall — GET/POST /gms/rest/zones/eeEnable
// ===========================================================================

// GetEEEnable returns the End-to-End Zone Based Firewall configuration.
// When enabled, the Orchestrator enforces zone-based firewall policies across
// the entire SD-WAN fabric (between all appliances), not just locally.
//
// API endpoint: GET /gms/rest/zones/eeEnable
func (c *Client) GetEEEnable() (*ZoneEEEnable, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/zones/eeEnable", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /zones/eeEnable returned status %d: %s", statusCode, string(respBody))
	}

	var result ZoneEEEnable
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling eeEnable response: %w", err)
	}

	return &result, nil
}

// SetEEEnable updates the End-to-End Zone Based Firewall configuration.
//
// API endpoint: POST /gms/rest/zones/eeEnable
func (c *Client) SetEEEnable(cfg ZoneEEEnable) error {
	respBody, statusCode, err := c.doRequest("POST", "/gms/rest/zones/eeEnable", cfg)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("POST /zones/eeEnable returned status %d: %s", statusCode, string(respBody))
	}

	return nil
}

// ===========================================================================
// VRF Zone Mappings — GET /gms/rest/zones/vrfSegmentZonesMap
// ===========================================================================

// GetVrfSegmentZonesMap returns the raw VRF-segment-to-zones mapping.
// This mapping defines which zones exist in each VRF segment. Each zone can
// have a different numeric ID in different VRF segments, even though they
// share the same name.
//
// API endpoint: GET /gms/rest/zones/vrfSegmentZonesMap
func (c *Client) GetVrfSegmentZonesMap() (VrfSegmentZonesMap, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/zones/vrfSegmentZonesMap", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /zones/vrfSegmentZonesMap returned status %d: %s", statusCode, string(respBody))
	}

	var result VrfSegmentZonesMap
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling vrfSegmentZonesMap response: %w", err)
	}

	return result, nil
}

// ===========================================================================
// VRF Firewall Zones — GET /gms/rest/zones/vrfZonesMap
// ===========================================================================

// GetVrfZonesMap returns the raw VRF firewall zones mapping.
//
// API endpoint: GET /gms/rest/zones/vrfZonesMap
func (c *Client) GetVrfZonesMap() (VrfZonesMap, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/zones/vrfZonesMap", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /zones/vrfZonesMap returned status %d: %s", statusCode, string(respBody))
	}

	var result VrfZonesMap
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling vrfZonesMap response: %w", err)
	}

	return result, nil
}

// ===========================================================================
// Security Policies — /gms/rest/vrf/config/securityPolicies
//
// Security policies define firewall rules between security zones within a
// VRF segment pair. Each policy has:
//   - A source and destination zone (identifying the traffic direction)
//   - A priority (lower number = evaluated first)
//   - An action (allow or deny)
//   - Match criteria (IP, port, protocol, application, DNS, geo, service, etc.)
//
// The API uses a deeply nested JSON structure:
//   data -> map1 -> <srcZone_dstZone> -> prio -> <priority> -> rule details
//
// Zone IDs in the API are VRF-specific. The client translates them to/from
// Default VRF (0) IDs so that Terraform users always work with consistent
// zone IDs regardless of which VRF segment pair is being configured.
// ===========================================================================

// SecurityPolicy represents a single firewall policy rule with all its match
// criteria and action settings. This is the internal model used by the provider;
// it uses Default VRF zone IDs (translated from VRF-specific IDs by the client).
type SecurityPolicy struct {
	SourceZoneID int    // Source security zone ID (in Default VRF numbering)
	DestZoneID   int    // Destination security zone ID (in Default VRF numbering)
	Priority     int    // Rule evaluation priority (20000-65535, lower = higher priority)
	Action       string // "allow" or "deny"
	RuleState    string // "enable" or "disable"
	Logging      string // "enable" or "disable" — whether to log matching traffic
	LogPriority  string // Syslog priority level for logged events
	Comment      string // User-defined comment for the rule

	// Match criteria — Network layer
	ACL        string // Named ACL to match against
	SrcIP      string // Source IP address or CIDR range (e.g. "10.0.0.0/8")
	DstIP      string // Destination IP address or CIDR range
	EitherIP   string // Match either source or destination IP (bidirectional)
	SrcPort    string // Source port or range (e.g. "80", "1024-65535")
	DstPort    string // Destination port or range
	EitherPort string // Match either source or destination port
	Protocol   string // IP protocol (e.g. "tcp", "udp", "icmp", "ip")

	// Match criteria — Application layer
	Application string // Application name to match (from application definitions)
	AppGroup    string // Application group/tag to match

	// Match criteria — DNS/Domain
	SrcDNS    string // Source DNS hostname pattern
	DstDNS    string // Destination DNS hostname pattern
	EitherDNS string // Match either direction DNS (supports wildcards: "*.google.com")

	// Match criteria — Geolocation
	SrcGeo    string // Source country code (e.g. "US", "DE")
	DstGeo    string // Destination country code
	EitherGeo string // Match either direction geo

	// Match criteria — Service
	SrcService    string // Source SaaS service or organization name
	DstService    string // Destination service
	EitherService string // Match either direction service

	// Match criteria — Address Groups (references to /ipObjects/addressGroup)
	SrcAddressGroup    string // Source address group name
	DstAddressGroup    string // Destination address group name
	EitherAddressGroup string // Match either direction against address group

	// Match criteria — Other
	DSCP    string // DiffServ Code Point value
	VLAN    string // Interface or VLAN name (e.g. "lan0", "wan0")
	Overlay string // Overlay tunnel to match
}

// commaToPipe replaces commas with pipes ("|"). The Orchestrator API uses
// "|" as the separator for multi-value match fields (IP and port lists),
// while users in HCL conventionally write comma-separated lists.
func commaToPipe(s string) string {
	if s == "" {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, '|')
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// pipeToComma is the inverse of commaToPipe. Used when reading values back
// from the API so that Terraform state matches what the user wrote in HCL.
func pipeToComma(s string) string {
	if s == "" {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			out = append(out, ',')
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// Wire-format types for the deeply nested security policies JSON response.
// These structs map directly to the JSON structure returned by the API.

// policyRuleMatch contains all the match criteria for a single policy rule.
// Fields with `omitempty` are excluded from the JSON when empty, keeping the
// payload minimal.
type policyRuleMatch struct {
	ACL           string `json:"acl,omitempty"`
	SrcIP         string `json:"src_ip,omitempty"`
	DstIP         string `json:"dst_ip,omitempty"`
	EitherIP      string `json:"either_ip,omitempty"`
	SrcPort       string `json:"src_port,omitempty"`
	DstPort       string `json:"dst_port,omitempty"`
	EitherPort    string `json:"either_port,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	Application   string `json:"application,omitempty"`
	AppGroup      string `json:"app_group,omitempty"`
	SrcDNS        string `json:"src_dns,omitempty"`
	DstDNS        string `json:"dst_dns,omitempty"`
	EitherDNS     string `json:"either_dns,omitempty"`
	SrcGeo        string `json:"src_geo,omitempty"`
	DstGeo        string `json:"dst_geo,omitempty"`
	EitherGeo     string `json:"either_geo,omitempty"`
	SrcService    string `json:"src_service,omitempty"`
	DstService    string `json:"dst_service,omitempty"`
	EitherService string `json:"either_service,omitempty"`
	DSCP          string `json:"dscp,omitempty"`
	VLAN          string `json:"vlan,omitempty"`
	Overlay       string `json:"overlay,omitempty"`
	// Address group references
	SrcAddressGroup    string `json:"src_addrgrp_groups,omitempty"`
	DstAddressGroup    string `json:"dst_addrgrp_groups,omitempty"`
	EitherAddressGroup string `json:"either_addrgrp_groups,omitempty"`
}

// policyRuleMisc contains metadata fields for a policy rule (enable/disable
// state, logging configuration). Used in GET responses where logging_priority
// is a json.Number.
type policyRuleMisc struct {
	Rule            string      `json:"rule"`             // "enable" or "disable"
	Logging         string      `json:"logging"`          // "enable" or "disable"
	LoggingPriority json.Number `json:"logging_priority"` // Syslog priority as a number
}

// policyRuleSet contains the action for a policy rule.
type policyRuleSet struct {
	Action string `json:"action"` // "allow" or "deny"
}

// policyRuleEntry is the complete wire format for a single policy rule as
// returned by the GET endpoint. It combines match criteria, metadata, and action.
type policyRuleEntry struct {
	Match     policyRuleMatch `json:"match"`      // Traffic matching criteria
	Self      int             `json:"self"`        // Self-referential ID (used internally by Orchestrator)
	Misc      policyRuleMisc  `json:"misc"`        // Rule state and logging settings
	Comment   string          `json:"comment"`     // User comment
	GMSMarked bool            `json:"gms_marked"`  // Whether the rule was created by the Orchestrator (GMS)
	Set       policyRuleSet   `json:"set"`         // The action to take (allow/deny)
}

// ===========================================================================
// Zone ID translation helpers for cross-VRF policies
//
// When the Orchestrator has multiple VRF segments, each zone gets a unique
// numeric ID per VRF. For example, the "LAN" zone might be ID 20 in VRF 0
// but ID 40 in VRF 1. The Terraform user always works with Default VRF (0)
// zone IDs, and the client translates them to/from VRF-specific IDs
// transparently.
// ===========================================================================

// translatePoliciesToDefaultVRF maps VRF-specific zone IDs in policy rules
// back to their equivalent Default VRF (0) IDs. This is called after fetching
// policies from the API so that the Terraform state always contains consistent
// Default VRF zone IDs.
//
// If the zone translation fails (e.g. VRF mappings can't be fetched), the
// original untranslated policies are returned as a fallback.
func (c *Client) translatePoliciesToDefaultVRF(policies []SecurityPolicy) []SecurityPolicy {
	zt, err := c.NewZoneTranslator()
	if err != nil {
		return policies // Fall back to untranslated on error.
	}
	result := make([]SecurityPolicy, len(policies))
	for i, p := range policies {
		result[i] = p
		result[i].SourceZoneID = zt.ToDefaultVRF(p.SourceZoneID)
		result[i].DestZoneID = zt.ToDefaultVRF(p.DestZoneID)
	}
	return result
}

// translatePoliciesToVRF maps Default VRF (0) zone IDs to the target VRF-specific
// IDs before posting policies to the API. srcVRFID and dstVRFID can differ for
// cross-VRF (inter-segment) policies.
//
// For example, if a policy goes from VRF 0 (Default) to VRF 1 (Guest), the source
// zones stay in VRF 0 numbering and destination zones are translated to VRF 1
// numbering.
func (c *Client) translatePoliciesToVRF(policies []SecurityPolicy, srcVRFID, dstVRFID int) ([]SecurityPolicy, error) {
	zt, err := c.NewZoneTranslator()
	if err != nil {
		return nil, err
	}
	result := make([]SecurityPolicy, len(policies))
	for i, p := range policies {
		result[i] = p
		// Only translate if the VRF is non-default (VRF 0 = Default, no translation needed).
		if srcVRFID != 0 {
			id, err := zt.ToVRF(p.SourceZoneID, srcVRFID)
			if err != nil {
				return nil, fmt.Errorf("translating source zone: %w", err)
			}
			result[i].SourceZoneID = id
		}
		if dstVRFID != 0 {
			id, err := zt.ToVRF(p.DestZoneID, dstVRFID)
			if err != nil {
				return nil, fmt.Errorf("translating dest zone: %w", err)
			}
			result[i].DestZoneID = id
		}
	}
	return result, nil
}

// ===========================================================================
// GET /gms/rest/vrf/config/securityPolicies?map=<segmentPair>
// ===========================================================================

// GetSecurityPolicies retrieves all security policies for a given VRF segment pair.
// The returned policies have their zone IDs translated to Default VRF (0) numbering
// for consistency with the Terraform state.
//
// Parameters:
//   - segmentPair: The VRF segment pair (e.g. "0_0" for Default-to-Default,
//     "0_1" for Default-to-Guest).
//
// API endpoint: GET /gms/rest/vrf/config/securityPolicies?map=<segmentPair>
func (c *Client) GetSecurityPolicies(segmentPair string) ([]SecurityPolicy, error) {
	path := fmt.Sprintf("/gms/rest/vrf/config/securityPolicies?map=%s", segmentPair)
	respBody, statusCode, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /vrf/config/securityPolicies returned status %d: %s", statusCode, string(respBody))
	}

	// Parse the deeply nested JSON response into a flat slice of SecurityPolicy.
	policies, err := parseSecurityPoliciesResponse(respBody)
	if err != nil {
		return nil, err
	}

	// Translate VRF-specific zone IDs back to Default VRF IDs so that
	// Terraform always sees consistent zone numbering.
	return c.translatePoliciesToDefaultVRF(policies), nil
}

// parseSecurityPoliciesResponse parses the deeply nested JSON response from the
// security policies endpoint into a flat, sorted slice of SecurityPolicy structs.
//
// The JSON structure is:
//
//	{
//	  "data": {
//	    "map1": {
//	      "20_21": {                          // zone pair: srcZone_dstZone
//	        "prio": {
//	          "1000": {                        // priority
//	            "match": { ... },              // match criteria
//	            "misc": { "rule": "enable" },  // rule state
//	            "set": { "action": "allow" }   // action
//	          }
//	        }
//	      }
//	    }
//	  },
//	  "options": { ... },
//	  "settings": { ... }
//	}
func parseSecurityPoliciesResponse(data []byte) ([]SecurityPolicy, error) {
	// Parse the top-level envelope to extract the "data" field.
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("error parsing security policies envelope: %w", err)
	}

	// "data" can be null when no policies exist for this segment pair.
	if string(envelope.Data) == "null" || len(envelope.Data) == 0 {
		return nil, nil
	}

	// Parse the data object which contains map entries (typically just "map1").
	var dataMaps map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data, &dataMaps); err != nil {
		return nil, fmt.Errorf("error parsing security policies data: %w", err)
	}

	var policies []SecurityPolicy

	// Iterate over map entries (usually just "map1").
	for _, mapRaw := range dataMaps {
		// Each map entry contains dynamic zone-pair keys like "20_21" plus a "self" key.
		var mapEntries map[string]json.RawMessage
		if err := json.Unmarshal(mapRaw, &mapEntries); err != nil {
			continue
		}

		for zonePairKey, zpRaw := range mapEntries {
			// Skip the "self" key which is metadata, not a zone pair.
			if zonePairKey == "self" {
				continue
			}

			// Parse the zone pair key (e.g. "20_21") into source and destination zone IDs.
			srcZone, dstZone, ok := parseZonePair(zonePairKey)
			if !ok {
				continue
			}

			// Each zone pair entry contains a "prio" object with priority-keyed rules.
			var zpEntry struct {
				Prio map[string]json.RawMessage `json:"prio"`
			}
			if err := json.Unmarshal(zpRaw, &zpEntry); err != nil {
				continue
			}

			// Iterate over priority entries and parse each rule.
			for prioKey, ruleRaw := range zpEntry.Prio {
				prio, err := strconv.Atoi(prioKey)
				if err != nil {
					continue
				}

				var rule policyRuleEntry
				if err := json.Unmarshal(ruleRaw, &rule); err != nil {
					continue
				}

				// Convert the wire-format rule into our internal SecurityPolicy model.
				policies = append(policies, SecurityPolicy{
					SourceZoneID:  srcZone,
					DestZoneID:    dstZone,
					Priority:      prio,
					Action:        rule.Set.Action,
					RuleState:     rule.Misc.Rule,
					Logging:       rule.Misc.Logging,
					LogPriority:   rule.Misc.LoggingPriority.String(),
					Comment:       rule.Comment,
					ACL:           rule.Match.ACL,
					SrcIP:         pipeToComma(rule.Match.SrcIP),
					DstIP:         pipeToComma(rule.Match.DstIP),
					EitherIP:      pipeToComma(rule.Match.EitherIP),
					SrcPort:       pipeToComma(rule.Match.SrcPort),
					DstPort:       pipeToComma(rule.Match.DstPort),
					EitherPort:    pipeToComma(rule.Match.EitherPort),
					Protocol:      rule.Match.Protocol,
					Application:   rule.Match.Application,
					AppGroup:      rule.Match.AppGroup,
					SrcDNS:        rule.Match.SrcDNS,
					DstDNS:        rule.Match.DstDNS,
					EitherDNS:     rule.Match.EitherDNS,
					SrcGeo:        rule.Match.SrcGeo,
					DstGeo:        rule.Match.DstGeo,
					EitherGeo:     rule.Match.EitherGeo,
					SrcService:    rule.Match.SrcService,
					DstService:    rule.Match.DstService,
					EitherService: rule.Match.EitherService,
					DSCP:               rule.Match.DSCP,
					VLAN:               rule.Match.VLAN,
					Overlay:            rule.Match.Overlay,
					SrcAddressGroup:    rule.Match.SrcAddressGroup,
					DstAddressGroup:    rule.Match.DstAddressGroup,
					EitherAddressGroup: rule.Match.EitherAddressGroup,
				})
			}
		}
	}

	// Sort policies by source zone, then destination zone, then priority
	// for consistent ordering.
	sort.Slice(policies, func(i, j int) bool {
		if policies[i].SourceZoneID != policies[j].SourceZoneID {
			return policies[i].SourceZoneID < policies[j].SourceZoneID
		}
		if policies[i].DestZoneID != policies[j].DestZoneID {
			return policies[i].DestZoneID < policies[j].DestZoneID
		}
		return policies[i].Priority < policies[j].Priority
	})

	return policies, nil
}

// parseZonePair splits a zone pair key like "20_21" into source and destination
// zone IDs. Returns (0, 0, false) if the key is not in the expected format.
func parseZonePair(key string) (src, dst int, ok bool) {
	parts := splitExact(key, "_", 2)
	if parts == nil {
		return 0, 0, false
	}
	src, err1 := strconv.Atoi(parts[0])
	dst, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return src, dst, true
}

// splitExact splits a string by a separator into exactly n parts.
// Returns nil if the string does not contain exactly n-1 separators.
// This is used instead of strings.SplitN to enforce exact part counts.
func splitExact(s, sep string, n int) []string {
	parts := make([]string, 0, n)
	idx := 0
	for i := 0; i < n-1; i++ {
		j := indexOf(s[idx:], sep)
		if j < 0 {
			return nil
		}
		parts = append(parts, s[idx:idx+j])
		idx += j + len(sep)
	}
	parts = append(parts, s[idx:])
	return parts
}

// indexOf returns the index of the first occurrence of sep in s, or -1 if not found.
func indexOf(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

// GetSecurityPolicy returns a single policy identified by its segment pair,
// source zone, destination zone, and priority.
//
// Returns (nil, nil) if the policy does not exist — this allows the Terraform
// Read method to detect deleted resources and remove them from state.
func (c *Client) GetSecurityPolicy(segmentPair string, srcZone, dstZone, priority int) (*SecurityPolicy, error) {
	policies, err := c.GetSecurityPolicies(segmentPair)
	if err != nil {
		return nil, err
	}

	for _, p := range policies {
		if p.SourceZoneID == srcZone && p.DestZoneID == dstZone && p.Priority == priority {
			return &p, nil
		}
	}

	return nil, nil
}

// ===========================================================================
// POST /gms/rest/vrf/config/securityPolicies?map=<segmentPair>
// ===========================================================================

// policyRulePost is the POST wire format for a policy rule. It differs from
// the GET format in that:
//   - It has no "self" field
//   - logging_priority is a string instead of json.Number
type policyRulePost struct {
	Match     policyRuleMatch    `json:"match"`
	Misc      policyRuleMiscPost `json:"misc"`
	Comment   string             `json:"comment"`
	GMSMarked bool               `json:"gms_marked"`
	Set       policyRuleSet      `json:"set"`
}

// policyRuleMiscPost is the POST variant of policyRuleMisc with logging_priority
// as a string.
type policyRuleMiscPost struct {
	Rule            string `json:"rule"`
	Logging         string `json:"logging"`
	LoggingPriority string `json:"logging_priority"`
}

// buildSecurityPoliciesPayload reconstructs the deeply nested API format from a
// flat slice of SecurityPolicy structs. This is the inverse of
// parseSecurityPoliciesResponse.
//
// The output structure is:
//
//	{
//	  "data": {
//	    "map1": {
//	      "<srcZone>_<dstZone>": {
//	        "prio": {
//	          "<priority>": { match, misc, set, comment }
//	        }
//	      }
//	    }
//	  },
//	  "options": { "merge": false, "templateApply": false },
//	  "settings": { "map1": { "logging": { "imp_fw_drop": "2" } } }
//	}
//
// The "merge: false" option tells the Orchestrator to replace all policies
// for this segment pair (not merge with existing ones).
func buildSecurityPoliciesPayload(policies []SecurityPolicy) map[string]interface{} {
	// Group policies by zone pair for the nested structure.
	type zonePairKey struct{ src, dst int }
	grouped := make(map[zonePairKey]map[int]SecurityPolicy)
	for _, p := range policies {
		k := zonePairKey{p.SourceZoneID, p.DestZoneID}
		if grouped[k] == nil {
			grouped[k] = make(map[int]SecurityPolicy)
		}
		grouped[k][p.Priority] = p
	}

	// Build the "map1" structure with zone pair keys and priority sub-maps.
	map1 := make(map[string]interface{})

	for zp, rules := range grouped {
		zpKey := fmt.Sprintf("%d_%d", zp.src, zp.dst)
		prioMap := make(map[string]interface{})
		for prio, rule := range rules {
			prioMap[strconv.Itoa(prio)] = policyRulePost{
				Match: policyRuleMatch{
					ACL:           rule.ACL,
					SrcIP:         commaToPipe(rule.SrcIP),
					DstIP:         commaToPipe(rule.DstIP),
					EitherIP:      commaToPipe(rule.EitherIP),
					SrcPort:       commaToPipe(rule.SrcPort),
					DstPort:       commaToPipe(rule.DstPort),
					EitherPort:    commaToPipe(rule.EitherPort),
					Protocol:      rule.Protocol,
					Application:   rule.Application,
					AppGroup:      rule.AppGroup,
					SrcDNS:        rule.SrcDNS,
					DstDNS:        rule.DstDNS,
					EitherDNS:     rule.EitherDNS,
					SrcGeo:        rule.SrcGeo,
					DstGeo:        rule.DstGeo,
					EitherGeo:     rule.EitherGeo,
					SrcService:    rule.SrcService,
					DstService:    rule.DstService,
					EitherService: rule.EitherService,
					DSCP:               rule.DSCP,
					VLAN:               rule.VLAN,
					Overlay:            rule.Overlay,
					SrcAddressGroup:    rule.SrcAddressGroup,
					DstAddressGroup:    rule.DstAddressGroup,
					EitherAddressGroup: rule.EitherAddressGroup,
				},
				Misc:      policyRuleMiscPost{Rule: rule.RuleState, Logging: rule.Logging, LoggingPriority: rule.LogPriority},
				Comment:   rule.Comment,
				GMSMarked: true, // Mark as GMS-managed so the Orchestrator tracks it
				Set:       policyRuleSet{Action: rule.Action},
			}
		}
		map1[zpKey] = map[string]interface{}{
			"prio": prioMap,
		}
	}

	return map[string]interface{}{
		"data": map[string]interface{}{
			"map1": map1,
		},
		"options": map[string]interface{}{
			"merge":         false, // Replace all policies, don't merge
			"templateApply": false, // Not a template application
		},
		"settings": map[string]interface{}{
			"map1": map[string]interface{}{
				"logging": map[string]interface{}{
					"imp_fw_drop": "2", // Default logging level for implicit deny
				},
			},
		},
	}
}

// setSecurityPolicies posts the complete policy set for a segment pair to the
// Orchestrator. This replaces ALL policies for the given segment pair.
//
// Before posting, zone IDs are translated from Default VRF numbering to the
// VRF-specific numbering required by the API.
func (c *Client) setSecurityPolicies(segmentPair string, policies []SecurityPolicy) error {
	// Parse the segment pair (e.g. "0_1") to determine source and destination VRF IDs.
	srcVRF, dstVRF, ok := parseZonePair(segmentPair)
	if !ok {
		return fmt.Errorf("invalid segment pair %q", segmentPair)
	}

	// Translate Default VRF zone IDs to the target VRF-specific IDs.
	translated, err := c.translatePoliciesToVRF(policies, srcVRF, dstVRF)
	if err != nil {
		return fmt.Errorf("error translating zone IDs for segment %s: %w", segmentPair, err)
	}

	// Build the deeply nested payload and POST it.
	payload := buildSecurityPoliciesPayload(translated)
	path := fmt.Sprintf("/gms/rest/vrf/config/securityPolicies?map=%s", segmentPair)
	respBody, statusCode, err := c.doRequest("POST", path, payload)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated && statusCode != http.StatusNoContent {
		return fmt.Errorf("POST /vrf/config/securityPolicies returned status %d: %s", statusCode, string(respBody))
	}

	return nil
}

// CreateSecurityPolicy adds a new policy rule to a segment pair using the
// read-modify-write pattern:
//  1. Check the local registry for duplicate policies (within this Terraform run).
//  2. Fetch all existing policies for the segment pair from the API.
//  3. Check for conflicts with existing API policies.
//  4. Append the new policy and POST the complete set back.
//  5. Register the policy for duplicate detection.
//  6. Read back the created policy to confirm.
//
// The operation is serialized via policyMu to prevent race conditions.
func (c *Client) CreateSecurityPolicy(segmentPair string, policy SecurityPolicy) (*SecurityPolicy, error) {
	c.policyMu.Lock()
	defer c.policyMu.Unlock()

	// Build the unique key for this policy (segment pair + zone pair + priority).
	key := policyKey{
		SegmentPair: segmentPair,
		SrcZone:     policy.SourceZoneID,
		DstZone:     policy.DestZoneID,
		Priority:    policy.Priority,
	}

	// Check the local registry for duplicates within this Terraform apply run.
	// This catches cases where the same priority is used in multiple resource
	// blocks before any of them hit the API.
	if c.policyRegistry == nil {
		c.policyRegistry = make(map[policyKey]string)
	}
	if existing, ok := c.policyRegistry[key]; ok {
		return nil, fmt.Errorf(
			"duplicate policy detected: segment_pair=%s, zone_pair=%d_%d, priority=%d is already used by %s. "+
				"Each priority must be unique per zone pair and segment pair",
			segmentPair, policy.SourceZoneID, policy.DestZoneID, policy.Priority, existing)
	}

	// Check the API for an existing policy at the same zone pair + priority.
	existingPolicies, err := c.GetSecurityPolicies(segmentPair)
	if err != nil {
		return nil, fmt.Errorf("error fetching existing policies: %w", err)
	}
	for _, p := range existingPolicies {
		if p.SourceZoneID == policy.SourceZoneID && p.DestZoneID == policy.DestZoneID && p.Priority == policy.Priority {
			return nil, fmt.Errorf("policy already exists on Orchestrator for zone pair %d_%d at priority %d", policy.SourceZoneID, policy.DestZoneID, policy.Priority)
		}
	}

	// Append the new policy and POST the complete set.
	all := append(existingPolicies, policy)
	if err := c.setSecurityPolicies(segmentPair, all); err != nil {
		return nil, err
	}

	// Register the policy for duplicate detection in subsequent creates.
	c.policyRegistry[key] = fmt.Sprintf("zone_pair=%d_%d/priority=%d", policy.SourceZoneID, policy.DestZoneID, policy.Priority)

	// Read back the created policy to get the authoritative state.
	created, err := c.GetSecurityPolicy(segmentPair, policy.SourceZoneID, policy.DestZoneID, policy.Priority)
	if err != nil {
		return nil, fmt.Errorf("policy was created but could not be read back: %w", err)
	}
	if created == nil {
		return &policy, nil
	}

	return created, nil
}

// UpdateSecurityPolicy updates an existing policy rule in place. It finds the
// matching policy by zone pair + priority and replaces it with the new values.
//
// If the policy is not found in the existing set, it is appended (upsert behavior).
func (c *Client) UpdateSecurityPolicy(segmentPair string, policy SecurityPolicy) (*SecurityPolicy, error) {
	c.policyMu.Lock()
	defer c.policyMu.Unlock()

	existing, err := c.GetSecurityPolicies(segmentPair)
	if err != nil {
		return nil, fmt.Errorf("error fetching existing policies: %w", err)
	}

	// Find and replace the matching policy in the existing set.
	found := false
	for i, p := range existing {
		if p.SourceZoneID == policy.SourceZoneID && p.DestZoneID == policy.DestZoneID && p.Priority == policy.Priority {
			existing[i] = policy
			found = true
			break
		}
	}
	if !found {
		existing = append(existing, policy)
	}

	if err := c.setSecurityPolicies(segmentPair, existing); err != nil {
		return nil, err
	}

	// Read back the updated policy.
	updated, err := c.GetSecurityPolicy(segmentPair, policy.SourceZoneID, policy.DestZoneID, policy.Priority)
	if err != nil {
		return nil, fmt.Errorf("policy was updated but could not be read back: %w", err)
	}
	if updated == nil {
		return &policy, nil
	}

	return updated, nil
}

// DeleteSecurityPolicy removes a policy rule from a segment pair by filtering
// it out of the complete policy set and posting the remaining policies back.
//
// The policy is also removed from the local duplicate detection registry.
func (c *Client) DeleteSecurityPolicy(segmentPair string, srcZone, dstZone, priority int) error {
	c.policyMu.Lock()
	defer c.policyMu.Unlock()

	existing, err := c.GetSecurityPolicies(segmentPair)
	if err != nil {
		return fmt.Errorf("error fetching policies for deletion: %w", err)
	}

	// Build a new slice excluding the policy to be deleted.
	remaining := make([]SecurityPolicy, 0, len(existing))
	for _, p := range existing {
		if p.SourceZoneID == srcZone && p.DestZoneID == dstZone && p.Priority == priority {
			continue // Skip the policy to be deleted
		}
		remaining = append(remaining, p)
	}

	// Remove from the local duplicate detection registry.
	if c.policyRegistry != nil {
		delete(c.policyRegistry, policyKey{
			SegmentPair: segmentPair,
			SrcZone:     srcZone,
			DstZone:     dstZone,
			Priority:    priority,
		})
	}

	return c.setSecurityPolicies(segmentPair, remaining)
}

// GetSecurityPolicySegments returns all available VRF segment pairs that have
// security policies configured.
//
// API endpoint: GET /gms/rest/vrf/config/securityPoliciesSegments
// Response format: ["0_0", "0_1", ...]
func (c *Client) GetSecurityPolicySegments() ([]string, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/vrf/config/securityPoliciesSegments", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /vrf/config/securityPoliciesSegments returned status %d: %s", statusCode, string(respBody))
	}

	var segments []string
	if err := json.Unmarshal(respBody, &segments); err != nil {
		return nil, fmt.Errorf("error unmarshaling securityPoliciesSegments response: %w", err)
	}

	return segments, nil
}
