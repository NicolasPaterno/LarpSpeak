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

	"github.com/larpspeak/call-signaling/internal/config"
	"github.com/larpspeak/call-signaling/internal/domain"
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
	cfg := config.Config{CallsTable: "calls", UsersTable: "users", OutboxTable: "outbox", ProcessedTable: "processed"}
	return &Store{DB: db, Cfg: cfg}, &got
}

func msg(t *testing.T) domain.Outgoing {
	e, err := domain.NewEnvelope("ParticipantLeft", "c1", map[string]string{"callId": "c1"})
	if err != nil {
		t.Fatal(err)
	}
	return domain.Outgoing{Dest: domain.DestTopic, Envelope: e}
}

func TestItemOrder(t *testing.T) {
	ctx := context.Background()
	c := &domain.Call{ID: "c1", CallerID: "ana", Taken: 2}
	cases := map[string]struct {
		run  func(*Store) error
		want []string
	}{
		"create": {func(s *Store) error { return s.Create(ctx, *c, msg(t)) },
			[]string{"Put:calls", "Put:outbox", "Put:users"}},
		"join": {func(s *Store) error { return s.Join(ctx, "c1", "bob", msg(t)) },
			[]string{"Update:calls", "Put:outbox", "Put:users"}},
		"leave last, from a message": {func(s *Store) error { return s.Leave(ctx, "m1", c, "ana", true, 1, msg(t), msg(t)) },
			[]string{"Put:processed", "Update:calls", "Delete:users", "Put:outbox", "Put:outbox"}},
		"fail, from the sweeper": {func(s *Store) error { return s.Fail(ctx, "", "c1", "relay timeout", msg(t)) },
			[]string{"Update:calls", "Put:outbox"}},
		"ack": {func(s *Store) error { return s.Ack(ctx, "m1", msg(t)) },
			[]string{"Put:processed", "Put:outbox"}},
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
	const ccf, none = "ConditionalCheckFailed", "None"
	c := &domain.Call{ID: "c1", CallerID: "ana", Taken: 1}
	cases := map[string]struct {
		cancel []string
		run    func(*Store) error
		want   error
	}{
		"join seat":       {[]string{ccf, none, none}, func(s *Store) error { return s.Join(ctx, "c1", "bob", msg(t)) }, domain.ErrSeatLost},
		"join user":       {[]string{none, none, ccf}, func(s *Store) error { return s.Join(ctx, "c1", "bob", msg(t)) }, domain.ErrStateChanged},
		"create user":     {[]string{none, none, ccf}, func(s *Store) error { return s.Create(ctx, *c, msg(t)) }, domain.ErrStateChanged},
		"leave duplicate": {[]string{ccf, none, none, none}, func(s *Store) error { return s.Leave(ctx, "m1", c, "ana", false, 1, msg(t)) }, domain.ErrDuplicate},
		"leave counter":   {[]string{none, ccf, none, none}, func(s *Store) error { return s.Leave(ctx, "m1", c, "ana", false, 1, msg(t)) }, domain.ErrStateChanged},
		"fail no msg":     {[]string{ccf, none}, func(s *Store) error { return s.Fail(ctx, "", "c1", "x", msg(t)) }, domain.ErrStateChanged},
	}
	for name, tc := range cases {
		s, _ := fakeDynamo(t, tc.cancel...)
		if err := tc.run(s); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
	}
}
