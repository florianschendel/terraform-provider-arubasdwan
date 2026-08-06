package client

// This file implements read-only access to the BGP configuration of the
// appliances. The data is consumed by the arubasdwan_bgp_config data source
// and combines three endpoints:
//
//   - GET /gms/rest/appliance
//     Appliance inventory (hostname, serial number, model, site, ...).
//   - GET /gms/rest/bgp/config/allVrfs/system?nePk=<nePk>&fromGms=<bool>
//     BGP system configuration (ASN, router ID, timers, redistribution)
//     of one appliance, keyed by VRF segment ID.
//   - GET /gms/rest/bgp/config/allVrfs/neighbor?nePk=<nePk>&fromGms=<bool>
//     BGP neighbor (peer) configuration of one appliance, keyed by VRF
//     segment ID and neighbor IP address.
//
// Orchestrator versions without the allVrfs endpoints fall back to the
// default-VRF endpoints /gms/rest/bgp/config/system and
// /gms/rest/bgp/config/neighbor, which return the same objects without the
// VRF wrapper.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
)

// BGPSystemConfig represents the BGP system (process) configuration of one
// VRF segment on an appliance, as returned by GET /gms/rest/bgp/config/system.
type BGPSystemConfig struct {
	Enabled               bool   // True if BGP is enabled in this VRF
	ASN                   int64  // Local autonomous system number (supports 4-byte ASNs)
	RouterID              string // BGP router ID
	GracefulRestart       bool   // True if graceful restart is enabled
	MaxRestartTime        int    // Max wait for a restarting peer in seconds (1-3600)
	StalePathTime         int    // Max time stale routes of a restarted peer are kept in seconds (1-3600)
	RedistOSPF            bool   // True if routes are redistributed to OSPF
	RedistOSPFFilter      int    // Filter (bitmask) applied to routes redistributed to OSPF
	RemoteASPathAdvertise bool   // True if the remote AS path is propagated
}

// BGPNeighbor represents one configured BGP peer of a VRF segment on an
// appliance, as returned by GET /gms/rest/bgp/config/neighbor.
type BGPNeighbor struct {
	IP                string // IP address of the neighbor
	RemoteAS          int64  // Remote autonomous system number of the neighbor (supports 4-byte ASNs)
	Type              string // Peer type (e.g. "Branch", "Branch-transit", "PE-router")
	Enabled           bool   // True if the BGP session to this neighbor is enabled
	ImportRoutes      bool   // True if routes learned from the neighbor are imported
	ExportMap         int64  // Route export policies bitmask; 4294967295 = predefined bitmask of the peer type unchanged
	HoldTimer         int    // Hold timer in seconds
	KeepAliveTimer    int    // Keepalive interval in seconds
	MED               int    // Multi-Exit Discriminator for routes advertised to the neighbor
	InboundMED        int    // Metric applied to routes received from the neighbor
	LocalPreference   int    // Local preference for routes advertised to the neighbor
	ASPrependCount    int    // Number of additional times the local AS is prepended to the AS path
	NextHopSelf       bool   // True if the appliance advertises its own IP as next hop
	DirectlyConnected bool   // True if the peer adjacency is treated as single hop
	BFDEnabled        bool   // True if a BFD session is desired for this peer ("bfd_desired")
	EVPN              bool   // True if EVPN is enabled for this peer
	Password          string // MD5 password of the session; may be empty or masked by the Orchestrator
}

// BGPVRFConfig groups the BGP configuration of one VRF segment on an
// appliance: the system configuration and the configured neighbors.
type BGPVRFConfig struct {
	VRFID     int    // VRF segment ID (0 = Default)
	VRFName   string // Resolved VRF segment name (e.g. "Default"); empty if it cannot be resolved
	System    BGPSystemConfig
	Neighbors []BGPNeighbor // Sorted by neighbor IP; empty if none are configured
}

// ApplianceBGP combines the appliance inventory data with the BGP
// configuration of its VRF segments.
type ApplianceBGP struct {
	Appliance
	VRFs []BGPVRFConfig // Sorted by VRF ID; empty if no BGP data is available for the appliance
}

