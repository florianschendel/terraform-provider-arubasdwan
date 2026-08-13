package client

// This file implements the address map application definitions, which the
// Orchestrator API calls IP intelligence classifications:
//
//   - GET    /gms/rest/applicationDefinition?base=ipIntelligenceClassification&resourceKey=userDefined
//   - POST   /gms/rest/applicationDefinition/ipIntelligenceClassification?ipStart=<n>&ipEnd=<n>
//   - DELETE /gms/rest/applicationDefinition/ipIntelligenceClassification?ipStart=<n>&ipEnd=<n>
//
// An address map classifies an IPv4 address range as a named application, so
// policies and overlay ACLs can match traffic to that range by name. The API
// represents addresses as 32-bit unsigned integers and keys the GET response
// by the range's start address; POST acts as an upsert.
//
// Both endpoints behave identically on Orchestrator 9.6.3 and 9.7.0.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strings"
)

// defaultAddressMapSubattributes is what the Orchestrator stores for entries
// created through its own UI. New entries reuse it so they are
// indistinguishable from UI-created ones.
const defaultAddressMapSubattributes = `{"msinstance":"","mscategory":"","proxy":"0"}`

// AddressMap represents one user-defined address map entry: an IPv4 range
// classified as a named application.
type AddressMap struct {
	Name        string // Application name; letters, digits, hyphen, underscore, and period, up to 31 characters
	IPStart     string // First IPv4 address of the range in dotted notation
	IPEnd       string // Last IPv4 address of the range in dotted notation
	Description string // Optional description
	Country     string // Country name associated with the range
	CountryCode string // Two-letter ISO 3166-1 alpha-2 country code
	Org         string // Organization associated with the range
	Priority    int    // Classification priority; higher wins over lower

	// ServiceID is assigned by the Orchestrator and reported back on reads.
	ServiceID int

	// Fields the Orchestrator maintains but this client does not model.
	// They are carried over on updates so an entry keeps whatever the
	// Orchestrator stored for it.
	saasID        int
	search        string
	subattributes string
}

// addressMapAPIEntry is the wire format of one entry.
type addressMapAPIEntry struct {
	IPStart       flexInt64 `json:"ip_start"`
	IPEnd         flexInt64 `json:"ip_end"`
	ServiceID     flexInt   `json:"service_id"`
	SaasID        flexInt   `json:"saas_id"`
	Country       string    `json:"country"`
	CountryCode   string    `json:"country_code"`
	Org           string    `json:"org"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Priority      flexInt   `json:"priority"`
	Search        string    `json:"search"`
	Subattributes string    `json:"subattributes"`
}

// addressMapPostBody is the JSON body for creating or updating an entry.
type addressMapPostBody struct {
	IPStart       uint32 `json:"ip_start"`
	IPEnd         uint32 `json:"ip_end"`
	SaasID        int    `json:"saas_id"`
	Country       string `json:"country"`
	CountryCode   string `json:"country_code"`
	Org           string `json:"org"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Priority      int    `json:"priority"`
	Search        string `json:"search"`
	Subattributes string `json:"subattributes"`
}

// ===========================================================================
// IPv4 address conversion
//
// The API encodes addresses as 32-bit unsigned integers. Callers work with
// dotted notation, which is what users write in Terraform configurations.
// ===========================================================================

// ParseIPv4 converts a dotted IPv4 address into its 32-bit representation.
func ParseIPv4(address string) (uint32, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid IP address", address)
	}
	addr = addr.Unmap()
	if !addr.Is4() {
		return 0, fmt.Errorf("%q is not an IPv4 address; address maps only support IPv4", address)
	}
	octets := addr.As4()
	return uint32(octets[0])<<24 | uint32(octets[1])<<16 | uint32(octets[2])<<8 | uint32(octets[3]), nil
}

// FormatIPv4 renders a 32-bit address in dotted notation.
func FormatIPv4(value uint32) string {
	return netip.AddrFrom4([4]byte{
		byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value),
	}).String()
}

// ===========================================================================
// GET /gms/rest/applicationDefinition?base=ipIntelligenceClassification
// ===========================================================================

// GetAddressMaps retrieves all user-defined address map entries, sorted by
// the start address of their range.
//
// The response is an object keyed by start address; an empty configuration
// yields {"result": "Not found"}.
func (c *Client) GetAddressMaps() ([]AddressMap, error) {
	respBody, statusCode, err := c.doRequest("GET",
		"/gms/rest/applicationDefinition?base=ipIntelligenceClassification&resourceKey=userDefined", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET ipIntelligenceClassification returned status %d: %s",
			statusCode, string(respBody))
	}

	// Empty state: the API reports "Not found" instead of an empty object.
	var notFound struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(respBody, &notFound) == nil && strings.EqualFold(notFound.Result, "Not found") {
		return nil, nil
	}

	var raw map[string]addressMapAPIEntry
	if err := json.Unmarshal(respBody, &raw); err != nil {
		// Some versions may answer with an array instead of a keyed object.
		var list []addressMapAPIEntry
		if err2 := json.Unmarshal(respBody, &list); err2 != nil {
			return nil, fmt.Errorf("error unmarshaling address maps response: %w", err)
		}
		raw = make(map[string]addressMapAPIEntry, len(list))
		for i, entry := range list {
			raw[fmt.Sprintf("%d-%d", int64(entry.IPStart), i)] = entry
		}
	}

	maps := make([]AddressMap, 0, len(raw))
	for _, entry := range raw {
		maps = append(maps, AddressMap{
			Name:          entry.Name,
			IPStart:       FormatIPv4(uint32(entry.IPStart)),
			IPEnd:         FormatIPv4(uint32(entry.IPEnd)),
			Description:   entry.Description,
			Country:       entry.Country,
			CountryCode:   entry.CountryCode,
			Org:           entry.Org,
			Priority:      int(entry.Priority),
			ServiceID:     int(entry.ServiceID),
			saasID:        int(entry.SaasID),
			search:        entry.Search,
			subattributes: entry.Subattributes,
		})
	}

	sort.Slice(maps, func(i, j int) bool {
		a, _ := ParseIPv4(maps[i].IPStart)
		b, _ := ParseIPv4(maps[j].IPStart)
		if a != b {
			return a < b
		}
		return maps[i].Name < maps[j].Name
	})
	return maps, nil
}

