package client

// This file implements access to Business Intent Overlay (BIO) configuration
// and to the overlay ACL that selects which traffic an overlay carries.
//
//   - GET  /gms/rest/gms/overlays/config[?overlayId=<id>]
//     Returns the configured overlays.
//   - PUT  /gms/rest/gms/overlays/config?overlayId=<id>
//     Replaces the configuration of one overlay.
//
// Both endpoints are identical on Orchestrator 9.6.3 and 9.7.0.
//
// An overlay selects traffic through its "match" object, which carries
// exactly one of:
//
//   - interfaceLabel: match all traffic of a LAN interface label
//   - acl:            reference an ACL defined on the appliance
//   - overlayAcl:     an ACL built into the overlay configuration
//
// The overlayAcl value is a JSON document embedded as a string:
//
//	{"data":{"<aclName>":{"entry":{"1":{"match_acl_all":""}}}}}
//
// Each entry selects traffic by application, application group, or all
// traffic. Because the Orchestrator API documents neither the full entry
// field set nor forbids additional fields, entries are parsed and written
// back field-preserving: unknown fields of existing entries survive an
// update untouched.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Overlay match types reported in Overlay.MatchType.
const (
	OverlayMatchOverlayACL     = "overlay_acl"
	OverlayMatchApplianceACL   = "appliance_acl"
	OverlayMatchInterfaceLabel = "interface_label"
	OverlayMatchNone           = ""
)

// OverlayACLMatch holds the traffic match criteria of an ACL entry. All
// criteria set on one entry are combined with AND; an entry with no
// criterion at all matches every packet.
//
// Multi-value fields (IP and port lists) use "|" as separator on the wire.
// Callers work with comma-separated lists, which are converted on the way
// in and out — the same convention the security policy resource uses.
type OverlayACLMatch struct {
	Application string // Application name (built-in or user-defined)
	AppGroup    string // Application group name

	SrcIP    string // Source IP address or CIDR list
	DstIP    string // Destination IP address or CIDR list
	EitherIP string // Match either direction

	SrcPort    string // Source port or range list
	DstPort    string // Destination port or range list
	EitherPort string // Match either direction
	Protocol   string // IP protocol (tcp, udp, icmp, ...)
	DSCP       string // DSCP marking (e.g. "ef")

	SrcDNS    string // Source DNS hostname pattern
	DstDNS    string // Destination DNS hostname pattern
	EitherDNS string // Match either direction (supports wildcards)

	SrcService    string // Source SaaS service / organization name
	DstService    string // Destination service
	EitherService string // Match either direction

	SrcAddressGroup    string // Source address group (see ipObjects/addressGroup)
	DstAddressGroup    string // Destination address group
	EitherAddressGroup string // Match either direction

	SrcVRF string // Source VRF segment ID
	DstVRF string // Destination VRF segment ID
}

// aclMatchFields maps the wire field names to the struct members, driving
// both parsing and serialization.
func (m *OverlayACLMatch) fieldMap() map[string]*string {
	return map[string]*string{
		"application":           &m.Application,
		"app_group":             &m.AppGroup,
		"src_ip":                &m.SrcIP,
		"dst_ip":                &m.DstIP,
		"either_ip":             &m.EitherIP,
		"src_port":              &m.SrcPort,
		"dst_port":              &m.DstPort,
		"either_port":           &m.EitherPort,
		"protocol":              &m.Protocol,
		"dscp":                  &m.DSCP,
		"src_dns":               &m.SrcDNS,
		"dst_dns":               &m.DstDNS,
		"either_dns":            &m.EitherDNS,
		"src_service":           &m.SrcService,
		"dst_service":           &m.DstService,
		"either_service":        &m.EitherService,
		"src_addrgrp_groups":    &m.SrcAddressGroup,
		"dst_addrgrp_groups":    &m.DstAddressGroup,
		"either_addrgrp_groups": &m.EitherAddressGroup,
		"src_vrf":               &m.SrcVRF,
		"dst_vrf":               &m.DstVRF,
	}
}

// IsEmpty reports whether no match criterion is set, which makes the entry
// match all traffic.
func (m OverlayACLMatch) IsEmpty() bool {
	for _, target := range (&m).fieldMap() {
		if *target != "" {
			return false
		}
	}
	return true
}

// OverlayACLEntry is one rule of an overlay ACL.
type OverlayACLEntry struct {
	Sequence int             // Evaluation order within the ACL (the entry's map key)
	Permit   bool            // True = permit (carry in this overlay), false = deny
	Comment  string          // Free-form comment
	Match    OverlayACLMatch // Traffic match criteria; empty matches all traffic

	// extra keeps fields of an existing entry that this client does not
	// model, so that updates never drop Orchestrator-side settings.
	extra map[string]json.RawMessage
}

