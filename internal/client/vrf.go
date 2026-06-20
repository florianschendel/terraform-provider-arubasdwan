package client

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// VRFSegment represents a Virtual Routing and Forwarding (VRF) segment on the
// Orchestrator.
//
// VRF segments provide network-level isolation within the SD-WAN fabric. Each
// segment acts as an independent routing domain with its own set of routes,
// security zones, and firewall policies. Common use cases include separating
// corporate traffic from guest traffic, or isolating PCI-compliant networks.
//
// The Orchestrator always has at least one VRF segment: the Default segment (ID 0).
// Additional segments can be created to support multi-tenancy or network isolation.
type VRFSegment struct {
	ID      int    `json:"id"`      // Unique segment identifier (0 = Default)
	Name    string `json:"name"`    // Human-readable name (e.g. "Default", "Guest", "PCI")
	Status  int    `json:"status"`  // Operational status (0 = active)
	Comment string `json:"comment"` // Optional description
}

// GetVRFSegments retrieves all VRF segments from the Orchestrator.
//
// API endpoint: GET /gms/rest/vrf/config/segments
//
// The API returns segments as a JSON object keyed by segment ID:
//
//	{"0": {"id": 0, "name": "Default", "status": 0, "comment": ""}, ...}
//
// The returned slice is sorted by segment ID in ascending order.
func (c *Client) GetVRFSegments() ([]VRFSegment, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/vrf/config/segments", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != 200 {
		return nil, fmt.Errorf("GET /vrf/config/segments returned status %d: %s", statusCode, string(respBody))
	}

	// Parse the JSON object: keys are segment IDs (as strings), values are segment objects.
	var raw map[string]VRFSegment
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling VRF segments: %w", err)
	}

	segments := make([]VRFSegment, 0, len(raw))
	for idStr, seg := range raw {
		// The segment ID should be set in the JSON value, but if it's zero (default),
		// fall back to parsing the map key. This handles edge cases in the API response.
		if seg.ID == 0 {
			if id, err := strconv.Atoi(idStr); err == nil {
				seg.ID = id
			}
		}
		segments = append(segments, seg)
	}

	// Sort by ID for consistent ordering.
	sort.Slice(segments, func(i, j int) bool { return segments[i].ID < segments[j].ID })

	return segments, nil
}

// VRFZoneMapping represents a zone-to-VRF assignment from the vrfSegmentZonesMap
// endpoint.
//
// In a multi-VRF environment, each security zone gets a unique numeric ID per VRF
// segment. For example, the "LAN" zone might be:
//   - Zone ID 20 in VRF 0 (Default)
//   - Zone ID 40 in VRF 1 (Guest)
//
// This mapping is essential for translating between the Default VRF zone IDs
// (used in Terraform state) and the VRF-specific zone IDs (used in the API).
type VRFZoneMapping struct {
	ZoneID   int    `json:"zoneId"`   // The zone's numeric ID within this specific VRF
	ZoneName string `json:"zoneName"` // The zone's human-readable name (same across all VRFs)
	VRFID    int    `json:"vrfId"`    // The VRF segment ID this mapping belongs to
	VRFName  string `json:"vrfName"`  // The VRF segment name
}

// GetVRFZoneMappings retrieves all zone-to-VRF assignments from the Orchestrator.
//
// API endpoint: GET /gms/rest/zones/vrfSegmentZonesMap
//
// The returned slice is sorted by VRF ID first, then by zone ID within each VRF.
func (c *Client) GetVRFZoneMappings() ([]VRFZoneMapping, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/zones/vrfSegmentZonesMap", nil)
	if err != nil {
		return nil, err
	}

	if statusCode != 200 {
		return nil, fmt.Errorf("GET /zones/vrfSegmentZonesMap returned status %d: %s", statusCode, string(respBody))
	}

	var mappings []VRFZoneMapping
	if err := json.Unmarshal(respBody, &mappings); err != nil {
		return nil, fmt.Errorf("error unmarshaling VRF zone mappings: %w", err)
	}

	// Sort by VRF ID first, then by zone ID for consistent ordering.
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].VRFID != mappings[j].VRFID {
			return mappings[i].VRFID < mappings[j].VRFID
		}
		return mappings[i].ZoneID < mappings[j].ZoneID
	})

	return mappings, nil
}

