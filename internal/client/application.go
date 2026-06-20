package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ===========================================================================
// Application Definitions
//
// The Aruba SD-WAN Orchestrator supports several types of application
// definitions that control how network traffic is classified. These
// classifications are used in security policies and traffic steering rules.
//
// Application definition types managed by this client:
//   - Port/Protocol:  Classify traffic by port number and IP protocol (TCP/UDP).
//   - DNS:            Classify traffic by DNS domain name patterns.
//   - Compound:       Classify traffic by combining multiple criteria (IP, port,
//                     protocol, DNS, geo, service, DSCP, VLAN).
//   - IP Intelligence: Classify traffic by IP address ranges with metadata
//                     (country, organization).
//   - SaaS:           Classify traffic for SaaS/Cloud applications by subnet
//                     addresses, domains, and ports.
//
// Application Groups (tags) can group multiple applications together for use
// in security policies.
// ===========================================================================

// PortProtocolClassification represents a port/protocol-based application
// classification. This is the simplest classification type — it matches traffic
// based on a specific port number and IP protocol (e.g. TCP port 8443).
type PortProtocolClassification struct {
	Name        string // Display name of the application
	Port        int    // Port number (e.g. 8443)
	Protocol    int    // IP protocol number (6 = TCP, 17 = UDP, 1 = ICMP)
	Description string // Optional description
	Priority    int    // Classification confidence/priority (0-100)
	Disabled    bool   // Whether this classification is currently disabled
}

// ApplicationGroup represents an application group (also called a "tag") that
// contains a list of application names. Groups are used in security policies
// to match multiple applications with a single rule (e.g. "Social Media"
// group containing "Facebook", "Twitter", "Instagram").
type ApplicationGroup struct {
	Name        string   // Unique group name (also serves as the identifier)
	Apps        []string // List of application names belonging to this group
	ParentGroup []string // Optional parent group references for hierarchical grouping
}

// ===========================================================================
// Port/Protocol Classifications — /applicationDefinition/portProtocolClassification
// ===========================================================================

// appDefAPIEntry is the JSON wire format for a single port/protocol classification
// entry as returned by the GET endpoint. Uses json.Number for numeric fields
// because the API returns them as strings in some contexts.
type appDefAPIEntry struct {
	Port        json.Number `json:"port"`
	Protocol    json.Number `json:"protocol"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Priority    json.Number `json:"priority"`
	Disabled    bool        `json:"disabled"`
}

// appDefPostBody is the JSON body for POST requests to create or update a
// port/protocol classification. Uses concrete int types instead of json.Number.
type appDefPostBody struct {
	Name        string `json:"name"`
	Port        int    `json:"port"`
	Protocol    int    `json:"protocol"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	Disabled    bool   `json:"disabled"`
}

// GetPortProtocolClassifications retrieves all user-defined port/protocol
// classifications from the Orchestrator.
//
// API endpoint: GET /gms/rest/applicationDefinition?base=portProtocolClassification&resourceKey=userDefined
//
// The API response format is a JSON object keyed by port number, where each
// value is an array of classification entries:
//
//	{"8443": [{"port":"8443","protocol":6,"name":"MyWebApp",...}], ...}
//
// When no user-defined classifications exist, the API returns:
//
//	{"result": "Not found"}
//
// The returned slice is sorted by port number, then by protocol number.
func (c *Client) GetPortProtocolClassifications() ([]PortProtocolClassification, error) {
	respBody, statusCode, err := c.doRequest("GET",
		"/gms/rest/applicationDefinition?base=portProtocolClassification&resourceKey=userDefined", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /applicationDefinition?base=portProtocolClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	// Check for the "Not found" response that indicates an empty state.
	// This is a quirk of the API — instead of returning an empty object {},
	// it returns {"result": "Not found"}.
	var notFound struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(respBody, &notFound) == nil && strings.EqualFold(notFound.Result, "Not found") {
		return nil, nil
	}

	// Parse the map-of-arrays response format.
	var raw map[string][]appDefAPIEntry
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling application definitions response: %w", err)
	}

	// Convert API entries to our internal model, handling json.Number → int conversion.
	var defs []PortProtocolClassification
	for _, entries := range raw {
		for _, e := range entries {
			port, _ := strconv.Atoi(e.Port.String())
			proto, _ := strconv.Atoi(e.Protocol.String())
			prio, _ := strconv.Atoi(e.Priority.String())
			defs = append(defs, PortProtocolClassification{
				Name:        e.Name,
				Port:        port,
				Protocol:    proto,
				Description: e.Description,
				Priority:    prio,
				Disabled:    e.Disabled,
			})
		}
	}

	// Sort by port, then by protocol for consistent ordering.
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].Port != defs[j].Port {
			return defs[i].Port < defs[j].Port
		}
		return defs[i].Protocol < defs[j].Protocol
	})

	return defs, nil
}