// MatchAll reports whether the entry matches all traffic.
func (e OverlayACLEntry) MatchAll() bool { return e.Match.IsEmpty() }

// Overlay represents one Business Intent Overlay with its traffic match
// configuration.
type Overlay struct {
	ID   int    // Numeric overlay ID
	Name string // Overlay name (e.g. "Business")

	MatchType      string            // One of the OverlayMatch* constants
	InterfaceLabel string            // LAN interface label, when MatchType is interface_label
	ACLName        string            // ACL name, when MatchType is overlay_acl or appliance_acl
	ACLEntries     []OverlayACLEntry // Entries of the built-in overlay ACL, sorted by sequence
	RawACL         string            // The raw overlayAcl string exactly as reported by the API

	// aclOptions holds the ACL document's top-level "options" object
	// (e.g. {"merge":true,"delDependent":true}) so updates preserve it.
	aclOptions json.RawMessage

	// raw is the complete overlay configuration as returned by the API. It
	// is sent back unmodified except for the match object, so that fields
	// this client does not model survive an update.
	raw map[string]json.RawMessage
}

// overlayMu serializes overlay updates: the API replaces a full overlay
// configuration, so concurrent read-modify-write cycles could lose changes.
var overlayMu sync.Mutex

// ===========================================================================
// overlayAcl wire format
// ===========================================================================

// reservedEntryFields are entry fields handled explicitly; every other
// field of an existing entry is preserved verbatim via
// OverlayACLEntry.extra.
var reservedEntryFields = map[string]bool{
	"permit":        true,
	"comment":       true,
	"self":          true,
	"match_acl_all": true, // documented match-all marker; appliances omit it
}

// overlayACLDocument is the decoded overlayAcl document.
type overlayACLDocument struct {
	Name    string
	Entries []OverlayACLEntry
	Options json.RawMessage
}

// parseOverlayACL decodes the embedded overlayAcl JSON document. It returns
// the ACL name, its entries sorted by sequence number, and the document's
// top-level options object, which must be preserved across updates. An
// empty string yields an empty document.
func parseOverlayACL(raw string) (overlayACLDocument, error) {
	doc := overlayACLDocument{}
	if strings.TrimSpace(raw) == "" {
		return doc, nil
	}

	var wire struct {
		Data map[string]struct {
			Entry map[string]map[string]json.RawMessage `json:"entry"`
		} `json:"data"`
		Options json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return doc, fmt.Errorf("error parsing overlay ACL: %w", err)
	}
	doc.Options = wire.Options

	// The document holds a single ACL; pick it deterministically.
	names := make([]string, 0, len(wire.Data))
	for name := range wire.Data {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return doc, nil
	}
	doc.Name = names[0]

	for seqKey, fields := range wire.Data[doc.Name].Entry {
		entry := OverlayACLEntry{
			Permit: true, // Appliances always send "permit"; default to allow.
			extra:  map[string]json.RawMessage{},
		}

		// The map key carries the sequence number; a "self" field, when
		// present, is authoritative.
		if seq, err := strconv.Atoi(seqKey); err == nil {
			entry.Sequence = seq
		}

		matchFields := entry.Match.fieldMap()
		for field, value := range fields {
			if target, ok := matchFields[field]; ok {
				var text string
				if err := json.Unmarshal(value, &text); err != nil {
					// Non-string match values (e.g. numeric IDs) are kept
					// verbatim so they survive a rewrite.
					entry.extra[field] = value
					continue
				}
				*target = pipeToComma(text)
				continue
			}

			switch field {
			case "permit":
				var permit flexBool
				if err := json.Unmarshal(value, &permit); err == nil {
					entry.Permit = bool(permit)
				}
			case "comment":
				_ = json.Unmarshal(value, &entry.Comment)
			case "self":
				var self flexInt
				if err := json.Unmarshal(value, &self); err == nil && int(self) != 0 {
					entry.Sequence = int(self)
				}
			case "match_acl_all":
				// Documented marker for "match all traffic"; an entry
				// without match criteria expresses the same thing.
			default:
				entry.extra[field] = value
			}
		}
		doc.Entries = append(doc.Entries, entry)
	}

	sort.Slice(doc.Entries, func(i, j int) bool { return doc.Entries[i].Sequence < doc.Entries[j].Sequence })
	return doc, nil
}

