package fetch

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
)

type rssItem struct {
	Title       string `xml:"title" json:"title"`
	Link        string `xml:"link" json:"link"`
	GUID        string `xml:"guid" json:"guid"`
	Description string `xml:"description" json:"description"`
	Content     string `xml:"encoded" json:"content,omitempty"`
	PubDate     string `xml:"pubDate" json:"pub_date"`
	Author      string `xml:"author" json:"author,omitempty"`
	Creator     string `xml:"creator" json:"creator,omitempty"`
	Location    string `xml:"location" json:"location,omitempty"`
}

type atomEntry struct {
	Title     string `xml:"title" json:"title"`
	ID        string `xml:"id" json:"id"`
	Summary   string `xml:"summary" json:"summary"`
	Content   string `xml:"content" json:"content,omitempty"`
	Published string `xml:"published" json:"published"`
	Updated   string `xml:"updated" json:"updated"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author" json:"-"`
	Links []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link" json:"-"`
}

// ParseFeed reads postings from an RSS 2.0 or Atom document.
func ParseFeed(doc []byte) ([]Item, error) {
	dec := xml.NewDecoder(bytes.NewReader(doc))
	dec.Strict = false
	var items []Item
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: not XML", ErrBadDocument)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "item":
			var it rssItem
			if err := dec.DecodeElement(&it, &start); err != nil {
				continue
			}
			items = append(items, rssCandidate(it))
		case "entry":
			var e atomEntry
			if err := dec.DecodeElement(&e, &start); err != nil {
				continue
			}
			items = append(items, atomCandidate(e))
		}
		if len(items) >= MaxItems {
			break
		}
	}
	if len(items) == 0 && !bytes.Contains(doc, []byte("<rss")) && !bytes.Contains(doc, []byte("<feed")) {
		return nil, fmt.Errorf("%w: neither an RSS nor an Atom feed", ErrBadDocument)
	}
	return items, nil
}

func rssCandidate(it rssItem) Item {
	id := strings.TrimSpace(it.GUID)
	if id == "" {
		id = strings.TrimSpace(it.Link)
	}
	company := strings.TrimSpace(it.Creator)
	if company == "" {
		company = strings.TrimSpace(it.Author)
	}
	body := it.Description
	if strings.TrimSpace(it.Content) != "" {
		body = it.Content
	}
	return Item{
		Raw: rawOf(it),
		Candidate: domain.Candidate{
			ExternalID: id, Title: it.Title, Company: company, URL: it.Link, Location: it.Location,
			PostedAt: parseTime(it.PubDate), Description: body,
		},
	}
}

func atomCandidate(e atomEntry) Item {
	link := ""
	for _, l := range e.Links {
		if l.Rel == "" || l.Rel == "alternate" {
			link = l.Href
			break
		}
	}
	id := strings.TrimSpace(e.ID)
	if id == "" {
		id = link
	}
	when := e.Published
	if strings.TrimSpace(when) == "" {
		when = e.Updated
	}
	body := e.Summary
	if strings.TrimSpace(e.Content) != "" {
		body = e.Content
	}
	return Item{
		Raw: rawOf(e),
		Candidate: domain.Candidate{
			ExternalID: id, Title: e.Title, Company: e.Author.Name, URL: link, PostedAt: parseTime(when), Description: body,
		},
	}
}
