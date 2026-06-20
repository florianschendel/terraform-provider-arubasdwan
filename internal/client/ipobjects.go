package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
)

// ===========================================================================
// IP Objects — /gms/rest/ipObjects/*
//
// IP Objects are reusable named groups managed centrally on the Orchestrator
// and referenced by ACLs and security policies. There are two kinds:
//   - Address Groups (type "AG"): named groups of IP addresses/CIDRs.
//   - Service Groups (type "SG"): named groups of service definitions.
//
// This file currently implements Address Groups only.
//
// Endpoint conventions discovered against the Orchestrator API:
//   - GET    /ipObjects/addressGroup            → list all address groups
//   - GET    /ipObjects/addressGroup?name=<n>   → fetch a single address group
//   - POST   /ipObjects/addressGroup            → create/replace a single group
//                                                 (object name is in the body)
//   - DELETE /ipObjects/addressGroup?name=<n>   → delete a single address group
//
// All operations are serialized via ipObjectMu to prevent concurrent
// modifications from racing.
// ===========================================================================

// IPAddressGroupRule represents a single rule within an address group.
// A group has one or more rules, each contributing IP ranges and/or nested
// group references to the overall set.
type IPAddressGroupRule struct {
	IncludedIPs    []string // Included IP addresses or CIDRs (e.g. "192.168.0.0/24")
	ExcludedIPs    []string // Explicitly excluded IPs/CIDRs
	IncludedGroups []string // Names of nested address groups to include
	Comment        string   // Free-form comment for this rule
}

// IPAddressGroup represents an address group ("AG") on the Orchestrator.
// The Type field is always "AG" for address groups; it is exposed for clarity
// but set automatically by the client.
type IPAddressGroup struct {
	Name  string               // Unique name (also serves as the API identifier)
	Type  string               // Always "AG" for address groups
	Rules []IPAddressGroupRule // One or more rules composing the group
}

// addressGroupRuleAPI is the wire format for a single rule inside an address group.
// Field names match the JSON keys used by the Orchestrator.
type addressGroupRuleAPI struct {
	IncludedIPs    []string `json:"includedIPs"`
	ExcludedIPs    []string `json:"excludedIPs"`
	IncludedGroups []string `json:"includedGroups"`
	Comment        string   `json:"comment"`
}

// addressGroupAPI is the wire format for a single address group.
type addressGroupAPI struct {
	Name  string                `json:"name"`
	Type  string                `json:"type"` // "AG"
	Rules []addressGroupRuleAPI `json:"rules"`
}

// addressGroupAPIToModel converts a wire-format entry into the internal model.
// It normalizes nil slices to empty slices to keep JSON output stable.
func addressGroupAPIToModel(a addressGroupAPI) IPAddressGroup {
	rules := make([]IPAddressGroupRule, 0, len(a.Rules))
	for _, r := range a.Rules {
		rules = append(rules, IPAddressGroupRule{
			IncludedIPs:    nilToEmpty(r.IncludedIPs),
			ExcludedIPs:    nilToEmpty(r.ExcludedIPs),
			IncludedGroups: nilToEmpty(r.IncludedGroups),
			Comment:        r.Comment,
		})
	}
	return IPAddressGroup{
		Name:  a.Name,
		Type:  a.Type,
		Rules: rules,
	}
}

// addressGroupModelToAPI converts the internal model to the wire format expected
// by POST. Type is forced to "AG".
func addressGroupModelToAPI(g IPAddressGroup) addressGroupAPI {
	rules := make([]addressGroupRuleAPI, 0, len(g.Rules))
	for _, r := range g.Rules {
		rules = append(rules, addressGroupRuleAPI{
			IncludedIPs:    nilToEmpty(r.IncludedIPs),
			ExcludedIPs:    nilToEmpty(r.ExcludedIPs),
			IncludedGroups: nilToEmpty(r.IncludedGroups),
			Comment:        r.Comment,
		})
	}
	return addressGroupAPI{
		Name:  g.Name,
		Type:  "AG",
		Rules: rules,
	}
}

