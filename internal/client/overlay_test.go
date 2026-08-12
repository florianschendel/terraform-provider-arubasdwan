package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// realWorldACL mirrors the structure an Orchestrator actually returns:
// a top-level "options" object, entries that always carry "permit" and
// "comment", a rich set of match criteria, an entry with no criterion at
// all (match everything), and fields this client does not model
// ("overlay", "internet"). Values are anonymized.
const realWorldACL = `{"data":{"Overlay_Business":{"entry":{` +
	`"1000":{"src_ip":"10.0.0.0/8|172.16.0.0/12","dst_ip":"10.0.0.0/8","app_group":"GroupA","permit":true,"comment":""},` +
	`"1010":{"src_ip":"10.0.0.0/8","dscp":"ef","permit":true,"comment":""},` +
	`"1020":{"application":"AppOne","permit":true,"comment":"voice"},` +
	`"1030":{"either_dns":"*example.com","either_port":"80|443","permit":true,"comment":"health check"},` +
	`"1040":{"src_ip":"10.1.2.3/32","dst_ip":"10.4.5.6/32","protocol":"udp","either_port":"53","permit":true,"comment":""},` +
	`"1050":{"src_addrgrp_groups":"TestNetworks","src_vrf":"13","permit":true,"comment":""},` +
	`"1060":{"either_service":"service.example.com","permit":true,"comment":""},` +
	`"1070":{"comment":"","permit":true,"protocol":"icmp","overlay":"4","internet":0},` +
	`"1999":{"comment":"catch all","permit":true},` +
	`"2000":{"application":"Blocked","permit":false,"comment":"denied"}` +
	`}}},"options":{"merge":true,"delDependent":true}}`

func TestParseOverlayACLRealWorld(t *testing.T) {
	doc, err := parseOverlayACL(realWorldACL)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if doc.Name != "Overlay_Business" {
		t.Errorf("ACL name = %q", doc.Name)
	}
	if len(doc.Entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(doc.Entries))
	}
	// The document options must be captured for later preservation.
	if !strings.Contains(string(doc.Options), `"merge":true`) {
		t.Errorf("options not captured: %s", doc.Options)
	}

	bySeq := map[int]OverlayACLEntry{}
	for _, e := range doc.Entries {
		bySeq[e.Sequence] = e
	}

	// Multi-value fields are converted from the wire separator to commas.
	if got := bySeq[1000].Match.SrcIP; got != "10.0.0.0/8,172.16.0.0/12" {
		t.Errorf("src_ip = %q", got)
	}
	if bySeq[1000].Match.AppGroup != "GroupA" || !bySeq[1000].Permit {
		t.Errorf("entry 1000: %+v", bySeq[1000])
	}
	if bySeq[1010].Match.DSCP != "ef" {
		t.Errorf("dscp = %q", bySeq[1010].Match.DSCP)
	}
	if bySeq[1020].Match.Application != "AppOne" || bySeq[1020].Comment != "voice" {
		t.Errorf("entry 1020: %+v", bySeq[1020])
	}
	if bySeq[1030].Match.EitherDNS != "*example.com" || bySeq[1030].Match.EitherPort != "80,443" {
		t.Errorf("entry 1030: %+v", bySeq[1030].Match)
	}
	if bySeq[1040].Match.Protocol != "udp" || bySeq[1040].Match.DstIP != "10.4.5.6/32" {
		t.Errorf("entry 1040: %+v", bySeq[1040].Match)
	}
	if bySeq[1050].Match.SrcAddressGroup != "TestNetworks" || bySeq[1050].Match.SrcVRF != "13" {
		t.Errorf("entry 1050: %+v", bySeq[1050].Match)
	}
	if bySeq[1060].Match.EitherService != "service.example.com" {
		t.Errorf("entry 1060: %+v", bySeq[1060].Match)
	}

	// An entry without any criterion matches all traffic.
	if !bySeq[1999].MatchAll() || bySeq[1999].Comment != "catch all" {
		t.Errorf("entry 1999: matchAll=%v %+v", bySeq[1999].MatchAll(), bySeq[1999])
	}
	// An entry with criteria does not.
	if bySeq[1020].MatchAll() {
		t.Error("entry 1020 must not report match-all")
	}
	// permit=false is preserved.
	if bySeq[2000].Permit {
		t.Error("entry 2000 must be a deny entry")
	}
}

