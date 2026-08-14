package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// fakeAppDefServer serves the compound classification endpoints: listing via
// GET /gms/rest/applicationDefinition and create/update/delete via
// POST/DELETE /gms/rest/applicationDefinition/compoundClassification.
type fakeAppDefServer struct {
	mu   sync.Mutex
	defs map[string]map[string]interface{} // keyed by ID
}

func (f *fakeAppDefServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/gms/rest/applicationDefinition", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.defs) == 0 {
			_, _ = w.Write([]byte(`{"result": "Not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.defs)
	})
	mux.HandleFunc("/gms/rest/applicationDefinition/compoundClassification", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		id := r.URL.Query().Get("id")
		switch r.Method {
		case http.MethodPost:
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.defs[id] = body
			_, _ = w.Write([]byte("{}"))
		case http.MethodDelete:
			delete(f.defs, id)
			_, _ = w.Write([]byte("{}"))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

// fakeAppGroupServer serves the application group endpoints: GET lists all
// groups keyed by name, POST replaces the complete set.
type fakeAppGroupServer struct {
	mu     sync.Mutex
	groups map[string]map[string]interface{} // keyed by group name
}

func (f *fakeAppGroupServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/gms/rest/applicationDefinition/applicationTags", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			if len(f.groups) == 0 {
				_, _ = w.Write([]byte(`{"result": "Not found"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(f.groups)
		case http.MethodPost:
			var body map[string]map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.groups = body
			_, _ = w.Write([]byte("{}"))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

func appGroupConfig(serverURL, name string) string {
	return fmt.Sprintf(`
provider "arubasdwan" {
  orchestrator_url = %q
  api_key          = "test-key"
}

resource "arubasdwan_application_group" "test" {
  name = %q
  apps = ["SomeApp"]
}
`, serverURL, name)
}

// TestApplicationGroupDuplicateNameRejected verifies that creating an
// application group whose name already exists on the Orchestrator is
// rejected instead of silently replacing the existing group's members.
func TestApplicationGroupDuplicateNameRejected(t *testing.T) {
	fake := &fakeAppGroupServer{groups: map[string]map[string]interface{}{
		"existing-group": {"apps": []string{"OtherApp"}, "parentGroup": nil},
	}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Same name as the pre-existing group: rejected at plan time.
				Config:      appGroupConfig(server.URL, "existing-group"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`name already in use`),
			},
			{
				// A unique name is created normally.
				Config: appGroupConfig(server.URL, "new-group"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_application_group.test", "id", "new-group"),
				),
			},
			{
				// Renaming replaces the resource; the plan for the
				// replacement hits the guard as well.
				Config:      appGroupConfig(server.URL, "EXISTING-group"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`name already in use`),
			},
		},
	})
}

func compoundConfig(serverURL, name string) string {
	return fmt.Sprintf(`
provider "arubasdwan" {
  orchestrator_url = %q
  api_key          = "test-key"
}

resource "arubasdwan_app_compound_classification" "test" {
  name   = %q
  dst_ip = "192.0.2.0/24"
}
`, serverURL, name)
}

// TestAppCompoundDuplicateNameRejected verifies that creating or renaming a
// compound classification to a name that already exists on the Orchestrator
// is rejected instead of silently adding a duplicate definition.
func TestAppCompoundDuplicateNameRejected(t *testing.T) {
	fake := &fakeAppDefServer{defs: map[string]map[string]interface{}{
		"7": {
			"id": json.Number(strconv.Itoa(7)), "name": "ExistingApp",
			"description": "created outside Terraform", "confidence": json.Number("100"),
			"disabled": false, "dst_ip": "198.51.100.0/24",
		},
	}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Same name as the pre-existing definition: rejected at
				// plan time already.
				Config:      compoundConfig(server.URL, "ExistingApp"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`name already in use`),
			},
			{
				// A unique name is created normally.
				Config: compoundConfig(server.URL, "NewApp"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_app_compound_classification.test", "name", "NewApp"),
					// The name identifies the definition; the numeric rule ID is
					// reported separately because it shifts on deletions.
					resource.TestCheckResourceAttr("arubasdwan_app_compound_classification.test", "id", "NewApp"),
					resource.TestCheckResourceAttr("arubasdwan_app_compound_classification.test", "rule_id", "8"),
				),
			},
			{
				// Renaming onto an existing name: rejected at plan time.
				Config:      compoundConfig(server.URL, "ExistingApp"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`name already in use`),
			},
		},
	})
}

// seedCompound adds a compound classification as the Orchestrator reports it.
func (f *fakeAppDefServer) seedCompound(id int, name, description string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defs[strconv.Itoa(id)] = map[string]interface{}{
		"id": json.Number(strconv.Itoa(id)), "name": name,
		"description": description, "confidence": json.Number("100"),
		"disabled": false, "dst_ip": "192.0.2.0/24",
	}
}

// deleteAndCompact removes the entry with the given name and renumbers the
// remaining ones from 1, the way the Orchestrator reassigns IDs on deletion:
// the ID doubles as the rule's position in the priority order, so entries
// above the deleted one shift down.
func (f *fakeAppDefServer) deleteAndCompact(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]int, 0, len(f.defs))
	for k := range f.defs {
		n, _ := strconv.Atoi(k)
		ids = append(ids, n)
	}
	sort.Ints(ids)

	compacted := make(map[string]map[string]interface{}, len(f.defs))
	next := 1
	for _, old := range ids {
		entry := f.defs[strconv.Itoa(old)]
		if entry["name"] == name {
			continue
		}
		entry["id"] = json.Number(strconv.Itoa(next))
		compacted[strconv.Itoa(next)] = entry
		next++
	}
	f.defs = compacted
}