// bgpSystemGetItem is the wire format of one BGP system configuration. All
// scalars use the tolerant flex types because the Orchestrator is not always
// consistent about scalar types across versions.
type bgpSystemGetItem struct {
	Enable                flexBool   `json:"enable"`
	ASN                   flexInt    `json:"asn"`
	RtrID                 flexString `json:"rtr_id"`
	GracefulRestartEn     flexBool   `json:"graceful_restart_en"`
	MaxRestartTime        flexInt    `json:"max_restart_time"`
	StalePathTime         flexInt    `json:"stale_path_time"`
	RedistOSPF            flexBool   `json:"redist_ospf"`
	RedistOSPFFilter      flexInt    `json:"redist_ospf_filter"`
	RemoteASPathAdvertise flexBool   `json:"remote_as_path_advertise"`
}

// toBGPSystemConfig converts the wire format into the public model.
func (s bgpSystemGetItem) toBGPSystemConfig() BGPSystemConfig {
	return BGPSystemConfig{
		Enabled:               bool(s.Enable),
		ASN:                   int64(s.ASN),
		RouterID:              string(s.RtrID),
		GracefulRestart:       bool(s.GracefulRestartEn),
		MaxRestartTime:        int(s.MaxRestartTime),
		StalePathTime:         int(s.StalePathTime),
		RedistOSPF:            bool(s.RedistOSPF),
		RedistOSPFFilter:      int(s.RedistOSPFFilter),
		RemoteASPathAdvertise: bool(s.RemoteASPathAdvertise),
	}
}

// bgpNeighborGetItem is the wire format of one BGP neighbor entry.
type bgpNeighborGetItem struct {
	Self              flexString `json:"self"`
	RemoteAS          flexInt    `json:"remote_as"`
	Type              flexString `json:"type"`
	Enable            flexBool   `json:"enable"`
	ImportRtes        flexBool   `json:"import_rtes"`
	ExportMap         flexInt    `json:"export_map"`
	Hold              flexInt    `json:"hold"`
	KA                flexInt    `json:"ka"`
	MED               flexInt    `json:"med"`
	InMED             flexInt    `json:"in_med"`
	LocPref           flexInt    `json:"loc_pref"`
	ASPrepend         flexInt    `json:"as_prepend"`
	NextHopSelf       flexBool   `json:"next_hop_self"`
	DirectlyConnected flexBool   `json:"directly_connected"`
	BFDDesired        flexBool   `json:"bfd_desired"`
	EVPN              flexBool   `json:"evpn"`
	Password          flexString `json:"password"`
}

// toBGPNeighbor converts the wire format into the public model. The key the
// entry was found under serves as fallback when the "self" field is empty.
func (n bgpNeighborGetItem) toBGPNeighbor(key string) BGPNeighbor {
	ip := string(n.Self)
	if ip == "" {
		ip = key
	}
	return BGPNeighbor{
		IP:                ip,
		RemoteAS:          int64(n.RemoteAS),
		Type:              string(n.Type),
		Enabled:           bool(n.Enable),
		ImportRoutes:      bool(n.ImportRtes),
		ExportMap:         int64(n.ExportMap),
		HoldTimer:         int(n.Hold),
		KeepAliveTimer:    int(n.KA),
		MED:               int(n.MED),
		InboundMED:        int(n.InMED),
		LocalPreference:   int(n.LocPref),
		ASPrependCount:    int(n.ASPrepend),
		NextHopSelf:       bool(n.NextHopSelf),
		DirectlyConnected: bool(n.DirectlyConnected),
		BFDEnabled:        bool(n.BFDDesired),
		EVPN:              bool(n.EVPN),
		Password:          string(n.Password),
	}
}

// bgpNotAvailable reports whether the response object is the documented
// {"ERROR": "NOT_AVAILABLE"} form, which the Orchestrator returns when it has
// no BGP data for the appliance (e.g. BGP never configured, or no cached
// copy). Callers treat it as "no BGP configuration" rather than a failure.
func bgpNotAvailable(obj map[string]json.RawMessage) bool {
	_, ok := obj["ERROR"]
	return ok && len(obj) == 1
}

// allKeysNumeric reports whether every key of the object parses as an
// integer, which distinguishes the allVrfs form (keyed by VRF segment ID)
// from the default-VRF form (named fields or neighbor IP keys). An empty
// object is not considered numeric-keyed.
func allKeysNumeric(obj map[string]json.RawMessage) bool {
	if len(obj) == 0 {
		return false
	}
	for key := range obj {
		if _, err := strconv.Atoi(key); err != nil {
			return false
		}
	}
	return true
}

