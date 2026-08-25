package handlers

import (
	"testing"
	"time"
)

func TestExpiringSet(t *testing.T) {
	set := newExpiringSet(5 * time.Minute)
	now := time.Unix(100, 0)

	if set.contains("machine-1", now) {
		t.Fatal("new set unexpectedly contains machine-1")
	}
	set.add("machine-1", now)
	if !set.contains("machine-1", now.Add(4*time.Minute)) {
		t.Fatal("entry expired before its TTL")
	}
	if set.contains("machine-1", now.Add(5*time.Minute)) {
		t.Fatal("entry remained at its expiration boundary")
	}
}

func TestExpiringSetPrunesExpiredEntriesOnAdd(t *testing.T) {
	set := newExpiringSet(time.Minute)
	now := time.Unix(100, 0)
	set.add("old", now)
	set.add("new", now.Add(2*time.Minute))

	if _, ok := set.entries["old"]; ok {
		t.Fatal("expired entry was not pruned")
	}
	if !set.contains("new", now.Add(2*time.Minute)) {
		t.Fatal("new entry was not retained")
	}
}
