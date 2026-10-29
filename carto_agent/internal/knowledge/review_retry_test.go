package knowledge

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func newCompilationRetryReview(t *testing.T, args map[string]any) (*ReviewStore, *Review) {
	t.Helper()
	store := NewReviewStore(t.TempDir())
	source := "部队到达甲地。"
	graph := Graph{"spatial_entities": []any{map[string]any{"entity_id": "p1", "name": "甲地", "lon": 110.0, "lat": 33.0}}}
	review, err := store.Create(ReviewCreateInput{
		SessionID: "retry", SourceText: source,
		Contract:       Contract{ContractID: "v1", SourceSHA256: reviewHash([]byte(source)), JSONSchema: json.RawMessage(`{}`)},
		OriginalResult: &Result{Graph: graph}, Graph: graph, CompileArgs: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, review
}

func failConfirmedCompilation(t *testing.T, store *ReviewStore, review *Review) *Review {
	t.Helper()
	if _, _, err := store.Claim(review.SessionID, review.ReviewID, review.Revision, "user-confirmed"); err != nil {
		t.Fatal(err)
	}
	failed, err := store.Fail(review.SessionID, review.ReviewID, review.Revision, []ReviewError{{Message: "路线预设不存在"}})
	if err != nil {
		t.Fatal(err)
	}
	return failed
}

func TestRetryCompilationPreservesConfirmationAndOnlyChangesDrawingPresets(t *testing.T) {
	for _, scenario := range []string{"missing_original_presets", "existing_compile_options", "built_in_defaults"} {
		t.Run(scenario, func(t *testing.T) {
			existing := scenario != "missing_original_presets"
			var args map[string]any
			if existing {
				args = map[string]any{"route_style": "旧路线", "symbols": "旧符号", "rules": "保留事实规则", "output_dir": "original-output", "other": map[string]any{"value": "retained"}}
			}
			store, review := newCompilationRetryReview(t, args)
			failed := failConfirmedCompilation(t, store, review)
			presets := map[string]string{"route_style": "红蓝对比", "symbols": "军标", "rules": "不能替换原有事实规则"}
			if scenario == "built_in_defaults" {
				presets = nil
			}
			retry, err := store.RetryCompilation(review.SessionID, review.ReviewID, review.Revision, presets)
			if err != nil {
				t.Fatal(err)
			}
			if retry.Status != ReviewCompiling || len(retry.Errors) != 0 || retry.ClaimedAt != failed.ClaimedAt || retry.ClaimRequestID != failed.ClaimRequestID || retry.ClaimedRevision != failed.ClaimedRevision || retry.Revision != failed.Revision {
				t.Fatalf("retry changed user confirmation or did not start: %+v", retry)
			}
			if !reflect.DeepEqual(retry.Graph, failed.Graph) || !reflect.DeepEqual(retry.OriginalResult, failed.OriginalResult) || retry.SourceText != failed.SourceText {
				t.Fatal("retry modified confirmed facts or original extraction")
			}
			want := map[string]any{"route_style": "红蓝对比", "symbols": "军标"}
			if scenario == "built_in_defaults" {
				want = map[string]any{}
			}
			if existing {
				for _, key := range []string{"rules", "output_dir", "other"} {
					want[key] = args[key]
				}
			}
			if !reflect.DeepEqual(retry.CompileArgs, want) {
				t.Fatalf("retry overwrote unrelated options: %#v", retry.CompileArgs)
			}
			persisted, err := store.Get(review.SessionID, review.ReviewID)
			if err != nil || !reflect.DeepEqual(persisted, retry) {
				t.Fatalf("retry was not persisted: %v", err)
			}
			if _, err := store.RetryCompilation(review.SessionID, review.ReviewID, review.Revision, nil); !errors.Is(err, ErrReviewConflict) {
				t.Fatalf("second retry accepted while compiling: %v", err)
			}
			completed, err := store.Complete(review.SessionID, review.ReviewID, review.Revision, retry.Graph, map[string]any{"dataset_dir": "compiled-dataset"})
			if err != nil || completed.Status != ReviewCompiled || completed.ClaimedAt != failed.ClaimedAt {
				t.Fatalf("retry cannot complete with original confirmation: %+v %v", completed, err)
			}
		})
	}
}

func TestRetryCompilationRequiresUnchangedConfirmedFailedRevision(t *testing.T) {
	for _, state := range []string{"never_confirmed", "stale_revision", "edited_after_failure", "missing_confirmation_time", "unconfirmed_revision", "already_completed"} {
		t.Run(state, func(t *testing.T) {
			store, review := newCompilationRetryReview(t, nil)
			revision := review.Revision
			if state != "never_confirmed" {
				review = failConfirmedCompilation(t, store, review)
			}
			switch state {
			case "stale_revision":
				revision++
			case "edited_after_failure":
				review.Graph["spatial_entities"].([]any)[0].(map[string]any)["name"] = "人工改为乙地"
				var err error
				review, err = store.SaveDraft(review.SessionID, review.ReviewID, revision, review.Graph)
				if err != nil {
					t.Fatal(err)
				}
				revision = review.Revision
			case "missing_confirmation_time", "unconfirmed_revision":
				// Historical failed records must prove that this exact revision was
				// confirmed, not merely carry a failed status.
				if state == "missing_confirmation_time" {
					review.ClaimedAt = ""
				} else {
					review.ClaimedRevision = 0
				}
				if err := store.writeLocked(review); err != nil {
					t.Fatal(err)
				}
			case "already_completed":
				if _, _, err := store.Claim(review.SessionID, review.ReviewID, revision, "retry-confirmed"); err != nil {
					t.Fatal(err)
				}
				var err error
				review, err = store.Complete(review.SessionID, review.ReviewID, revision, review.Graph, map[string]any{"dataset_dir": "compiled-dataset"})
				if err != nil {
					t.Fatal(err)
				}
			}
			before, err := store.Get(review.SessionID, review.ReviewID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.RetryCompilation(review.SessionID, review.ReviewID, revision, map[string]string{"route_style": "红蓝对比"}); !errors.Is(err, ErrReviewConflict) {
				t.Fatalf("%s accepted: %v", state, err)
			}
			after, err := store.Get(review.SessionID, review.ReviewID)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected retry changed saved review: %v", err)
			}
		})
	}
}