// GetAddressMap retrieves a single address map by its IP range. Returns
// (nil, nil) when no entry covers exactly that range.
func (c *Client) GetAddressMap(ipStart, ipEnd string) (*AddressMap, error) {
	start, err := ParseIPv4(ipStart)
	if err != nil {
		return nil, err
	}
	end, err := ParseIPv4(ipEnd)
	if err != nil {
		return nil, err
	}

	maps, err := c.GetAddressMaps()
	if err != nil {
		return nil, err
	}
	for i := range maps {
		s, _ := ParseIPv4(maps[i].IPStart)
		e, _ := ParseIPv4(maps[i].IPEnd)
		if s == start && e == end {
			return &maps[i], nil
		}
	}
	return nil, nil
}

// FindAddressMapByName returns the first address map with the given name,
// compared case-insensitively, or nil when none matches. Names are not
// enforced to be unique by the Orchestrator, but policies and overlay ACLs
// reference applications by name, so duplicates are ambiguous.
func FindAddressMapByName(maps []AddressMap, name string, ignoreRanges ...string) *AddressMap {
	ignored := make(map[string]bool, len(ignoreRanges))
	for _, r := range ignoreRanges {
		ignored[r] = true
	}
	for i := range maps {
		if ignored[AddressMapRangeKey(maps[i].IPStart, maps[i].IPEnd)] {
			continue
		}
		if strings.EqualFold(maps[i].Name, name) {
			return &maps[i]
		}
	}
	return nil
}

// AddressMapRangeKey renders a range as the identifier this provider uses:
// a single address when start and end match, otherwise "start-end".
func AddressMapRangeKey(ipStart, ipEnd string) string {
	if ipStart == ipEnd {
		return ipStart
	}
	return ipStart + "-" + ipEnd
}

// ===========================================================================
// POST/DELETE /gms/rest/applicationDefinition/ipIntelligenceClassification
// ===========================================================================

// setAddressMap creates or updates an entry; the API treats POST as an
// upsert keyed by the address range.
func (c *Client) setAddressMap(def AddressMap) error {
	start, err := ParseIPv4(def.IPStart)
	if err != nil {
		return err
	}
	end, err := ParseIPv4(def.IPEnd)
	if err != nil {
		return err
	}
	if end < start {
		return fmt.Errorf("ip_end %s must not be lower than ip_start %s", def.IPEnd, def.IPStart)
	}

	subattributes := def.subattributes
	if subattributes == "" {
		subattributes = defaultAddressMapSubattributes
	}

	body := addressMapPostBody{
		IPStart:       start,
		IPEnd:         end,
		SaasID:        def.saasID,
		Country:       def.Country,
		CountryCode:   def.CountryCode,
		Org:           def.Org,
		Name:          def.Name,
		Description:   def.Description,
		Priority:      def.Priority,
		Search:        def.search,
		Subattributes: subattributes,
	}

	path := fmt.Sprintf("/gms/rest/applicationDefinition/ipIntelligenceClassification?ipStart=%d&ipEnd=%d", start, end)
	respBody, statusCode, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated && statusCode != http.StatusNoContent {
		return fmt.Errorf("POST ipIntelligenceClassification returned status %d: %s",
			statusCode, string(respBody))
	}
	return nil
}

// CreateAddressMap creates a new address map entry.
func (c *Client) CreateAddressMap(def AddressMap) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()
	return c.setAddressMap(def)
}

// UpdateAddressMap updates an existing entry, carrying over the fields this
// client does not model so the Orchestrator's own settings survive.
func (c *Client) UpdateAddressMap(def AddressMap) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	if existing, err := c.GetAddressMap(def.IPStart, def.IPEnd); err == nil && existing != nil {
		def.saasID = existing.saasID
		def.search = existing.search
		if def.subattributes == "" {
			def.subattributes = existing.subattributes
		}
	}
	return c.setAddressMap(def)
}

// DeleteAddressMap removes the entry covering the given range. Deleting an
// entry that is referenced by an application group is refused by the API.
func (c *Client) DeleteAddressMap(ipStart, ipEnd string) error {
	c.appDefMu.Lock()
	defer c.appDefMu.Unlock()

	start, err := ParseIPv4(ipStart)
	if err != nil {
		return err
	}
	end, err := ParseIPv4(ipEnd)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/gms/rest/applicationDefinition/ipIntelligenceClassification?ipStart=%d&ipEnd=%d", start, end)
	respBody, statusCode, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("DELETE ipIntelligenceClassification returned status %d: %s",
			statusCode, string(respBody))
	}
	return nil
}