// buildOverlayACL encodes an ACL document into the string embedded in the
// overlay's match object. Returns an empty string when there are no
// entries, which clears the ACL.
//
// Entries are written in the shape appliances use: "permit" and "comment"
// are always present, match criteria only when set, and an entry without
// any criterion matches all traffic.
func buildOverlayACL(doc overlayACLDocument) (string, error) {
	if len(doc.Entries) == 0 {
		return "", nil
	}

	entryMap := make(map[string]map[string]json.RawMessage, len(doc.Entries))
	for _, entry := range doc.Entries {
		fields := map[string]json.RawMessage{}

		// Preserve fields of an existing entry that this client does not
		// model (e.g. "overlay" or "internet").
		for field, value := range entry.extra {
			if !reservedEntryFields[field] {
				fields[field] = value
			}
		}

		for field, target := range entry.Match.fieldMap() {
			if *target == "" {
				continue
			}
			value, err := json.Marshal(commaToPipe(*target))
			if err != nil {
				return "", err
			}
			fields[field] = value
		}

		comment, err := json.Marshal(entry.Comment)
		if err != nil {
			return "", err
		}
		fields["comment"] = comment
		if entry.Permit {
			fields["permit"] = json.RawMessage("true")
		} else {
			fields["permit"] = json.RawMessage("false")
		}

		entryMap[strconv.Itoa(entry.Sequence)] = fields
	}

	wire := map[string]interface{}{
		"data": map[string]interface{}{
			doc.Name: map[string]interface{}{
				"entry": entryMap,
			},
		},
	}
	// Preserve the document options; fall back to the defaults appliances
	// report when the ACL is new.
	if len(doc.Options) > 0 {
		wire["options"] = doc.Options
	} else {
		wire["options"] = map[string]bool{"merge": true, "delDependent": true}
	}

	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("error encoding overlay ACL: %w", err)
	}
	return string(encoded), nil
}

// DefaultOverlayACLName returns the ACL name the Orchestrator uses for an
// overlay's built-in ACL.
func DefaultOverlayACLName(overlayName string) string {
	return "Overlay_" + overlayName
}

// ===========================================================================
// GET /gms/rest/gms/overlays/config
// ===========================================================================

// parseOverlay converts one raw overlay object into the public model.
func parseOverlay(raw map[string]json.RawMessage) (Overlay, error) {
	overlay := Overlay{raw: raw}

	if v, ok := raw["id"]; ok {
		var id flexInt
		if err := json.Unmarshal(v, &id); err == nil {
			overlay.ID = int(id)
		}
	}
	if v, ok := raw["name"]; ok {
		_ = json.Unmarshal(v, &overlay.Name)
	}

	var match struct {
		ACL            string `json:"acl"`
		InterfaceLabel string `json:"interfaceLabel"`
		OverlayACL     string `json:"overlayAcl"`
	}
	if v, ok := raw["match"]; ok {
		if err := json.Unmarshal(v, &match); err != nil {
			return overlay, fmt.Errorf("error parsing match of overlay %q: %w", overlay.Name, err)
		}
	}

	switch {
	case match.OverlayACL != "":
		overlay.MatchType = OverlayMatchOverlayACL
		overlay.RawACL = match.OverlayACL
		doc, err := parseOverlayACL(match.OverlayACL)
		if err != nil {
			return overlay, fmt.Errorf("overlay %q: %w", overlay.Name, err)
		}
		overlay.ACLName = doc.Name
		overlay.ACLEntries = doc.Entries
		overlay.aclOptions = doc.Options
	case match.ACL != "":
		overlay.MatchType = OverlayMatchApplianceACL
		overlay.ACLName = match.ACL
	case match.InterfaceLabel != "":
		overlay.MatchType = OverlayMatchInterfaceLabel
		overlay.InterfaceLabel = match.InterfaceLabel
	default:
		overlay.MatchType = OverlayMatchNone
	}

	return overlay, nil
}

// GetOverlays retrieves all configured overlays, sorted by overlay ID.
//
// API endpoint: GET /gms/rest/gms/overlays/config
func (c *Client) GetOverlays() ([]Overlay, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/gms/overlays/config", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /gms/overlays/config returned status %d: %s", statusCode, string(respBody))
	}

	// The endpoint returns an array; some versions wrap a single overlay in
	// an object keyed by overlay ID.
	var rawList []map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &rawList); err != nil {
		var byID map[string]map[string]json.RawMessage
		if err2 := json.Unmarshal(respBody, &byID); err2 != nil {
			return nil, fmt.Errorf("error unmarshaling overlays response: %w", err)
		}
		keys := make([]string, 0, len(byID))
		for key := range byID {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			entry := byID[key]
			if _, ok := entry["id"]; !ok {
				if id, err := strconv.Atoi(key); err == nil {
					entry["id"] = json.RawMessage(strconv.Itoa(id))
				}
			}
			rawList = append(rawList, entry)
		}
	}

	overlays := make([]Overlay, 0, len(rawList))
	for _, raw := range rawList {
		overlay, err := parseOverlay(raw)
		if err != nil {
			return nil, err
		}
		overlays = append(overlays, overlay)
	}

	sort.Slice(overlays, func(i, j int) bool { return overlays[i].ID < overlays[j].ID })
	return overlays, nil
}