// TestOverlayACLRoundTripRealWorld is the critical guarantee: re-encoding a
// parsed ACL must not lose anything — neither the document options, nor
// unmodelled entry fields, nor any match criterion.
func TestOverlayACLRoundTripRealWorld(t *testing.T) {
	doc, err := parseOverlayACL(realWorldACL)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	encoded, err := buildOverlayACL(doc)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// Options survive.
	if !strings.Contains(encoded, `"merge":true`) || !strings.Contains(encoded, `"delDependent":true`) {
		t.Errorf("options lost: %s", encoded)
	}
	// Unmodelled fields survive.
	if !strings.Contains(encoded, `"overlay":"4"`) || !strings.Contains(encoded, `"internet":0`) {
		t.Errorf("unmodelled entry fields lost: %s", encoded)
	}
	// Wire separators are restored.
	if !strings.Contains(encoded, `10.0.0.0/8|172.16.0.0/12`) {
		t.Errorf("multi-value separator not restored: %s", encoded)
	}

	// Re-parsing yields the same entries.
	again, err := parseOverlayACL(encoded)
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}
	if len(again.Entries) != len(doc.Entries) {
		t.Fatalf("entry count changed: %d -> %d", len(doc.Entries), len(again.Entries))
	}
	for i := range doc.Entries {
		a, b := doc.Entries[i], again.Entries[i]
		if a.Sequence != b.Sequence || a.Permit != b.Permit || a.Comment != b.Comment || a.Match != b.Match {
			t.Errorf("entry %d changed:\n  before %+v\n  after  %+v", a.Sequence, a, b)
		}
	}
}

// TestBuildOverlayACLShape verifies the encoding matches what appliances
// send: permit and comment always present, no match_acl_all marker.
func TestBuildOverlayACLShape(t *testing.T) {
	doc := overlayACLDocument{
		Name: "Overlay_Test",
		Entries: []OverlayACLEntry{
			{Sequence: 10, Permit: true, Match: OverlayACLMatch{AppGroup: "GroupA"}},
			{Sequence: 20, Permit: true}, // match all
		},
	}
	encoded, err := buildOverlayACL(doc)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	var wire struct {
		Data map[string]struct {
			Entry map[string]map[string]json.RawMessage `json:"entry"`
		} `json:"data"`
		Options map[string]bool `json:"options"`
	}
	if err := json.Unmarshal([]byte(encoded), &wire); err != nil {
		t.Fatalf("encoded document is not valid JSON: %v", err)
	}

	entries := wire.Data["Overlay_Test"].Entry
	for _, seq := range []string{"10", "20"} {
		if _, ok := entries[seq]["permit"]; !ok {
			t.Errorf("entry %s: permit missing", seq)
		}
		if _, ok := entries[seq]["comment"]; !ok {
			t.Errorf("entry %s: comment missing", seq)
		}
		if _, ok := entries[seq]["match_acl_all"]; ok {
			t.Errorf("entry %s: unexpected match_acl_all marker", seq)
		}
	}
	// The catch-all entry carries no match criterion.
	if len(entries["20"]) != 2 {
		t.Errorf("catch-all entry should only hold permit and comment: %v", entries["20"])
	}
	// New ACLs get the options appliances report.
	if !wire.Options["merge"] || !wire.Options["delDependent"] {
		t.Errorf("default options missing: %v", wire.Options)
	}

	// No entries clears the ACL.
	cleared, err := buildOverlayACL(overlayACLDocument{Name: "Overlay_Test"})
	if err != nil || cleared != "" {
		t.Errorf("clearing ACL: %q, err=%v", cleared, err)
	}
}

