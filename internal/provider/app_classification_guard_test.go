package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
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
					resource.TestCheckResourceAttr("arubasdwan_app_compound_classification.test", "id", "8"),
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