// GetOverlayByID returns the overlay with the given ID, or (nil, nil) when
// no such overlay exists.
func (c *Client) GetOverlayByID(id int) (*Overlay, error) {
	overlays, err := c.GetOverlays()
	if err != nil {
		return nil, err
	}
	for i := range overlays {
		if overlays[i].ID == id {
			return &overlays[i], nil
		}
	}
	return nil, nil
}

// GetOverlayByName returns the overlay with the given name (compared
// case-insensitively), or (nil, nil) when no such overlay exists.
func (c *Client) GetOverlayByName(name string) (*Overlay, error) {
	overlays, err := c.GetOverlays()
	if err != nil {
		return nil, err
	}
	for i := range overlays {
		if strings.EqualFold(overlays[i].Name, name) {
			return &overlays[i], nil
		}
	}
	return nil, nil
}

// ===========================================================================
// PUT /gms/rest/gms/overlays/config?overlayId=<id>
// ===========================================================================

// SetOverlayACL replaces the built-in ACL of an overlay with the given
// entries and pushes the overlay configuration back to the Orchestrator.
//
// The overlay is re-read immediately before the update and only its match
// object is replaced, so every other setting — including fields this client
// does not model — is sent back unchanged. Passing no entries clears the
// ACL, which makes the overlay match all traffic again.
//
// API endpoint: PUT /gms/rest/gms/overlays/config?overlayId=<id>
func (c *Client) SetOverlayACL(overlayID int, aclName string, entries []OverlayACLEntry) (*Overlay, error) {
	overlayMu.Lock()
	defer overlayMu.Unlock()

	overlay, err := c.GetOverlayByID(overlayID)
	if err != nil {
		return nil, fmt.Errorf("error reading overlay %d: %w", overlayID, err)
	}
	if overlay == nil {
		return nil, fmt.Errorf("no overlay with ID %d found", overlayID)
	}

	// Keep the existing ACL name unless the caller supplies one; fall back
	// to the Orchestrator's naming convention for a fresh ACL.
	if aclName == "" {
		aclName = overlay.ACLName
	}
	if aclName == "" {
		aclName = DefaultOverlayACLName(overlay.Name)
	}

	// Carry over unmodelled fields of entries that already exist, matched
	// by sequence number.
	existing := make(map[int]OverlayACLEntry, len(overlay.ACLEntries))
	for _, entry := range overlay.ACLEntries {
		existing[entry.Sequence] = entry
	}
	merged := make([]OverlayACLEntry, 0, len(entries))
	for _, entry := range entries {
		if prev, ok := existing[entry.Sequence]; ok {
			entry.extra = prev.extra
		}
		merged = append(merged, entry)
	}

	encodedACL, err := buildOverlayACL(overlayACLDocument{
		Name:    aclName,
		Entries: merged,
		Options: overlay.aclOptions,
	})
	if err != nil {
		return nil, err
	}

	// Replace only the match object; everything else is sent back as read.
	payload := make(map[string]json.RawMessage, len(overlay.raw))
	for key, value := range overlay.raw {
		payload[key] = value
	}
	match := map[string]string{}
	if encodedACL != "" {
		match["overlayAcl"] = encodedACL
	}
	encodedMatch, err := json.Marshal(match)
	if err != nil {
		return nil, err
	}
	payload["match"] = encodedMatch

	path := fmt.Sprintf("/gms/rest/gms/overlays/config?overlayId=%s", url.QueryEscape(strconv.Itoa(overlayID)))
	respBody, statusCode, err := c.doRequest("PUT", path, payload)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated && statusCode != http.StatusNoContent {
		return nil, fmt.Errorf("PUT /gms/overlays/config returned status %d: %s", statusCode, string(respBody))
	}

	// Read back so callers observe the Orchestrator's own view.
	updated, err := c.GetOverlayByID(overlayID)
	if err != nil {
		return nil, fmt.Errorf("overlay ACL was updated but could not be read back: %w", err)
	}
	if updated == nil {
		return nil, fmt.Errorf("overlay %d disappeared after the update", overlayID)
	}
	return updated, nil
}