// lookupCompound returns the entry carrying the given name, or nil.
func (f *fakeAppDefServer) lookupCompound(name string) map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.defs {
		if e["name"] == name {
			return e
		}
	}
	return nil
}

func compoundConfigWithDescription(serverURL, name, description string) string {
	return fmt.Sprintf(`
provider "arubasdwan" {
  orchestrator_url = %q
  api_key          = "test-key"
}

resource "arubasdwan_app_compound_classification" "test" {
  name        = %q
  description = %q
  dst_ip      = "192.0.2.0/24"
}
`, serverURL, name, description)
}

// TestCompoundSurvivesIDShift is the regression test for the Orchestrator
// renumbering compound classifications on deletion. A rule managed by
// Terraform moves to a lower ID when an unrelated rule below it is deleted;
// the update that follows must still reach that rule and must not overwrite
// whichever one moved into the old slot.
func TestCompoundSurvivesIDShift(t *testing.T) {
	fake := &fakeAppDefServer{defs: map[string]map[string]interface{}{}}
	fake.seedCompound(1, "AppOne", "unrelated, deleted later")
	fake.seedCompound(2, "AppTwo", "unrelated bystander")
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: compoundConfigWithDescription(server.URL, "Managed", "before"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_app_compound_classification.test", "id", "Managed"),
					resource.TestCheckResourceAttr("arubasdwan_app_compound_classification.test", "rule_id", "3"),
				),
			},
			{
				// AppOne is deleted outside Terraform. The Orchestrator
				// renumbers, so Managed drops from ID 3 to ID 2.
				PreConfig: func() { fake.deleteAndCompact("AppOne") },
				Config:    compoundConfigWithDescription(server.URL, "Managed", "after"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_app_compound_classification.test", "rule_id", "2"),
					func(s *terraform.State) error {
						// The update must have landed on Managed...
						managed := fake.lookupCompound("Managed")
						if managed == nil {
							return fmt.Errorf("Managed disappeared")
						}
						if managed["description"] != "after" {
							return fmt.Errorf("Managed not updated: description = %v", managed["description"])
						}
						// ...and must not have touched the rule that moved
						// into the slot Managed used to occupy.
						other := fake.lookupCompound("AppTwo")
						if other == nil {
							return fmt.Errorf("AppTwo was destroyed by the update")
						}
						if other["description"] != "unrelated bystander" {
							return fmt.Errorf("AppTwo was overwritten: description = %v", other["description"])
						}
						return nil
					},
				),
			},
		},
	})
}

// TestCompoundDeleteAfterIDShift proves the delete path resolves the ID too:
// destroying a resource whose ID shifted must remove that rule, not the one
// that took its place.
func TestCompoundDeleteAfterIDShift(t *testing.T) {
	fake := &fakeAppDefServer{defs: map[string]map[string]interface{}{}}
	fake.seedCompound(1, "AppOne", "unrelated, deleted later")
	fake.seedCompound(2, "AppTwo", "must survive the destroy")
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		CheckDestroy: func(s *terraform.State) error {
			if fake.lookupCompound("Managed") != nil {
				return fmt.Errorf("Managed still exists after destroy")
			}
			if fake.lookupCompound("AppTwo") == nil {
				return fmt.Errorf("destroy removed AppTwo instead of Managed")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: compoundConfigWithDescription(server.URL, "Managed", "doomed"),
				Check: resource.TestCheckResourceAttr(
					"arubasdwan_app_compound_classification.test", "rule_id", "3"),
			},
			{
				// Shift the IDs, then let the framework destroy everything.
				PreConfig: func() { fake.deleteAndCompact("AppOne") },
				Config:    compoundConfigWithDescription(server.URL, "Managed", "doomed"),
				Check: resource.TestCheckResourceAttr(
					"arubasdwan_app_compound_classification.test", "rule_id", "2"),
			},
		},
	})
}

// TestCompoundImportByName covers importing through the application name,
// which replaced the numeric ID as the import identifier.
func TestCompoundImportByName(t *testing.T) {
	fake := &fakeAppDefServer{defs: map[string]map[string]interface{}{}}
	fake.seedCompound(1, "Filler", "occupies the first slot")
	fake.seedCompound(2, "Importable", "created outside Terraform")
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:        compoundConfigWithDescription(server.URL, "Importable", "created outside Terraform"),
				ResourceName:  "arubasdwan_app_compound_classification.test",
				ImportState:   true,
				ImportStateId: "Importable",
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported state, got %d", len(states))
					}
					attrs := states[0].Attributes
					if attrs["name"] != "Importable" || attrs["id"] != "Importable" {
						return fmt.Errorf("unexpected identity: id=%q name=%q", attrs["id"], attrs["name"])
					}
					if attrs["rule_id"] != "2" {
						return fmt.Errorf("rule_id = %q, want 2", attrs["rule_id"])
					}
					return nil
				},
			},
		},
	})
}
