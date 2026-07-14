package service

import "testing"

func TestAPIKeyService_RejectsV10AuthSnapshotWithoutModelsListConfig(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-models-list", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  10,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v10 auth snapshot to be rejected after models_list_config was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV14AuthSnapshotWithoutMergedGroupFields(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-v14", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 14},
	})

	if err != nil {
		t.Fatalf("expected stale v14 snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatal("expected v14 auth snapshot to be rejected after merged group fields were added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale v14 snapshot, got %#v", apiKey)
	}
}

// After the 0.1.155 merge the snapshot version was bumped to 16 to union the
// fork video-pricing/mixed-type-weight fields with upstream web-search per-call
// pricing. Both pre-merge branches shipped an incompatible v15; ensure a v15
// snapshot is rejected so no field is silently zero-filled.
func TestAPIKeyService_RejectsV15AuthSnapshotBeforeMergedGroupFieldUnion(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-v15", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 15},
	})

	if err != nil {
		t.Fatalf("expected stale v15 snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatal("expected v15 auth snapshot to be rejected after group field union bumped version to 16")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale v15 snapshot, got %#v", apiKey)
	}
}