// GetPortProtocolClassification retrieves a single port/protocol classification
// by its port and protocol numbers. Returns (nil, nil) when the classification
// does not exist — this allows the Terraform Read method to detect deleted resources.
func (c *Client) GetPortProtocolClassification(port, protocol int) (*PortProtocolClassification, error) {
	defs, err := c.GetPortProtocolClassifications()
	if err != nil {
		return nil, err
	}

	for _, d := range defs {
		if d.Port == port && d.Protocol == protocol {
			return &d, nil
		}
	}

	return nil, nil
}

// CreatePortProtocolClassification creates a new port/protocol classification
// on the Orchestrator.
//
// API endpoint: POST /gms/rest/applicationDefinition/portProtocolClassification?port=<port>&protocol=<proto>
//
// The operation is serialized via appDefMu to prevent concurrent modifications.
func (c *Client) CreatePortProtocolClassification(def PortProtocolClassification) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	body := appDefPostBody{
		Name:        def.Name,
		Port:        def.Port,
		Protocol:    def.Protocol,
		Description: def.Description,
		Priority:    def.Priority,
		Disabled:    def.Disabled,
	}

	path := fmt.Sprintf("/gms/rest/applicationDefinition/portProtocolClassification?port=%d&protocol=%d",
		def.Port, def.Protocol)
	respBody, statusCode, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("POST /applicationDefinition/portProtocolClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// UpdatePortProtocolClassification updates an existing port/protocol classification.
// The API uses the same POST endpoint for both create and update (upsert behavior).
//
// API endpoint: POST /gms/rest/applicationDefinition/portProtocolClassification?port=<port>&protocol=<proto>
func (c *Client) UpdatePortProtocolClassification(def PortProtocolClassification) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	body := appDefPostBody{
		Name:        def.Name,
		Port:        def.Port,
		Protocol:    def.Protocol,
		Description: def.Description,
		Priority:    def.Priority,
		Disabled:    def.Disabled,
	}

	path := fmt.Sprintf("/gms/rest/applicationDefinition/portProtocolClassification?port=%d&protocol=%d",
		def.Port, def.Protocol)
	respBody, statusCode, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("POST /applicationDefinition/portProtocolClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// DeletePortProtocolClassification deletes a port/protocol classification
// identified by its port and protocol numbers.
//
// API endpoint: DELETE /gms/rest/applicationDefinition/portProtocolClassification?port=<port>&protocol=<proto>
func (c *Client) DeletePortProtocolClassification(port, protocol int) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	path := fmt.Sprintf("/gms/rest/applicationDefinition/portProtocolClassification?port=%d&protocol=%d",
		port, protocol)
	respBody, statusCode, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("DELETE /applicationDefinition/portProtocolClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// ===========================================================================
// Application Groups — /applicationDefinition/applicationTags
//
// Application groups use a "full payload replacement" pattern similar to
// security zones: the POST body must contain ALL groups. To add, update,
// or delete a single group, the client fetches all groups, modifies the
// set locally, and POSTs the complete set back.
// ===========================================================================

// appGroupAPIEntry is the JSON wire format for a single application group
// as returned by the GET endpoint.
type appGroupAPIEntry struct {
	Apps        []string `json:"apps"`        // List of application names in this group
	ParentGroup []string `json:"parentGroup"` // Parent group references (may be null)
}

// GetApplicationGroups retrieves all user-defined application groups from the
// Orchestrator.
//
// API endpoint: GET /gms/rest/applicationDefinition/applicationTags?resourceKey=userDefined
//
// The API response is a JSON object keyed by group name:
//
//	{"Social Media": {"apps": ["Facebook", "Twitter"], "parentGroup": null}, ...}
//
// When no groups exist, the API returns: {"result": "Not found"}
//
// The returned slice is sorted alphabetically by group name.
func (c *Client) GetApplicationGroups() ([]ApplicationGroup, error) {
	respBody, statusCode, err := c.doRequest("GET",
		"/gms/rest/applicationDefinition/applicationTags?resourceKey=userDefined", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /applicationDefinition/applicationTags returned status %d: %s",
			statusCode, string(respBody))
	}

	// Check for the "Not found" response (empty state).
	var notFound struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(respBody, &notFound) == nil && strings.EqualFold(notFound.Result, "Not found") {
		return nil, nil
	}

	// Parse the map response: keys are group names, values are group details.
	var raw map[string]appGroupAPIEntry
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling application groups response: %w", err)
	}

	var groups []ApplicationGroup
	for name, entry := range raw {
		// Normalize nil slices to empty slices to avoid null in JSON output.
		apps := entry.Apps
		if apps == nil {
			apps = []string{}
		}
		parentGroup := entry.ParentGroup
		if parentGroup == nil {
			parentGroup = []string{}
		}
		groups = append(groups, ApplicationGroup{
			Name:        name,
			Apps:        apps,
			ParentGroup: parentGroup,
		})
	}

	// Sort alphabetically by name for consistent ordering.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return groups, nil
}

// GetApplicationGroup retrieves a single application group by name.
// Returns (nil, nil) when the group does not exist.
func (c *Client) GetApplicationGroup(name string) (*ApplicationGroup, error) {
	groups, err := c.GetApplicationGroups()
	if err != nil {
		return nil, err
	}

	for _, g := range groups {
		if g.Name == name {
			return &g, nil
		}
	}

	return nil, nil
}

// groupsToAPIPayload converts a slice of ApplicationGroup structs into the JSON
// format expected by the POST endpoint. The output is an object keyed by group name:
//
//	{"GroupA": {"apps": ["App1", "App2"]}, "GroupB": {"apps": ["App3"]}}
func groupsToAPIPayload(groups []ApplicationGroup) map[string]appGroupAPIEntry {
	payload := make(map[string]appGroupAPIEntry, len(groups))
	for _, g := range groups {
		apps := g.Apps
		if apps == nil {
			apps = []string{}
		}
		payload[g.Name] = appGroupAPIEntry{
			Apps: apps,
		}
	}
	return payload
}

// postAllApplicationGroups sends the complete set of application groups to the API.
// This replaces ALL groups — any group on the Orchestrator but not in the slice
// will be deleted.
//
// API endpoint: POST /gms/rest/applicationDefinition/applicationTags
func (c *Client) postAllApplicationGroups(groups []ApplicationGroup) error {
	payload := groupsToAPIPayload(groups)
	respBody, statusCode, err := c.doRequest("POST",
		"/gms/rest/applicationDefinition/applicationTags", payload)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("POST /applicationDefinition/applicationTags returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// CreateApplicationGroup creates a new application group using the read-modify-write
// pattern:
//  1. Fetch all existing groups.
//  2. Check for name conflicts.
//  3. Append the new group to the list.
//  4. POST the complete set back.
//
// The operation is serialized via appGroupMu to prevent race conditions when
// Terraform creates multiple groups concurrently.
func (c *Client) CreateApplicationGroup(group ApplicationGroup) error {
	c.appGroupMu.Lock()
	defer c.appGroupMu.Unlock()

	existing, err := c.GetApplicationGroups()
	if err != nil {
		return fmt.Errorf("error fetching existing application groups: %w", err)
	}

	// Check for naming conflict before attempting creation.
	for _, g := range existing {
		if g.Name == group.Name {
			return fmt.Errorf("application group %q already exists", group.Name)
		}
	}

	all := append(existing, group)
	return c.postAllApplicationGroups(all)
}

// UpdateApplicationGroup updates an existing application group by replacing it
// in the complete group set. If the group is not found, it is appended (upsert).
func (c *Client) UpdateApplicationGroup(group ApplicationGroup) error {
	c.appGroupMu.Lock()
	defer c.appGroupMu.Unlock()

	existing, err := c.GetApplicationGroups()
	if err != nil {
		return fmt.Errorf("error fetching existing application groups: %w", err)
	}

	// Find and replace the matching group by name.
	found := false
	for i, g := range existing {
		if g.Name == group.Name {
			existing[i] = group
			found = true
			break
		}
	}
	if !found {
		existing = append(existing, group)
	}

	return c.postAllApplicationGroups(existing)
}

// DeleteApplicationGroup removes an application group by filtering it out of
// the complete group set and posting the remaining groups back.
func (c *Client) DeleteApplicationGroup(name string) error {
	c.appGroupMu.Lock()
	defer c.appGroupMu.Unlock()

	existing, err := c.GetApplicationGroups()
	if err != nil {
		return fmt.Errorf("error fetching application groups for deletion: %w", err)
	}

	// Build a new slice excluding the group to be deleted.
	remaining := make([]ApplicationGroup, 0, len(existing))
	for _, g := range existing {
		if g.Name == name {
			continue
		}
		remaining = append(remaining, g)
	}

	return c.postAllApplicationGroups(remaining)
}

// ===========================================================================
// DNS Classification — /applicationDefinition/dnsClassification
//
// DNS classifications match traffic based on DNS domain name patterns.
// This allows classifying traffic to specific domains (e.g. "*.google.com")
// as a named application for use in security policies.
// ===========================================================================

// DNSClassification represents a DNS domain-based application classification.
type DNSClassification struct {
	Name        string // Display name of the application
	Domain      string // DNS domain pattern (e.g. "*.example.com") — serves as the unique key
	Description string // Optional description
	Priority    int    // Classification confidence/priority
	Disabled    bool   // Whether this classification is currently disabled
}

// dnsClassAPIEntry is the JSON wire format for a single DNS classification entry.
type dnsClassAPIEntry struct {
	Name        string      `json:"name"`
	Domain      string      `json:"domain"`
	Description string      `json:"description"`
	Priority    json.Number `json:"priority"`
	Disabled    bool        `json:"disabled"`
}

// dnsClassPostBody is the JSON body for POST requests to create or update
// a DNS classification.
type dnsClassPostBody struct {
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	Disabled    bool   `json:"disabled"`
}

// GetDNSClassifications retrieves all user-defined DNS classification entries
// from the Orchestrator.
//
// API endpoint: GET /gms/rest/applicationDefinition?base=dnsClassification&resourceKey=userDefined
//
// The API returns a JSON array of classification entries. When empty, it returns
// {"result": "Not found"}.
func (c *Client) GetDNSClassifications() ([]DNSClassification, error) {
	respBody, statusCode, err := c.doRequest("GET",
		"/gms/rest/applicationDefinition?base=dnsClassification&resourceKey=userDefined", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET dnsClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	// Check for the "Not found" response (empty state).
	var notFound struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(respBody, &notFound) == nil && strings.EqualFold(notFound.Result, "Not found") {
		return nil, nil
	}

	// Parse the array of DNS classification entries.
	var raw []dnsClassAPIEntry
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling DNS classifications response: %w", err)
	}

	var defs []DNSClassification
	for _, e := range raw {
		prio, _ := strconv.Atoi(e.Priority.String())
		defs = append(defs, DNSClassification{
			Name:        e.Name,
			Domain:      e.Domain,
			Description: e.Description,
			Priority:    prio,
			Disabled:    e.Disabled,
		})
	}

	return defs, nil
}

// GetDNSClassification retrieves a single DNS classification by its domain pattern.
// Returns (nil, nil) when the classification does not exist.
func (c *Client) GetDNSClassification(domain string) (*DNSClassification, error) {
	defs, err := c.GetDNSClassifications()
	if err != nil {
		return nil, err
	}

	for _, d := range defs {
		if d.Domain == domain {
			return &d, nil
		}
	}

	return nil, nil
}

// CreateDNSClassification creates a new DNS domain classification.
// The domain is URL-encoded in the query parameter.
//
// API endpoint: POST /gms/rest/applicationDefinition/dnsClassification?domain=<domain>
func (c *Client) CreateDNSClassification(def DNSClassification) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	body := dnsClassPostBody{
		Name:        def.Name,
		Domain:      def.Domain,
		Description: def.Description,
		Priority:    def.Priority,
		Disabled:    def.Disabled,
	}

	path := fmt.Sprintf("/gms/rest/applicationDefinition/dnsClassification?domain=%s",
		url.QueryEscape(def.Domain))
	respBody, statusCode, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("POST dnsClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// UpdateDNSClassification updates an existing DNS classification.
// Uses the same POST endpoint as Create (upsert behavior).
//
// API endpoint: POST /gms/rest/applicationDefinition/dnsClassification?domain=<domain>
func (c *Client) UpdateDNSClassification(def DNSClassification) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	body := dnsClassPostBody{
		Name:        def.Name,
		Domain:      def.Domain,
		Description: def.Description,
		Priority:    def.Priority,
		Disabled:    def.Disabled,
	}

	path := fmt.Sprintf("/gms/rest/applicationDefinition/dnsClassification?domain=%s",
		url.QueryEscape(def.Domain))
	respBody, statusCode, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("POST dnsClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// DeleteDNSClassification deletes a DNS classification by its domain pattern.
//
// API endpoint: DELETE /gms/rest/applicationDefinition/dnsClassification?domain=<domain>
func (c *Client) DeleteDNSClassification(domain string) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	path := fmt.Sprintf("/gms/rest/applicationDefinition/dnsClassification?domain=%s",
		url.QueryEscape(domain))
	respBody, statusCode, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("DELETE dnsClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// ===========================================================================
// Compound Classification — /applicationDefinition/compoundClassification
//
// Compound classifications combine multiple match criteria into a single
// application definition. A compound classification can match on any
// combination of: protocol, source/destination IP, port, DNS, geolocation,
// service, DSCP, and VLAN.
//
// This provides more granular traffic classification than simple port/protocol
// or DNS-based definitions.
// ===========================================================================

// CompoundClassification represents a multi-criteria application classification.
// Multiple match fields can be set simultaneously to create precise traffic
// matching rules.
type CompoundClassification struct {
	ID            int    // Unique numeric ID assigned by the Orchestrator
	Name          string // Display name of the application
	Description   string // Optional description
	Confidence    int    // Classification confidence level (0-100)
	Disabled      bool   // Whether this classification is currently disabled
	Protocol      string // Protocol match (e.g. "tcp", "udp")
	SrcIP         string // Source IP/CIDR match (e.g. "10.0.0.0/8")
	DstIP         string // Destination IP/CIDR match
	EitherIP      string // Bidirectional IP match
	SrcPort       string // Source port match
	DstPort       string // Destination port match
	EitherPort    string // Bidirectional port match
	SrcDNS        string // Source DNS hostname match
	DstDNS        string // Destination DNS hostname match
	EitherDNS     string // Bidirectional DNS match
	SrcGeo        string // Source geolocation (country code)
	DstGeo        string // Destination geolocation
	EitherGeo     string // Bidirectional geolocation
	SrcService    string // Source service match
	DstService    string // Destination service match
	EitherService string // Bidirectional service match
	DSCP          string // DSCP value match
	VLAN          string // VLAN/interface match
}

// compoundClassAPIEntry is the JSON wire format for a compound classification
// entry returned by the GET endpoint.
type compoundClassAPIEntry struct {
	ID            json.Number `json:"id"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	Confidence    json.Number `json:"confidence"`
	Disabled      bool        `json:"disabled"`
	Protocol      string      `json:"protocol"`
	SrcIP         string      `json:"src_ip"`
	DstIP         string      `json:"dst_ip"`
	EitherIP      string      `json:"either_ip"`
	SrcPort       string      `json:"src_port"`
	DstPort       string      `json:"dst_port"`
	EitherPort    string      `json:"either_port"`
	SrcDNS        string      `json:"src_dns"`
	DstDNS        string      `json:"dst_dns"`
	EitherDNS     string      `json:"either_dns"`
	SrcGeo        string      `json:"src_geo"`
	DstGeo        string      `json:"dst_geo"`
	EitherGeo     string      `json:"either_geo"`
	SrcService    string      `json:"src_service"`
	DstService    string      `json:"dst_service"`
	EitherService string      `json:"either_service"`
	DSCP          string      `json:"dscp"`
	VLAN          string      `json:"vlan"`
}

// compoundClassPostBody is the JSON body for POST requests to create or update
// a compound classification.
type compoundClassPostBody struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Confidence    int    `json:"confidence"`
	Disabled      bool   `json:"disabled"`
	Protocol      string `json:"protocol"`
	SrcIP         string `json:"src_ip"`
	DstIP         string `json:"dst_ip"`
	EitherIP      string `json:"either_ip"`
	SrcPort       string `json:"src_port"`
	DstPort       string `json:"dst_port"`
	EitherPort    string `json:"either_port"`
	SrcDNS        string `json:"src_dns"`
	DstDNS        string `json:"dst_dns"`
	EitherDNS     string `json:"either_dns"`
	SrcGeo        string `json:"src_geo"`
	DstGeo        string `json:"dst_geo"`
	EitherGeo     string `json:"either_geo"`
	SrcService    string `json:"src_service"`
	DstService    string `json:"dst_service"`
	EitherService string `json:"either_service"`
	DSCP          string `json:"dscp"`
	VLAN          string `json:"vlan"`
}

// compoundEntryToModel converts a wire-format API entry to the internal
// CompoundClassification model, handling json.Number → int conversions.
func compoundEntryToModel(e compoundClassAPIEntry) CompoundClassification {
	id, _ := strconv.Atoi(e.ID.String())
	conf, _ := strconv.Atoi(e.Confidence.String())
	return CompoundClassification{
		ID:            id,
		Name:          e.Name,
		Description:   e.Description,
		Confidence:    conf,
		Disabled:      e.Disabled,
		Protocol:      e.Protocol,
		SrcIP:         e.SrcIP,
		DstIP:         e.DstIP,
		EitherIP:      e.EitherIP,
		SrcPort:       e.SrcPort,
		DstPort:       e.DstPort,
		EitherPort:    e.EitherPort,
		SrcDNS:        e.SrcDNS,
		DstDNS:        e.DstDNS,
		EitherDNS:     e.EitherDNS,
		SrcGeo:        e.SrcGeo,
		DstGeo:        e.DstGeo,
		EitherGeo:     e.EitherGeo,
		SrcService:    e.SrcService,
		DstService:    e.DstService,
		EitherService: e.EitherService,
		DSCP:          e.DSCP,
		VLAN:          e.VLAN,
	}
}

// compoundModelToPostBody converts the internal CompoundClassification model
// to the JSON POST body format expected by the API.
func compoundModelToPostBody(def CompoundClassification) compoundClassPostBody {
	return compoundClassPostBody{
		ID:            def.ID,
		Name:          def.Name,
		Description:   def.Description,
		Confidence:    def.Confidence,
		Disabled:      def.Disabled,
		Protocol:      def.Protocol,
		SrcIP:         def.SrcIP,
		DstIP:         def.DstIP,
		EitherIP:      def.EitherIP,
		SrcPort:       def.SrcPort,
		DstPort:       def.DstPort,
		EitherPort:    def.EitherPort,
		SrcDNS:        def.SrcDNS,
		DstDNS:        def.DstDNS,
		EitherDNS:     def.EitherDNS,
		SrcGeo:        def.SrcGeo,
		DstGeo:        def.DstGeo,
		EitherGeo:     def.EitherGeo,
		SrcService:    def.SrcService,
		DstService:    def.DstService,
		EitherService: def.EitherService,
		DSCP:          def.DSCP,
		VLAN:          def.VLAN,
	}
}

// GetCompoundClassifications retrieves all user-defined compound classification
// entries from the Orchestrator.
//
// API endpoint: GET /gms/rest/applicationDefinition?base=compoundClassification&resourceKey=userDefined
//
// The API returns a JSON object keyed by classification ID. When empty, returns
// {"result": "Not found"}.
//
// The returned slice is sorted by ID in ascending order.
func (c *Client) GetCompoundClassifications() ([]CompoundClassification, error) {
	respBody, statusCode, err := c.doRequest("GET",
		"/gms/rest/applicationDefinition?base=compoundClassification&resourceKey=userDefined", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET compoundClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	var notFound struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(respBody, &notFound) == nil && strings.EqualFold(notFound.Result, "Not found") {
		return nil, nil
	}

	// Parse the object keyed by classification ID.
	var raw map[string]compoundClassAPIEntry
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling compound classifications response: %w", err)
	}

	var defs []CompoundClassification
	for _, e := range raw {
		defs = append(defs, compoundEntryToModel(e))
	}

	// Sort by ID for consistent ordering.
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].ID < defs[j].ID
	})

	return defs, nil
}

// GetCompoundClassification retrieves a single compound classification by its
// numeric ID. Returns (nil, nil) when the classification does not exist.
func (c *Client) GetCompoundClassification(id int) (*CompoundClassification, error) {
	defs, err := c.GetCompoundClassifications()
	if err != nil {
		return nil, err
	}

	for _, d := range defs {
		if d.ID == id {
			return &d, nil
		}
	}

	return nil, nil
}

// CreateCompoundClassification creates a new compound classification.
// It automatically assigns the next available ID by finding the maximum
// existing ID and incrementing it.
//
// The def parameter is a pointer because the assigned ID is written back to
// the caller's struct.
//
// API endpoint: POST /gms/rest/applicationDefinition/compoundClassification?id=<id>
func (c *Client) CreateCompoundClassification(def *CompoundClassification) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	// Find the next available ID by scanning existing classifications.
	existing, err := c.GetCompoundClassifications()
	if err != nil {
		return fmt.Errorf("error fetching existing compound classifications: %w", err)
	}

	maxID := 0
	for _, e := range existing {
		if e.ID > maxID {
			maxID = e.ID
		}
	}
	def.ID = maxID + 1

	body := compoundModelToPostBody(*def)

	path := fmt.Sprintf("/gms/rest/applicationDefinition/compoundClassification?id=%d", def.ID)
	respBody, statusCode, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("POST compoundClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// UpdateCompoundClassification updates an existing compound classification.
//
// API endpoint: POST /gms/rest/applicationDefinition/compoundClassification?id=<id>
func (c *Client) UpdateCompoundClassification(def CompoundClassification) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	body := compoundModelToPostBody(def)

	path := fmt.Sprintf("/gms/rest/applicationDefinition/compoundClassification?id=%d", def.ID)
	respBody, statusCode, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return fmt.Errorf("POST compoundClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// DeleteCompoundClassification deletes a compound classification by its numeric ID.
//
// API endpoint: DELETE /gms/rest/applicationDefinition/compoundClassification?id=<id>
func (c *Client) DeleteCompoundClassification(id int) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	path := fmt.Sprintf("/gms/rest/applicationDefinition/compoundClassification?id=%d", id)
	respBody, statusCode, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("DELETE compoundClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// ===========================================================================
// Application Wildcard Search — /applicationDefinition/applications/wildcard
//
// Orchestrator-side search endpoint that returns matching application names
// across all sources (built-in + user-defined). Use this for discovery —
// the names returned can then be referenced in security policies.
// ===========================================================================

// appSearchBody is the JSON request body for the wildcard search endpoint.
type appSearchBody struct {
	Pattern string `json:"pattern"`
	Limit   int    `json:"limit"`
}

// SearchApplications performs a server-side wildcard search and returns the
// matching application names. The pattern is a substring match. Limit 0 means
// "no limit" — the Orchestrator returns all matches.
//
// API endpoint: POST /gms/rest/applicationDefinition/applications/wildcard
//
// The API response is a flat JSON array of strings, e.g. ["Skype", "SkypeForBusiness"].
func (c *Client) SearchApplications(pattern string, limit int) ([]string, error) {
	body := appSearchBody{Pattern: pattern, Limit: limit}
	respBody, statusCode, err := c.doRequest("POST",
		"/gms/rest/applicationDefinition/applications/wildcard", body)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /applicationDefinition/applications/wildcard returned status %d: %s",
			statusCode, string(respBody))
	}

	var names []string
	if err := json.Unmarshal(respBody, &names); err != nil {
		return nil, fmt.Errorf("error unmarshaling application search response: %w", err)
	}

	return names, nil
}
