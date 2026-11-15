package handlerregistration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rancher/go-rancher/v2"
)

func TestRetireLegacyRegistration(t *testing.T) {
	legacy := Legacy{
		Name:      "goMachineService-machine",
		Priority:  2000,
		OnError:   "machinedriver.error",
		Processes: []string{"machinedriver.reactivate", "ping"},
	}
	tests := []struct {
		name           string
		handlerName    string
		uuid           string
		ambiguous      bool
		unexpectedMap  bool
		missingAction  bool
		failMaps       bool
		failDeactivate bool
		wantDeactivate int32
		wantError      string
	}{
		{name: "generated legacy UUID", handlerName: legacy.Name, uuid: "00000000-0000-4000-8000-000000000001", wantDeactivate: 1},
		{name: "router name as legacy UUID", handlerName: legacy.Name, uuid: legacy.Name, wantDeactivate: 1},
		{name: "clean database"},
		{name: "current router", handlerName: "hostProvisioner-machine", uuid: legacy.Name},
		{name: "lookalike UUID", handlerName: legacy.Name, uuid: "goMachineService-agent"},
		{name: "ambiguous registrations", handlerName: legacy.Name, uuid: legacy.Name, ambiguous: true, wantError: "multiple matching active registrations"},
		{name: "unrelated mapping", handlerName: legacy.Name, uuid: legacy.Name, unexpectedMap: true},
		{name: "mapping read failure", handlerName: legacy.Name, uuid: legacy.Name, failMaps: true, wantError: "503"},
		{name: "missing action", handlerName: legacy.Name, uuid: legacy.Name, missingAction: true, wantError: "has no deactivate action"},
		{name: "deactivation rejection", handlerName: legacy.Name, uuid: legacy.Name, failDeactivate: true, wantDeactivate: 1, wantError: "409"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var deactivateCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				base := "http://" + r.Host + "/v2-beta"
				var response interface{}
				switch r.Method + " " + r.URL.Path {
				case "GET /v2-beta":
					w.Header().Set("X-API-Schemas", base)
					schemas := []client.Schema{}
					for resource, collection := range map[string]string{
						"externalHandler":                          "externalHandlers",
						"externalHandlerExternalHandlerProcessMap": "externalHandlerExternalHandlerProcessMaps",
						"externalHandlerProcess":                   "externalHandlerProcesses",
					} {
						schemas = append(schemas, client.Schema{
							Resource:          client.Resource{Id: resource, Links: map[string]string{"collection": base + "/" + collection}},
							CollectionMethods: []string{"GET"},
							ResourceMethods:   []string{"GET"},
						})
					}
					response = client.Schemas{Data: schemas}
				case "GET /v2-beta/externalHandlers":
					if query := r.URL.Query(); query.Get("name") != legacy.Name || query.Get("state") != "active" || query.Get("limit") != "25" {
						t.Errorf("unexpected handler filters: %v", query)
					}
					handlers := []client.ExternalHandler{}
					if tt.handlerName != "" && deactivateCalls.Load() == 0 {
						actions := map[string]string{"remove": base + "/unexpected-remove"}
						if !tt.missingAction {
							actions["deactivate"] = base + "/externalHandlers/1eh1?action=deactivate"
						}
						handlers = append(handlers, client.ExternalHandler{
							Resource: client.Resource{Id: "1eh1", Actions: actions},
							Name:     tt.handlerName,
							Uuid:     tt.uuid,
							State:    "active",
							Priority: legacy.Priority,
						})
						if tt.ambiguous {
							other := handlers[0]
							other.Id = "1eh2"
							other.Uuid = "00000000-0000-4000-8000-000000000002"
							handlers = append(handlers, other)
						}
					}
					response = client.ExternalHandlerCollection{Data: handlers}
				case "GET /v2-beta/externalHandlerExternalHandlerProcessMaps":
					query := r.URL.Query()
					id := query.Get("externalHandlerId")
					if (id != "1eh1" && !(tt.ambiguous && id == "1eh2")) || query.Get("state") != "active" || query.Get("limit") != "25" {
						t.Errorf("unexpected mapping filters: %v", query)
					}
					if tt.failMaps {
						http.Error(w, "mapping temporarily unavailable", http.StatusServiceUnavailable)
						return
					}
					response = client.ExternalHandlerExternalHandlerProcessMapCollection{
						Data: []client.ExternalHandlerExternalHandlerProcessMap{
							{ExternalHandlerId: id, ExternalHandlerProcessId: "1ehp1", State: "active", OnError: legacy.OnError},
							{ExternalHandlerId: id, ExternalHandlerProcessId: "1ehp2", State: "active", OnError: legacy.OnError},
						},
					}
				case "GET /v2-beta/externalHandlerProcesses/1ehp1":
					response = client.ExternalHandlerProcess{Name: "ping", State: "active"}
				case "GET /v2-beta/externalHandlerProcesses/1ehp2":
					name := "machinedriver.reactivate"
					if tt.unexpectedMap {
						name = "physicalhost.create"
					}
					response = client.ExternalHandlerProcess{Name: name, State: "active"}
				case "POST /v2-beta/externalHandlers/1eh1":
					if r.URL.Query().Get("action") != "deactivate" {
						t.Errorf("unexpected action: %s", r.URL)
					}
					deactivateCalls.Add(1)
					if tt.failDeactivate {
						http.Error(w, "deactivation rejected", http.StatusConflict)
						return
					}
					response = client.ExternalHandler{State: "deactivating"}
				case "GET /v2-beta/externalHandlers/1eh1":
					if deactivateCalls.Load() != 1 || tt.failDeactivate {
						t.Error("read back a registration that was never deactivated")
					}
					response = client.ExternalHandler{State: "inactive"}
				default:
					t.Errorf("unexpected API request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()

			api, err := client.NewRancherClient(&client.ClientOpts{Url: server.URL + "/v2-beta"})
			if err != nil {
				t.Fatal(err)
			}
			err = legacy.Deactivate(api)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if err := legacy.Deactivate(api); err != nil {
				t.Fatalf("repeat reconciliation: %v", err)
			}
			if got := deactivateCalls.Load(); got != tt.wantDeactivate {
				t.Fatalf("deactivation calls = %d, want %d", got, tt.wantDeactivate)
			}
		})
	}
}
