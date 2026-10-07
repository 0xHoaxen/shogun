package events_test

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/events"
	"github.com/0xHoaxen/shogun/services/katana/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

type learnt struct {
	owner uuid.UUID
	item  domain.LearnedItem
}

type fakeLearner struct {
	mu   sync.Mutex
	runs []learnt
}

func (f *fakeLearner) LearnedItem(_ context.Context, _ pgx.Tx, owner uuid.UUID, item domain.LearnedItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, learnt{owner, item})
	return nil
}

func newSink(t *testing.T) (*bus.SinkServer, *fakeLearner) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "katana")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	learner := &fakeLearner{}
	log := slog.New(slog.DiscardHandler)
	sink, err := bus.NewSinkServer(pool, events.Handlers(learner, log), log)
	if err != nil {
		t.Fatalf("sink: %v", err)
	}
	return sink, learner
}

func deliver(t *testing.T, sink *bus.SinkServer, id string, payload proto.Message) error {
	t.Helper()
	packed, err := anypb.New(payload)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	_, err = sink.Deliver(context.Background(), &eventsv1.DeliverRequest{Envelope: &eventsv1.Envelope{
		Id: id, Type: events.TypeItemCompleted, Source: "dojo", Payload: packed,
	}})
	return err
}

func TestItemCompletedAsksForARunAboutTheItem(t *testing.T) {
	sink, learner := newSink(t)
	owner, item := uuid.New(), uuid.NewString()

	err := deliver(t, sink, uuid.NewString(), &dojov1.LearningItemCompleted{
		OwnerId: owner.String(), ItemId: item, Title: "Go course", Kind: dojov1.ItemKind_ITEM_KIND_COURSE, Url: "https://example.com/go",
	})

	want := domain.LearnedItem{ItemID: item, Title: "Go course", Kind: "course", URL: "https://example.com/go"}
	if err != nil || len(learner.runs) != 1 || learner.runs[0].owner != owner || learner.runs[0].item != want {
		t.Fatalf("err %v, runs %+v, want %+v", err, learner.runs, want)
	}
}

func TestADuplicateDeliveryAsksForOneRun(t *testing.T) {
	sink, learner := newSink(t)
	payload := &dojov1.LearningItemCompleted{OwnerId: uuid.NewString(), ItemId: uuid.NewString(), Title: "x"}
	id := uuid.NewString()

	first := deliver(t, sink, id, payload)
	again := deliver(t, sink, id, payload)

	if first != nil || again != nil || len(learner.runs) != 1 {
		t.Fatalf("errs %v %v, runs %d; want one", first, again, len(learner.runs))
	}
}

func TestEventsThatCannotBeHandledAreAcknowledgedWithoutARun(t *testing.T) {
	tests := []struct {
		name    string
		payload proto.Message
	}{
		{"no owner", &dojov1.LearningItemCompleted{ItemId: uuid.NewString()}},
		{"bad owner", &dojov1.LearningItemCompleted{OwnerId: "nope", ItemId: uuid.NewString()}},
		{"bad item id", &dojov1.LearningItemCompleted{OwnerId: uuid.NewString(), ItemId: "nope"}},
		{"payload of another type", wrapperspb.String("x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink, learner := newSink(t)

			err := deliver(t, sink, uuid.NewString(), tt.payload)

			if err != nil || len(learner.runs) != 0 {
				t.Fatalf("err %v, runs %d; want a quiet acknowledgement", err, len(learner.runs))
			}
		})
	}
}
