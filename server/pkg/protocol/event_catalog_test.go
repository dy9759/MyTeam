package protocol

import "testing"

func TestCatalogNonEmpty(t *testing.T) {
	if len(EventCatalog) == 0 {
		t.Fatal("catalog is empty")
	}
}

func TestCatalogCount(t *testing.T) {
	// Per PRD §2.2 — spot check count is in the expected range.
	// 20 project + 8 account + 10 session + 3 inbox = 41 events.
	want := 41
	if len(EventCatalog) != want {
		t.Errorf("expected %d events, got %d", want, len(EventCatalog))
	}
}

func TestEventTypesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range EventCatalog {
		if seen[e.Type] {
			t.Errorf("duplicate event type %q", e.Type)
		}
		seen[e.Type] = true
	}
}

func TestEventsHaveDomain(t *testing.T) {
	for _, e := range EventCatalog {
		if e.Domain == "" {
			t.Errorf("event %q missing domain", e.Type)
		}
	}
}

func TestEventsHaveDescription(t *testing.T) {
	for _, e := range EventCatalog {
		if e.Description == "" {
			t.Errorf("event %q missing description", e.Type)
		}
	}
}

func TestCatalogByType(t *testing.T) {
	m := CatalogByType()
	if _, ok := m["task:status_changed"]; !ok {
		t.Error("missing task:status_changed in lookup")
	}
	if len(m) != len(EventCatalog) {
		t.Errorf("map/catalog length mismatch: %d vs %d", len(m), len(EventCatalog))
	}
}

func TestCatalogByDomain(t *testing.T) {
	by := CatalogByDomain()
	if len(by[DomainProject]) < 15 {
		t.Errorf("expected at least 15 project events, got %d", len(by[DomainProject]))
	}
	if len(by[DomainInbox]) != 3 {
		t.Errorf("expected 3 inbox events, got %d", len(by[DomainInbox]))
	}
}
