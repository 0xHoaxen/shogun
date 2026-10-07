package fetch_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/fetch"
)

const rssDoc = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:content="http://purl.org/rss/1.0/modules/content/">
 <channel>
  <title>Jobs</title>
  <item>
   <title>Backend Engineer</title>
   <link>https://jobs.example.com/1</link>
   <guid>job-1</guid>
   <description>Short.</description>
   <content:encoded>Long text about Go and Postgres.</content:encoded>
   <pubDate>Tue, 06 Oct 2026 09:00:00 +0000</pubDate>
   <dc:creator>Acme</dc:creator>
   <location>Remote</location>
  </item>
  <item>
   <title>No guid</title>
   <link>https://jobs.example.com/2</link>
   <description>Plain.</description>
  </item>
 </channel>
</rss>`

const atomDoc = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
 <entry>
  <title>Platform Engineer</title>
  <id>tag:example.com,2026:3</id>
  <link rel="self" href="https://jobs.example.com/self"/>
  <link rel="alternate" href="https://jobs.example.com/3"/>
  <summary>Kubernetes.</summary>
  <updated>2026-10-05T10:00:00Z</updated>
  <author><name>Globex</name></author>
 </entry>
</feed>`

func TestParseFeedReadsRSS(t *testing.T) {
	items, err := fetch.ParseFeed([]byte(rssDoc))

	if err != nil || len(items) != 2 {
		t.Fatalf("items %d, err %v", len(items), err)
	}
	c := items[0].Candidate
	if c.ExternalID != "job-1" || c.Title != "Backend Engineer" || c.Company != "Acme" || c.URL != "https://jobs.example.com/1" ||
		c.Location != "Remote" || c.Description != "Long text about Go and Postgres." {
		t.Fatalf("candidate = %+v", c)
	}
	if c.PostedAt == nil || !c.PostedAt.Equal(time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("posted at = %v", c.PostedAt)
	}
	if second := items[1].Candidate; second.ExternalID != "https://jobs.example.com/2" || second.PostedAt != nil {
		t.Fatalf("second = %+v, want the link as the id when there is no guid", second)
	}
	if !strings.Contains(string(items[0].Raw), "Backend Engineer") {
		t.Fatalf("raw = %s", items[0].Raw)
	}
}

func TestParseFeedReadsAtom(t *testing.T) {
	items, err := fetch.ParseFeed([]byte(atomDoc))

	if err != nil || len(items) != 1 {
		t.Fatalf("items %d, err %v", len(items), err)
	}
	c := items[0].Candidate
	if c.ExternalID != "tag:example.com,2026:3" || c.Title != "Platform Engineer" || c.Company != "Globex" ||
		c.URL != "https://jobs.example.com/3" || c.Description != "Kubernetes." || c.PostedAt == nil {
		t.Fatalf("candidate = %+v", c)
	}
}

func TestParseFeedRefusesWhatIsNotAFeedAndAcceptsAnEmptyOne(t *testing.T) {
	_, notXML := fetch.ParseFeed([]byte(`{"json":true}`))
	_, html := fetch.ParseFeed([]byte(`<html><body>hi</body></html>`))
	empty, emptyErr := fetch.ParseFeed([]byte(`<rss version="2.0"><channel></channel></rss>`))

	if !errors.Is(notXML, fetch.ErrBadDocument) || !errors.Is(html, fetch.ErrBadDocument) {
		t.Fatalf("notXML %v, html %v", notXML, html)
	}
	if emptyErr != nil || len(empty) != 0 {
		t.Fatalf("empty feed = %v, %v", empty, emptyErr)
	}
}

func TestParseFeedCapsTheItemCount(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<rss><channel>`)
	for range fetch.MaxItems + 20 {
		b.WriteString(`<item><title>t</title><guid>g</guid></item>`)
	}
	b.WriteString(`</channel></rss>`)

	items, err := fetch.ParseFeed([]byte(b.String()))

	if err != nil || len(items) != fetch.MaxItems {
		t.Fatalf("items %d, err %v", len(items), err)
	}
}

func TestParseJSONThroughAMapping(t *testing.T) {
	doc := `{"data":{"jobs":[
	  {"id":101,"title":"Backend Engineer","company":{"name":"Acme"},"link":"https://acme.example/101","where":"Remote","posted":"2026-10-06","text":"Go and Postgres"},
	  {"id":"b","title":"Second"},
	  "not an object",
	  {"id":102}
	]}}`
	m := domain.FieldMapping{
		ItemsPath: "data.jobs", ID: "id", Title: "title", Company: "company.name", URL: "link",
		Location: "where", PostedAt: "posted", Description: "text",
	}

	items, err := fetch.ParseJSON([]byte(doc), m)

	if err != nil || len(items) != 3 {
		t.Fatalf("items %d, err %v; want the object entries, even one that will fail validation later", len(items), err)
	}
	c := items[0].Candidate
	if c.ExternalID != "101" || c.Title != "Backend Engineer" || c.Company != "Acme" || c.URL != "https://acme.example/101" ||
		c.Location != "Remote" || c.Description != "Go and Postgres" || c.PostedAt == nil || c.PostedAt.Day() != 6 {
		t.Fatalf("candidate = %+v", c)
	}
	if _, err := items[2].Candidate.Validate(); err == nil {
		t.Fatal("an item with no title should fail validation")
	}
}

func TestParseJSONWithTheDocumentAsTheArray(t *testing.T) {
	items, err := fetch.ParseJSON([]byte(`[{"id":"1","title":"A"},{"id":"2","title":"B"}]`), domain.FieldMapping{ID: "id", Title: "title"})

	if err != nil || len(items) != 2 || items[1].Candidate.Title != "B" {
		t.Fatalf("items %+v, err %v", items, err)
	}
}

func TestParseJSONRefusesWhatIsNotTheMappedShape(t *testing.T) {
	m := domain.FieldMapping{ItemsPath: "jobs", ID: "id", Title: "title"}
	for _, doc := range []string{`not json`, `{"jobs":"nope"}`, `{"other":[]}`, `[]`} {
		if _, err := fetch.ParseJSON([]byte(doc), m); !errors.Is(err, fetch.ErrBadDocument) {
			t.Errorf("%q: err = %v, want ErrBadDocument", doc, err)
		}
	}
}
