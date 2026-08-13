package client

import (
	"encoding/json"
	"testing"
)

func TestParseAndFormatIPv4(t *testing.T) {
	cases := []struct {
		text  string
		value uint32
	}{
		{"0.0.0.0", 0},
		{"10.0.0.0", 167772160},
		{"10.0.13.72", 167775560},
		// Above 2^31: these overflow a signed 32-bit int, which is why the
		// wire format is parsed into int64.
		{"165.225.73.68", 2783004996},
		{"192.168.1.1", 3232235777},
		{"255.255.255.255", 4294967295},
	}
	for _, c := range cases {
		got, err := ParseIPv4(c.text)
		if err != nil {
			t.Errorf("ParseIPv4(%q) failed: %v", c.text, err)
			continue
		}
		if got != c.value {
			t.Errorf("ParseIPv4(%q) = %d, want %d", c.text, got, c.value)
		}
		if back := FormatIPv4(c.value); back != c.text {
			t.Errorf("FormatIPv4(%d) = %q, want %q", c.value, back, c.text)
		}
	}

	// Surrounding whitespace is tolerated; anything else is rejected.
	if got, err := ParseIPv4("  10.0.0.1 "); err != nil || got != 167772161 {
		t.Errorf("ParseIPv4 with whitespace = %d, err=%v", got, err)
	}
	for _, bad := range []string{"", "not-an-ip", "10.0.0.256", "2001:db8::1", "10.0.0.0/24"} {
		if _, err := ParseIPv4(bad); err == nil {
			t.Errorf("ParseIPv4(%q) should fail", bad)
		}
	}
}

// TestParseAddressMapsResponse feeds the wire format the Orchestrator
// actually returns: an object keyed by start address, with addresses beyond
// the signed 32-bit range and fields this client carries over untouched.
func TestParseAddressMapsResponse(t *testing.T) {
	raw := `{
		"167775560": {"ip_start": 167775560, "ip_end": 167775560, "service_id": 700000, "saas_id": 0,
			"country": "Germany", "country_code": "DE", "org": "Example Org", "name": "host-a.example.com",
			"description": "single host", "priority": 100, "search": "no",
			"subattributes": "{\"msinstance\":\"\",\"mscategory\":\"\",\"proxy\":\"0\"}"},
		"2783004996": {"ip_start": 2783004996, "ip_end": 2783005000, "service_id": 700001, "saas_id": 0,
			"country": "", "country_code": "", "org": "", "name": "range-b",
			"description": "", "priority": "50", "search": "",
			"subattributes": ""}
	}`

	var wire map[string]addressMapAPIEntry
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(wire) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(wire))
	}

	high := wire["2783004996"]
	if int64(high.IPStart) != 2783004996 || int64(high.IPEnd) != 2783005000 {
		t.Errorf("addresses above 2^31 not parsed: start=%d end=%d", int64(high.IPStart), int64(high.IPEnd))
	}
	// priority arrives as a string here and must still parse.
	if int(high.Priority) != 50 {
		t.Errorf("priority = %d, want 50", int(high.Priority))
	}

	single := wire["167775560"]
	if FormatIPv4(uint32(single.IPStart)) != "10.0.13.72" {
		t.Errorf("start address = %q", FormatIPv4(uint32(single.IPStart)))
	}
	if single.Search != "no" || single.Subattributes == "" {
		t.Errorf("carried-over fields lost: %+v", single)
	}
}

func TestAddressMapRangeKey(t *testing.T) {
	if got := AddressMapRangeKey("10.0.0.1", "10.0.0.1"); got != "10.0.0.1" {
		t.Errorf("single address key = %q", got)
	}
	if got := AddressMapRangeKey("10.0.0.1", "10.0.0.9"); got != "10.0.0.1-10.0.0.9" {
		t.Errorf("range key = %q", got)
	}
}

