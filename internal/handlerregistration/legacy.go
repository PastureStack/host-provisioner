package handlerregistration

import (
	"fmt"
	"regexp"

	"github.com/PastureStack/host-provisioner/internal/logsafe"
	"github.com/PastureStack/host-provisioner/logging"
	"github.com/rancher/event-subscriber/events"
	"github.com/rancher/go-rancher/v2"
)

// Legacy identifies a historical host-provisioner registration.
type Legacy struct {
	Name      string
	Priority  int64
	OnError   string
	Processes []string
}

// Some Engine schemas allocate the UUID instead of persisting the supplied router name.
var generatedHandlerUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Deactivate stops dispatch to the old subscriber and preserves its registration for rollback.
func (legacy Legacy) Deactivate(api *client.RancherClient) error {
	if err := legacy.deactivate(api); err != nil {
		return fmt.Errorf("deactivate legacy registration %s: %w", legacy.Name, err)
	}
	return nil
}

func (legacy Legacy) deactivate(api *client.RancherClient) error {
	opts := client.NewListOpts()
	opts.Filters["name"] = legacy.Name
	opts.Filters["state"] = "active"
	opts.Filters["limit"] = "25"
	handlers, err := api.ExternalHandler.List(opts)
	if err != nil {
		return err
	}
	if handlers.Pagination != nil && handlers.Pagination.Next != "" {
		return fmt.Errorf("too many active registrations for %s", legacy.Name)
	}
	var owned *client.ExternalHandler
	for _, handler := range handlers.Data {
		if handler.Name != legacy.Name || handler.State != "active" || handler.Removed != "" {
			continue
		}
		match, err := legacy.matches(api, handler)
		if err != nil {
			return err
		}
		if !match {
			logging.Logger().WithField("handlerId", logsafe.Value(handler.Id)).Warn("Skipping legacy handler with an unexpected registration")
			continue
		}
		if owned != nil {
			return fmt.Errorf("found multiple matching active registrations for %s", legacy.Name)
		}
		owned = &handler
	}
	if owned == nil {
		return nil
	}
	if owned.Actions["deactivate"] == "" {
		return fmt.Errorf("active handler %q has no deactivate action", owned.Id)
	}
	if _, err := api.ExternalHandler.ActionDeactivate(owned); err != nil {
		return err
	}
	if err := events.WaitForTransition(func() (bool, error) {
		current, err := api.ExternalHandler.ById(owned.Id)
		if err != nil {
			return false, err
		}
		return current == nil || current.State == "inactive" || current.State == "removed", nil
	}); err != nil {
		return err
	}
	logging.Logger().WithField("handlerId", logsafe.Value(owned.Id)).Info("Deactivated legacy handler registration")
	return nil
}

func (legacy Legacy) matches(api *client.RancherClient, handler client.ExternalHandler) (bool, error) {
	if handler.Priority != legacy.Priority || (handler.Uuid != legacy.Name && !generatedHandlerUUID.MatchString(handler.Uuid)) {
		return false, nil
	}
	opts := client.NewListOpts()
	opts.Filters["externalHandlerId"] = handler.Id
	opts.Filters["state"] = "active"
	opts.Filters["limit"] = "25"
	mappings, err := api.ExternalHandlerExternalHandlerProcessMap.List(opts)
	if err != nil {
		return false, err
	}
	if len(mappings.Data) != len(legacy.Processes) || (mappings.Pagination != nil && mappings.Pagination.Next != "") {
		return false, nil
	}
	expected := make(map[string]bool, len(legacy.Processes))
	for _, name := range legacy.Processes {
		expected[name] = true
	}
	for _, mapping := range mappings.Data {
		if mapping.ExternalHandlerId != handler.Id || mapping.State != "active" || mapping.Removed != "" ||
			mapping.EventName != "" || mapping.OnError != legacy.OnError {
			return false, nil
		}
		process, err := api.ExternalHandlerProcess.ById(mapping.ExternalHandlerProcessId)
		if err != nil {
			return false, err
		}
		if process == nil || process.State != "active" || process.Removed != "" || !expected[process.Name] {
			return false, nil
		}
		delete(expected, process.Name)
	}
	return len(expected) == 0, nil
}