// ZoneTranslator provides efficient zone ID translation between VRF segments.
// It caches the VRF zone mappings from a single API call and provides lookup
// methods to translate zone IDs between VRFs.
//
// This is used by the security policy operations to translate between:
//   - Default VRF (0) zone IDs: Used in Terraform state for consistency
//   - VRF-specific zone IDs: Used in the Orchestrator API
//
// Example: If "LAN" is zone 20 in VRF 0 and zone 40 in VRF 1:
//   - ToVRF(20, 1) returns 40     (Default VRF → VRF 1)
//   - ToDefaultVRF(40) returns 20 (VRF 1 → Default VRF)
type ZoneTranslator struct {
	mappings  []VRFZoneMapping                   // All zone-to-VRF mappings
	byID      map[int]VRFZoneMapping             // Lookup by zone ID → mapping
	byNameVRF map[string]map[int]VRFZoneMapping  // Lookup by zone name → VRF ID → mapping
}

// NewZoneTranslator fetches zone-to-VRF mappings once and returns a reusable
// translator. The translator caches all mappings to avoid redundant API calls
// when translating multiple zone IDs in a single operation (e.g. translating
// all zone IDs in a policy set).
func (c *Client) NewZoneTranslator() (*ZoneTranslator, error) {
	mappings, err := c.GetVRFZoneMappings()
	if err != nil {
		return nil, err
	}

	// Build lookup indices for efficient translation.
	zt := &ZoneTranslator{
		mappings:  mappings,
		byID:      make(map[int]VRFZoneMapping, len(mappings)),
		byNameVRF: make(map[string]map[int]VRFZoneMapping),
	}
	for _, m := range mappings {
		zt.byID[m.ZoneID] = m
		if zt.byNameVRF[m.ZoneName] == nil {
			zt.byNameVRF[m.ZoneName] = make(map[int]VRFZoneMapping)
		}
		zt.byNameVRF[m.ZoneName][m.VRFID] = m
	}
	return zt, nil
}

// ToVRF translates a zone ID to the equivalent ID in the target VRF segment.
//
// The translation works by:
//  1. Looking up the source zone ID to find its zone name and current VRF.
//  2. Finding the same zone name in the target VRF.
//  3. Returning the target VRF's zone ID for that zone name.
//
// Returns an error if the zone ID is not found or the zone doesn't exist in
// the target VRF.
func (zt *ZoneTranslator) ToVRF(zoneID, targetVRFID int) (int, error) {
	// Find the source zone's name by looking up its ID.
	src, ok := zt.byID[zoneID]
	if !ok {
		return 0, fmt.Errorf("zone ID %d not found in any VRF", zoneID)
	}
	// Find the same zone name in the target VRF.
	dst, ok := zt.byNameVRF[src.ZoneName][targetVRFID]
	if !ok {
		return 0, fmt.Errorf("zone %q not found in VRF %d", src.ZoneName, targetVRFID)
	}
	return dst.ZoneID, nil
}

// ToDefaultVRF translates a VRF-specific zone ID back to the Default VRF (0) ID.
// If the translation fails (zone not found or not present in Default VRF), the
// original zone ID is returned unchanged as a safe fallback.
func (zt *ZoneTranslator) ToDefaultVRF(zoneID int) int {
	src, ok := zt.byID[zoneID]
	if !ok {
		return zoneID // Zone not found; return as-is
	}
	dst, ok := zt.byNameVRF[src.ZoneName][0] // VRF 0 = Default
	if !ok {
		return zoneID // Zone not in Default VRF; return as-is
	}
	return dst.ZoneID
}
