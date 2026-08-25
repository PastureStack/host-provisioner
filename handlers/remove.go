package handlers

import (
	"sync"
	"time"

	"github.com/rancher/event-subscriber/events"
	client "github.com/rancher/go-rancher/v2"
	"github.com/sirupsen/logrus"
)

type expiringSet struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]time.Time
}

func newExpiringSet(ttl time.Duration) *expiringSet {
	return &expiringSet{ttl: ttl, entries: make(map[string]time.Time)}
}

func (s *expiringSet) contains(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	expires, ok := s.entries[key]
	if !ok {
		return false
	}
	if !now.Before(expires) {
		delete(s.entries, key)
		return false
	}
	return true
}

func (s *expiringSet) add(key string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for existing, expires := range s.entries {
		if !now.Before(expires) {
			delete(s.entries, existing)
		}
	}
	s.entries[key] = now.Add(s.ttl)
}

var removeCache = newExpiringSet(5 * time.Minute)

func PurgeMachine(event *events.Event, apiClient *client.RancherClient) error {
	logger.WithFields(logrus.Fields{
		"resourceId": event.ResourceID,
		"eventId":    event.ID,
	}).Info("Purging Machine")

	if removeCache.contains(event.ResourceID, time.Now()) {
		logger.WithFields(logrus.Fields{
			"resourceId": event.ResourceID,
			"eventId":    event.ID,
		}).Info("Machine already purged")
		return publishReply(newReply(event), apiClient)
	}

	machine, machineDirs, err := preEvent(event, apiClient)
	if err != nil || machine == nil {
		return err
	}
	defer removeMachineDir(machineDirs.jailDir)

	mExists, err := machineExists(machineDirs.jailDir, machine.Name)
	if err != nil {
		return err
	}

	if mExists {
		if err := deleteMachine(machineDirs.jailDir, machine); err != nil {
			return err
		}
	}

	removeCache.add(event.ResourceID, time.Now())

	logger.WithFields(logrus.Fields{
		"resourceId":        event.ResourceID,
		"machineExternalId": machine.ExternalId,
		"machineDir":        machineDirs.jailDir,
	}).Info("Machine purged")

	removeMachineDir(machineDirs.jailDir)

	return publishReply(newReply(event), apiClient)
}