// parseBGPSystemConfigs accepts both wire forms of the BGP system
// configuration endpoints: the allVrfs object keyed by VRF segment ID, and
// the plain default-VRF object (mapped to VRF 0). The NOT_AVAILABLE error
// object and an empty object yield an empty map.
func parseBGPSystemConfigs(body []byte) (map[int]BGPSystemConfig, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("unrecognized BGP system config response format: %s", snippet)
	}

	configs := make(map[int]BGPSystemConfig)
	if len(obj) == 0 || bgpNotAvailable(obj) {
		return configs, nil
	}

	if allKeysNumeric(obj) {
		for key, raw := range obj {
			vrfID, _ := strconv.Atoi(key)
			var item bgpSystemGetItem
			if err := json.Unmarshal(raw, &item); err != nil {
				return nil, fmt.Errorf("error unmarshaling BGP system config of VRF %s: %w", key, err)
			}
			configs[vrfID] = item.toBGPSystemConfig()
		}
		return configs, nil
	}

	// Default-VRF form: the object itself is the system configuration.
	var item bgpSystemGetItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("error unmarshaling BGP system config: %w", err)
	}
	configs[0] = item.toBGPSystemConfig()
	return configs, nil
}

// sortBGPNeighbors sorts neighbors by IP address, numerically where the
// addresses parse (so 10.0.0.2 comes before 10.0.0.10), falling back to
// string order.
func sortBGPNeighbors(neighbors []BGPNeighbor) {
	sort.Slice(neighbors, func(i, j int) bool {
		a, errA := netip.ParseAddr(neighbors[i].IP)
		b, errB := netip.ParseAddr(neighbors[j].IP)
		if errA == nil && errB == nil {
			return a.Less(b)
		}
		return neighbors[i].IP < neighbors[j].IP
	})
}

// parseBGPNeighborMap converts one object keyed by neighbor IP address into
// the sorted neighbor list.
func parseBGPNeighborMap(raw json.RawMessage) ([]BGPNeighbor, error) {
	var byIP map[string]bgpNeighborGetItem
	if err := json.Unmarshal(raw, &byIP); err != nil {
		return nil, err
	}
	neighbors := make([]BGPNeighbor, 0, len(byIP))
	for key, item := range byIP {
		if key == "ERROR" {
			continue
		}
		neighbors = append(neighbors, item.toBGPNeighbor(key))
	}
	sortBGPNeighbors(neighbors)
	return neighbors, nil
}

// parseBGPNeighbors accepts both wire forms of the BGP neighbor endpoints:
// the allVrfs object keyed by VRF segment ID with neighbor maps as values,
// and the plain default-VRF object keyed by neighbor IP address (mapped to
// VRF 0). The NOT_AVAILABLE error object and an empty object yield an empty
// map.
func parseBGPNeighbors(body []byte) (map[int][]BGPNeighbor, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("unrecognized BGP neighbor response format: %s", snippet)
	}

	neighbors := make(map[int][]BGPNeighbor)
	if len(obj) == 0 || bgpNotAvailable(obj) {
		return neighbors, nil
	}

	if allKeysNumeric(obj) {
		for key, raw := range obj {
			vrfID, _ := strconv.Atoi(key)
			list, err := parseBGPNeighborMap(raw)
			if err != nil {
				return nil, fmt.Errorf("error unmarshaling BGP neighbors of VRF %s: %w", key, err)
			}
			neighbors[vrfID] = list
		}
		return neighbors, nil
	}

	// Default-VRF form: the object itself is the neighbor map.
	list, err := parseBGPNeighborMap(body)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling BGP neighbors: %w", err)
	}
	neighbors[0] = list
	return neighbors, nil
}

// getBGPConfig retrieves the BGP system and neighbor configuration of a
// single appliance across all VRF segments. It prefers the allVrfs endpoints
// and falls back to the default-VRF endpoints when those are unavailable.
//
// API endpoints: GET /gms/rest/bgp/config/allVrfs/system?nePk=<nePk>&fromGms=<bool>
//
//	GET /gms/rest/bgp/config/allVrfs/neighbor?nePk=<nePk>&fromGms=<bool>
//	GET /gms/rest/bgp/config/system?nePk=<nePk>&fromGms=<bool>      (fallback)
//	GET /gms/rest/bgp/config/neighbor?nePk=<nePk>&fromGms=<bool>    (fallback)
//
// With fromGms=true the data is served from the Orchestrator database; with
// fromGms=false the Orchestrator queries the appliance directly (slower, and
// it fails for unreachable appliances).
func (c *Client) getBGPConfig(nePk string, cached bool) (map[int]BGPSystemConfig, map[int][]BGPNeighbor, error) {
	body, err := c.fetchBGPSection(nePk, cached, "system")
	if err != nil {
		return nil, nil, err
	}
	systems, err := parseBGPSystemConfigs(body)
	if err != nil {
		return nil, nil, err
	}

	body, err = c.fetchBGPSection(nePk, cached, "neighbor")
	if err != nil {
		return nil, nil, err
	}
	neighbors, err := parseBGPNeighbors(body)
	if err != nil {
		return nil, nil, err
	}
	return systems, neighbors, nil
}

