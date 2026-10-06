package app_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

// axis returns a unit vector along dimension i, so two axes are orthogonal.
func axis(i int) []float32 {
	v := make([]float32, app.EmbeddingDimensions)
	v[i] = 1
	return v
}

// fakeEmbedder returns a vector chosen by the first matching word in the text.
type fakeEmbedder struct {
	byWord map[string][]float32
	vec    []float32
	err    error
	texts  []string
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.texts = append(f.texts, text)
	for word, v := range f.byWord {
		if strings.Contains(text, word) {
			return v, f.err
		}
	}
	return f.vec, f.err
}

func (e *env) sample(t *testing.T, text string, embedding []float32) uuid.UUID {
	t.Helper()
	repo := store.New(e.pool)
	row, err := repo.InsertVoiceSample(context.Background(), db.InsertVoiceSampleParams{
		ID: store.NewID(), OwnerID: e.owner, Channel: "email", Text: text,
	})
	if err != nil {
		t.Fatalf("insert sample: %v", err)
	}
	if embedding != nil {
		if err := repo.SetVoiceSampleEmbedding(context.Background(), row.ID, embedding); err != nil {
			t.Fatalf("embed sample: %v", err)
		}
	}
	return row.ID
}

func (e *env) embedded(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM voice_samples WHERE id = $1 AND embedding IS NOT NULL`, id) == 1
}

func TestEmbedSampleStoresTheVector(t *testing.T) {
	e := newEnv(t, fakeSource{})
	id := e.sample(t, "hello", nil)
	embedder := &fakeEmbedder{vec: axis(3)}

	err := app.NewVoiceEmbedder(e.pool, embedder).EmbedSample(context.Background(), id)

	if err != nil || !e.embedded(t, id) || embedder.texts[0] != "hello" {
		t.Fatalf("err %v, embedded %v, texts %v", err, e.embedded(t, id), embedder.texts)
	}
}

func TestEmbedSampleRefusesBadResults(t *testing.T) {
	boom := errors.New("provider down")
	tests := []struct {
		name     string
		embedder *fakeEmbedder
		gone     bool
		want     error
	}{
		{"embedder fails", &fakeEmbedder{err: boom}, false, boom},
		{"wrong width", &fakeEmbedder{vec: []float32{1, 2, 3}}, false, app.ErrEmbeddingSize},
		{"sample is gone", &fakeEmbedder{vec: axis(0)}, true, store.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, fakeSource{})
			id := e.sample(t, "hello", nil)
			if tt.gone {
				id = uuid.New()
			}

			err := app.NewVoiceEmbedder(e.pool, tt.embedder).EmbedSample(context.Background(), id)

			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			if !tt.gone && e.embedded(t, id) {
				t.Fatal("a failed embed must leave the sample unembedded")
			}
		})
	}
}

func newEmbeddingGenerator(e *env, embedder app.Embedder, source fakeSource) *app.Generator {
	return app.NewGenerator(e.pool, e.llm, source, discardLog(), func() time.Time { return e.now }, app.WithEmbedder(embedder))
}

func TestGenerateRanksVoiceSamplesByCloseness(t *testing.T) {
	e := newEnv(t, fakeSource{target: app.TargetContext{Summary: "about databases"}})
	e.sample(t, "SAMPLE-FOOD", axis(1))
	e.sample(t, "SAMPLE-DATABASES", axis(2))
	e.sample(t, "SAMPLE-UNEMBEDDED", nil)
	embedder := &fakeEmbedder{byWord: map[string][]float32{"databases": axis(2)}, vec: axis(9)}
	d := e.newDraft(t, "cover_letter", "email")

	err := newEmbeddingGenerator(e, embedder, fakeSource{target: app.TargetContext{Summary: "about databases"}}).Generate(context.Background(), e.args(d, 1))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	system := e.llm.req.System
	closest, other, unembedded := strings.Index(system, "SAMPLE-DATABASES"), strings.Index(system, "SAMPLE-FOOD"), strings.Index(system, "SAMPLE-UNEMBEDDED")
	if closest < 0 || closest >= other || other >= unembedded {
		t.Fatalf("want closest, then farther, then unembedded; positions %d %d %d in:\n%s", closest, other, unembedded, system)
	}
}

func TestGenerateFallsBackToNewestSamplesWhenEmbeddingFails(t *testing.T) {
	tests := []struct {
		name     string
		embedder *fakeEmbedder
	}{
		{"embedder error", &fakeEmbedder{err: errors.New("down")}},
		{"wrong width", &fakeEmbedder{vec: []float32{1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, fakeSource{target: app.TargetContext{Summary: "topic"}})
			e.sample(t, "SAMPLE-OLD", axis(1))
			e.sample(t, "SAMPLE-NEW", nil)
			d := e.newDraft(t, "cover_letter", "email")

			err := newEmbeddingGenerator(e, tt.embedder, fakeSource{target: app.TargetContext{Summary: "topic"}}).Generate(context.Background(), e.args(d, 1))

			system := e.llm.req.System
			if err != nil || !strings.Contains(system, "SAMPLE-NEW") || strings.Index(system, "SAMPLE-NEW") > strings.Index(system, "SAMPLE-OLD") {
				t.Fatalf("err %v; want the newest sample first in:\n%s", err, system)
			}
		})
	}
}

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }
