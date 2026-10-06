package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/domain"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

// Event types and sources written by classification.
const (
	eventSource        = "tsubame"
	eventMailClassed   = "mail.classified"
	eventReplyDetected = "mail.reply_detected"

	classifiedByRule = "rule"
	classifiedByLLM  = "llm"
)

var urlPattern = regexp.MustCompile(`https?://[^\s<>"')]+`)

// Links is the kagami contact and job a message was tied to; either may be nil.
type Links struct {
	ContactID *uuid.UUID
	JobID     *uuid.UUID
}

// Linker finds what a message is about from the sender and the links in it.
type Linker interface {
	FindLinks(ctx context.Context, from string, urls []string) (Links, error)
}

// Classifier decides what each synced message is.
type Classifier struct {
	pool   *pgxpool.Pool
	llm    Completer
	linker Linker
}

// NewClassifier returns a Classifier.
func NewClassifier(pool *pgxpool.Pool, completer Completer, linker Linker) *Classifier {
	return &Classifier{pool: pool, llm: completer, linker: linker}
}

// Classify classifies one inbound message and emits mail.classified, and
// mail.reply_detected when a contact wrote back. Rules go first; the model is
// asked only when they are unsure. A message already classified, or one the
// owner sent, is left alone. A *llm.BudgetError is returned as is, so the job
// can wait for the budget to reset.
func (c *Classifier) Classify(ctx context.Context, owner, messageID uuid.UUID) error {
	repo := store.New(c.pool)
	msg, err := repo.GetMessage(ctx, owner, messageID)
	if err != nil {
		return err
	}
	if msg.Classification != nil || msg.Direction != string(mail.Inbound) {
		return nil
	}

	links, err := c.link(ctx, repo, msg)
	if err != nil {
		return err
	}
	replyToUs := false
	if msg.ProviderThreadID != nil {
		if replyToUs, err = repo.ThreadHasOutbound(ctx, msg.AccountID, *msg.ProviderThreadID); err != nil {
			return err
		}
	}
	verdict := domain.Classify(domain.Signals{
		Subject: deref(msg.Subject), Snippet: deref(msg.Snippet),
		LinkedContact: links.ContactID != nil, LinkedJob: links.JobID != nil, ReplyToUs: replyToUs,
	})
	by := classifiedByRule
	if !verdict.Sure {
		class, confidence, err := c.ask(ctx, msg, links)
		if err != nil {
			return err
		}
		verdict = domain.Verdict{Class: class, Confidence: confidence, Sure: true}
		by = classifiedByLLM
	}
	return c.save(ctx, msg, verdict, by, links)
}

// link ties the message to a job and contact: first through its thread, then,
// for whatever is still missing, by asking kagami.
func (c *Classifier) link(ctx context.Context, repo *store.Repo, msg db.Message) (Links, error) {
	var links Links
	if msg.ProviderThreadID != nil {
		job, contact, err := repo.ThreadLinks(ctx, msg.AccountID, msg.ID, *msg.ProviderThreadID)
		if err != nil {
			return Links{}, err
		}
		links = Links{JobID: job, ContactID: contact}
	}
	if links.JobID != nil && links.ContactID != nil {
		return links, nil
	}
	found, err := c.linker.FindLinks(ctx, msg.FromAddr, urlsIn(deref(msg.Subject)+" "+deref(msg.Snippet)))
	if err != nil {
		return Links{}, fmt.Errorf("find links: %w", err)
	}
	if links.JobID == nil {
		links.JobID = found.JobID
	}
	if links.ContactID == nil {
		links.ContactID = found.ContactID
	}
	return links, nil
}

func (c *Classifier) ask(ctx context.Context, msg db.Message, links Links) (domain.Class, float32, error) {
	resp, err := c.llm.Complete(ctx, classifyFeature, classifyRequest(msg, links.ContactID != nil, links.JobID != nil))
	if err != nil {
		return "", 0, fmt.Errorf("classify with model: %w", err)
	}
	return parseClassification(resp.Text)
}

// save stores the classification and its events in one transaction.
func (c *Classifier) save(ctx context.Context, msg db.Message, v domain.Verdict, by string, links Links) error {
	return postgres.InTx(ctx, c.pool, func(tx pgx.Tx) error {
		changed, err := store.New(tx).SetMessageClassification(ctx, msg.OwnerID, msg.ID, store.ClassifyArgs{
			Classification: string(v.Class), Confidence: v.Confidence, ClassifiedBy: by,
			JobID: links.JobID, ContactID: links.ContactID,
		})
		if err != nil || !changed {
			return err // a concurrent run classified it first
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventMailClassed, msg.ID.String(), &tsubamev1.MailClassified{
			OwnerId: msg.OwnerID.String(), MessageId: msg.ID.String(), Classification: classToProto(v.Class),
			Confidence: v.Confidence, JobId: idString(links.JobID), ContactId: idString(links.ContactID), ClassifiedBy: by,
		})
		if err != nil {
			return fmt.Errorf("write %s event: %w", eventMailClassed, err)
		}
		if v.Class != domain.ClassReply || links.ContactID == nil {
			return nil
		}
		if _, err := outbox.Write(ctx, tx, eventSource, eventReplyDetected, msg.ID.String(), &tsubamev1.MailReplyDetected{
			OwnerId: msg.OwnerID.String(), MessageId: msg.ID.String(), ContactId: links.ContactID.String(),
		}); err != nil {
			return fmt.Errorf("write %s event: %w", eventReplyDetected, err)
		}
		return nil
	})
}

func classToProto(c domain.Class) tsubamev1.MailClass {
	return tsubamev1.MailClass(tsubamev1.MailClass_value["MAIL_CLASS_"+strings.ToUpper(string(c))])
}

func idString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// urlsIn returns the links in text, without trailing punctuation.
func urlsIn(text string) []string {
	var out []string
	for _, u := range urlPattern.FindAllString(text, -1) {
		out = append(out, strings.TrimRight(u, ".,;:!?"))
	}
	return out
}