// fetchBGPSection fetches one section ("system" or "neighbor") of the BGP
// configuration, trying the allVrfs endpoint first and falling back to the
// default-VRF endpoint on a non-OK status.
func (c *Client) fetchBGPSection(nePk string, cached bool, section string) ([]byte, error) {
	query := fmt.Sprintf("nePk=%s&fromGms=%t", url.QueryEscape(nePk), cached)

	respBody, statusCode, err := c.doRequest("GET", fmt.Sprintf("/gms/rest/bgp/config/allVrfs/%s?%s", section, query), nil)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusOK {
		return respBody, nil
	}

	respBody, statusCode, err = c.doRequest("GET", fmt.Sprintf("/gms/rest/bgp/config/%s?%s", section, query), nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /bgp/config/%s returned status %d: %s", section, statusCode, string(respBody))
	}
	return respBody, nil
}

// GetApplianceBGP retrieves the BGP configuration of the appliances passing
// the filter, combined with their inventory data. Appliances without any BGP
// configuration are included with an empty VRF list, so the result always
// covers the complete filtered inventory.
//
// The cached flag selects the data source of the per-appliance BGP reads:
// true serves data from the Orchestrator database (fast, works for
// unreachable appliances), false queries each appliance directly.
//
// Failures of the appliance inventory or of any per-appliance BGP read abort
// the operation with an error, so a partial inventory is never silently
// reported as complete. A failure to resolve VRF segment names is returned
// as a warning; VRF names are then empty.
func (c *Client) GetApplianceBGP(filter ApplianceFilter, cached bool) ([]ApplianceBGP, []string, error) {
	var warnings []string

	appliances, err := c.GetAppliances()
	if err != nil {
		return nil, nil, fmt.Errorf("error fetching appliance inventory: %w", err)
	}

	filtered := make([]Appliance, 0, len(appliances))
	for _, a := range appliances {
		if filter.matches(a) {
			filtered = append(filtered, a)
		}
	}
	if filter.NePk != "" && len(filtered) == 0 {
		return nil, nil, fmt.Errorf("no appliance with nePk %q found (or it is excluded by the filter)", filter.NePk)
	}

	// Resolve VRF segment IDs to names once for all appliances; degrade to
	// empty names when the segments endpoint fails.
	segmentNames := map[int]string{}
	if segments, err := c.GetVRFSegments(); err != nil {
		warnings = append(warnings, fmt.Sprintf("could not resolve VRF segment names: %s", err))
	} else {
		for _, segment := range segments {
			segmentNames[segment.ID] = segment.Name
		}
	}

	result := make([]ApplianceBGP, 0, len(filtered))
	for _, appliance := range filtered {
		systems, neighbors, err := c.getBGPConfig(appliance.NePk, cached)
		if err != nil {
			return nil, nil, fmt.Errorf("error fetching BGP configuration of appliance %s (%s): %w", appliance.HostName, appliance.NePk, err)
		}

		// Union of the VRF IDs seen in either section.
		vrfIDs := make(map[int]bool)
		for id := range systems {
			vrfIDs[id] = true
		}
		for id := range neighbors {
			vrfIDs[id] = true
		}
		ids := make([]int, 0, len(vrfIDs))
		for id := range vrfIDs {
			ids = append(ids, id)
		}
		sort.Ints(ids)

		vrfs := make([]BGPVRFConfig, 0, len(ids))
		for _, id := range ids {
			vrf := BGPVRFConfig{
				VRFID:     id,
				VRFName:   segmentNames[id],
				System:    systems[id],
				Neighbors: neighbors[id],
			}
			if vrf.Neighbors == nil {
				vrf.Neighbors = []BGPNeighbor{}
			}
			vrfs = append(vrfs, vrf)
		}
		result = append(result, ApplianceBGP{
			Appliance: appliance,
			VRFs:      vrfs,
		})
	}
	return result, warnings, nil
}
