package dynamo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/larpspeak/media-relay/internal/config"
	"github.com/larpspeak/media-relay/internal/domain"
)

// fakeDynamo captures TransactWriteItems requests as "op:table" per item and
// answers with cancel (per-item cancellation codes) when it is set.
func fakeDynamo(t *testing.T, cancel ...string) (*Store, *[]string) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			TransactItems []map[string]struct{ TableName string }
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, it := range req.TransactItems {
			for op, v := range it {
				got = append(got, op+":"+v.TableName)
			}
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		if len(cancel) == 0 {
			w.Write([]byte(`{}`))
			return
		}
		var reasons []map[string]string
		for _, c := range cancel {
			reasons = append(reasons, map[string]string{"Code": c})
		}
		body, _ := json.Marshal(map[string]any{"__type": "TransactionCanceledException", "message": "cancelled", "CancellationReasons": reasons})
		w.WriteHeader(http.StatusBadRequest)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	db := dynamodb.New(dynamodb.Options{BaseEndpoint: aws.String(srv.URL), Region: "us-east-1",
		Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1})
	cfg := config.Config{SessionsTable: "sessions", NodesTable: "nodes", OutboxTable: "outbox", ProcessedTable: "processed"}
	return &Store{DB: db, Cfg: cfg}, &got
}

func msg(t *testing.T) domain.Outgoing {
	e, err := domain.NewEnvelope("RelayMoved", "c1", map[string]string{"callId": "c1"})
	if err != nil {
		t.Fatal(err)
	}
	return domain.Outgoing{Dest: "relay-results", Envelope: e}
}

func TestItemOrder(t *testing.T) {
	ctx := context.Background()
	ss := domain.Session{CallID: "c1", Owner: "pod-dead"}
	cases := map[string]struct {
		run  func(*Store) error
		want []string
	}{
		"allocate": {func(s *Store) error { return s.Allocate(ctx, "m1", ss, msg(t)) },
			[]string{"Put:processed", "Put:sessions", "Put:outbox"}},
		"release": {func(s *Store) error { return s.Release(ctx, "m1", "c1", "call ended") },
			[]string{"Put:processed", "Update:sessions"}},
		"member": {func(s *Store) error { return s.Member(ctx, "m1", "c1", "ADD", "ana") },
			[]string{"Put:processed", "Update:sessions"}},
		"mark dead": {func(s *Store) error { return s.MarkDead(ctx, domain.Node{ID: "pod-b"}, "pod-a", 2) },
			[]string{"ConditionCheck:nodes", "Update:nodes"}},
		"move": {func(s *Store) error { return s.Move(ctx, "pod-a", 2, ss, domain.Node{ID: "pod-b"}, msg(t)) },
			[]string{"ConditionCheck:nodes", "Update:sessions", "Put:outbox"}},
		"emit": {func(s *Store) error { return s.Emit(ctx, msg(t)) }, []string{"Put:outbox"}},
	}
	for name, tc := range cases {
		s, got := fakeDynamo(t)
		if err := tc.run(s); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(*got, tc.want) {
			t.Errorf("%s: items %v, want %v", name, *got, tc.want)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	ctx := context.Background()
	for cancel, want := range map[[2]string]error{
		{"ConditionalCheckFailed", "None"}: domain.ErrDuplicate,
		{"None", "ConditionalCheckFailed"}: domain.ErrReleased,
	} {
		s, _ := fakeDynamo(t, cancel[:]...)
		if err := s.Release(ctx, "m1", "c1", "x"); !errors.Is(err, want) {
			t.Errorf("%v: got %v, want %v", cancel, err, want)
		}
	}
}