// nilToEmpty replaces a nil slice with an empty slice so JSON serializes as []
// rather than null.
func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// GetIPAddressGroups retrieves all address groups from the Orchestrator.
// The result is sorted alphabetically by name for deterministic ordering.
//
// API endpoint: GET /gms/rest/ipObjects/addressGroup
func (c *Client) GetIPAddressGroups() ([]IPAddressGroup, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/ipObjects/addressGroup", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /ipObjects/addressGroup returned status %d: %s",
			statusCode, string(respBody))
	}

	var raw []addressGroupAPI
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling address groups response: %w", err)
	}

	groups := make([]IPAddressGroup, 0, len(raw))
	for _, a := range raw {
		groups = append(groups, addressGroupAPIToModel(a))
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return groups, nil
}

// GetIPAddressGroup retrieves a single address group by name. Returns
// (nil, nil) when the group does not exist, allowing the Terraform Read
// method to detect deleted resources.
//
// API endpoint: GET /gms/rest/ipObjects/addressGroup?name=<n>
func (c *Client) GetIPAddressGroup(name string) (*IPAddressGroup, error) {
	path := fmt.Sprintf("/gms/rest/ipObjects/addressGroup?name=%s", url.QueryEscape(name))
	respBody, statusCode, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, nil
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /ipObjects/addressGroup?name=%s returned status %d: %s",
			name, statusCode, string(respBody))
	}

	// The Orchestrator returns either a single object or, for unknown names, a
	// JSON array (often empty). Try the object form first, then fall back.
	var single addressGroupAPI
	if err := json.Unmarshal(respBody, &single); err == nil && single.Name != "" {
		g := addressGroupAPIToModel(single)
		return &g, nil
	}

	var list []addressGroupAPI
	if err := json.Unmarshal(respBody, &list); err == nil {
		for _, a := range list {
			if a.Name == name {
				g := addressGroupAPIToModel(a)
				return &g, nil
			}
		}
		return nil, nil
	}

	return nil, fmt.Errorf("error unmarshaling single address group response")
}

// CreateIPAddressGroup creates a new address group. The Orchestrator's POST
// endpoint is upsert-style — it will overwrite an existing group with the same
// name. To prevent accidental overwrites, this function checks existence first
// and returns an error when a group with the requested name already exists.
//
// API endpoint: POST /gms/rest/ipObjects/addressGroup
func (c *Client) CreateIPAddressGroup(group IPAddressGroup) error {
	c.ipObjectMu.Lock()
	defer c.ipObjectMu.Unlock()

	existing, err := c.GetIPAddressGroup(group.Name)
	if err != nil {
		return fmt.Errorf("error checking existing address group: %w", err)
	}
	if existing != nil {
		return fmt.Errorf("address group %q already exists", group.Name)
	}

	return c.postIPAddressGroup(group)
}

// UpdateIPAddressGroup replaces an existing address group with the supplied
// definition. The same POST endpoint is used as for Create (upsert behavior).
//
// API endpoint: POST /gms/rest/ipObjects/addressGroup
func (c *Client) UpdateIPAddressGroup(group IPAddressGroup) error {
	c.ipObjectMu.Lock()
	defer c.ipObjectMu.Unlock()

	return c.postIPAddressGroup(group)
}

// postIPAddressGroup sends the address group to the API. Caller must hold the
// ipObjectMu mutex.
func (c *Client) postIPAddressGroup(group IPAddressGroup) error {
	body := addressGroupModelToAPI(group)
	respBody, statusCode, err := c.doRequest("POST", "/gms/rest/ipObjects/addressGroup", body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated && statusCode != http.StatusNoContent {
		return fmt.Errorf("POST /ipObjects/addressGroup returned status %d: %s",
			statusCode, string(respBody))
	}

	return nil
}

// DeleteIPAddressGroup removes an address group by name.
//
// API endpoint: DELETE /gms/rest/ipObjects/addressGroup?name=<n>
func (c *Client) DeleteIPAddressGroup(name string) error {
	c.ipObjectMu.Lock()
	defer c.ipObjectMu.Unlock()

	path := fmt.Sprintf("/gms/rest/ipObjects/addressGroup?name=%s", url.QueryEscape(name))
	respBody, statusCode, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("DELETE /ipObjects/addressGroup?name=%s returned status %d: %s",
			name, statusCode, string(respBody))
	}

	return nil
}