func TestFindAddressMapByName(t *testing.T) {
	maps := []AddressMap{
		{Name: "host-a", IPStart: "10.0.0.1", IPEnd: "10.0.0.1"},
		{Name: "Range-B", IPStart: "10.0.1.0", IPEnd: "10.0.1.255"},
	}

	// Matching ignores case, as the Orchestrator does for application names.
	if got := FindAddressMapByName(maps, "RANGE-b"); got == nil || got.Name != "Range-B" {
		t.Errorf("case-insensitive lookup failed: %+v", got)
	}
	if got := FindAddressMapByName(maps, "missing"); got != nil {
		t.Errorf("unexpected match: %+v", got)
	}
	// A resource's own range never counts as a collision with itself.
	if got := FindAddressMapByName(maps, "host-a", "10.0.0.1"); got != nil {
		t.Errorf("own range should be ignored: %+v", got)
	}
	if got := FindAddressMapByName(maps, "Range-B", "10.0.0.1"); got == nil {
		t.Error("ignoring a different range must not hide the match")
	}
}

// TestFindOverlappingAddressMap covers the range relationships that matter:
// containment in both directions, partial overlap from either side, exact
// match, and the adjacent-but-disjoint case that must NOT be flagged.
func TestFindOverlappingAddressMap(t *testing.T) {
	maps := []AddressMap{
		{Name: "block-a", IPStart: "10.0.1.0", IPEnd: "10.0.1.255"},
		{Name: "single-b", IPStart: "10.0.5.5", IPEnd: "10.0.5.5"},
	}

	cases := []struct {
		name       string
		start, end string
		wantMatch  string // empty means no overlap expected
	}{
		{"identical range", "10.0.1.0", "10.0.1.255", "block-a"},
		{"contained inside", "10.0.1.10", "10.0.1.20", "block-a"},
		{"contains the entry", "10.0.0.0", "10.0.2.0", "block-a"},
		{"overlaps at the start", "10.0.0.128", "10.0.1.10", "block-a"},
		{"overlaps at the end", "10.0.1.200", "10.0.2.10", "block-a"},
		{"touches the first address", "10.0.0.0", "10.0.1.0", "block-a"},
		{"touches the last address", "10.0.1.255", "10.0.3.0", "block-a"},
		{"single address inside", "10.0.1.99", "10.0.1.99", "block-a"},
		{"single address entry hit", "10.0.5.0", "10.0.5.10", "single-b"},
		// Adjacent ranges share no address and must pass.
		{"ends right before", "10.0.0.0", "10.0.0.255", ""},
		{"starts right after", "10.0.2.0", "10.0.2.255", ""},
		{"far away", "192.168.0.0", "192.168.0.255", ""},
	}

	for _, c := range cases {
		got := FindOverlappingAddressMap(maps, c.start, c.end)
		switch {
		case c.wantMatch == "" && got != nil:
			t.Errorf("%s (%s-%s): unexpected overlap with %q", c.name, c.start, c.end, got.Name)
		case c.wantMatch != "" && got == nil:
			t.Errorf("%s (%s-%s): expected overlap with %q, got none", c.name, c.start, c.end, c.wantMatch)
		case c.wantMatch != "" && got != nil && got.Name != c.wantMatch:
			t.Errorf("%s: overlapped %q, want %q", c.name, got.Name, c.wantMatch)
		}
	}

	// A resource must not report an overlap with its own entry.
	if got := FindOverlappingAddressMap(maps, "10.0.1.0", "10.0.1.255", "10.0.1.0-10.0.1.255"); got != nil {
		t.Errorf("own range should be ignored, got %q", got.Name)
	}
	// Ignoring one range must not hide an overlap with another.
	if got := FindOverlappingAddressMap(maps, "10.0.1.0", "10.0.5.5", "10.0.1.0-10.0.1.255"); got == nil || got.Name != "single-b" {
		t.Errorf("expected overlap with single-b, got %+v", got)
	}

	// Ranges above 2^31 must compare correctly, not wrap.
	high := []AddressMap{{Name: "high", IPStart: "200.0.0.0", IPEnd: "200.0.0.255"}}
	if got := FindOverlappingAddressMap(high, "200.0.0.128", "200.0.1.0"); got == nil {
		t.Error("overlap above 2^31 not detected")
	}
	if got := FindOverlappingAddressMap(high, "199.255.255.0", "199.255.255.255"); got != nil {
		t.Errorf("unexpected overlap below the range: %q", got.Name)
	}
}