// TestParseOverlayACLDocumentedForm accepts the match_acl_all marker from
// the API documentation as an equivalent match-all entry.
func TestParseOverlayACLDocumentedForm(t *testing.T) {
	doc, err := parseOverlayACL(`{"data":{"Overlay_Business":{"entry":{"1":{"match_acl_all":""}}}}}`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(doc.Entries) != 1 || !doc.Entries[0].MatchAll() {
		t.Errorf("documented match-all form not recognized: %+v", doc.Entries)
	}

	// An empty ACL yields an empty document without erroring.
	empty, err := parseOverlayACL("")
	if err != nil || empty.Name != "" || empty.Entries != nil {
		t.Errorf("empty ACL: %+v err=%v", empty, err)
	}
}

// TestParseOverlay covers the three match types of an overlay and the
// preservation of the full configuration for read-modify-write updates.
func TestParseOverlay(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantType  string
		wantExtra string // ACL name or interface label
	}{
		{
			name:      "built-in overlay ACL",
			raw:       `{"id":1,"name":"Business","bondingPolicy":2,"match":{"overlayAcl":"{\"data\":{\"Overlay_Business\":{\"entry\":{\"1\":{\"permit\":true,\"comment\":\"\"}}}},\"options\":{\"merge\":true}}"}}`,
			wantType:  OverlayMatchOverlayACL,
			wantExtra: "Overlay_Business",
		},
		{
			name:      "appliance ACL reference",
			raw:       `{"id":2,"name":"VoIP","match":{"acl":"VoiceACL"}}`,
			wantType:  OverlayMatchApplianceACL,
			wantExtra: "VoiceACL",
		},
		{
			name:      "interface label",
			raw:       `{"id":3,"name":"Guest","match":{"interfaceLabel":"LAN1"}}`,
			wantType:  OverlayMatchInterfaceLabel,
			wantExtra: "LAN1",
		},
		{
			name:     "no match configured",
			raw:      `{"id":4,"name":"Empty","match":{}}`,
			wantType: OverlayMatchNone,
		},
	}

	for _, c := range cases {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(c.raw), &raw); err != nil {
			t.Fatalf("%s: unmarshal failed: %v", c.name, err)
		}
		overlay, err := parseOverlay(raw)
		if err != nil {
			t.Errorf("%s: parse failed: %v", c.name, err)
			continue
		}
		if overlay.MatchType != c.wantType {
			t.Errorf("%s: match type = %q, want %q", c.name, overlay.MatchType, c.wantType)
		}
		switch c.wantType {
		case OverlayMatchInterfaceLabel:
			if overlay.InterfaceLabel != c.wantExtra {
				t.Errorf("%s: interface label = %q", c.name, overlay.InterfaceLabel)
			}
		case OverlayMatchOverlayACL, OverlayMatchApplianceACL:
			if overlay.ACLName != c.wantExtra {
				t.Errorf("%s: ACL name = %q, want %q", c.name, overlay.ACLName, c.wantExtra)
			}
		}
		// The complete configuration must be retained for updates.
		if _, ok := overlay.raw["name"]; !ok {
			t.Errorf("%s: raw configuration not preserved", c.name)
		}
	}

	// The built-in ACL of the first case must be reported with its entries
	// and its options must be captured for later updates.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal([]byte(cases[0].raw), &raw)
	overlay, _ := parseOverlay(raw)
	if len(overlay.ACLEntries) != 1 || !overlay.ACLEntries[0].MatchAll() {
		t.Errorf("overlay ACL entries: %+v", overlay.ACLEntries)
	}
	if overlay.RawACL == "" {
		t.Error("raw ACL string not reported")
	}
	if !strings.Contains(string(overlay.aclOptions), "merge") {
		t.Errorf("ACL options not captured: %s", overlay.aclOptions)
	}
}

func TestDefaultOverlayACLName(t *testing.T) {
	if got := DefaultOverlayACLName("Business"); got != "Overlay_Business" {
		t.Errorf("DefaultOverlayACLName = %q", got)
	}
}
