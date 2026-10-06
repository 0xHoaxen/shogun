package app

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
)

// maxLinkURLs bounds how many links of one mail are looked at.
const maxLinkURLs = 20

// freeMailDomains are shared providers: mail from them says nothing about which
// company it is from, so they never match a company's domain.
var freeMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true,
	"yahoo.com": true, "icloud.com": true, "me.com": true, "proton.me": true, "protonmail.com": true,
	"aol.com": true, "gmx.com": true, "mail.com": true,
}

// MailLinks is what a piece of mail is about, as ids; empty when unknown.
type MailLinks struct {
	ContactID string
	JobID     string
}

// FindMailLinks says which contact and job a piece of mail is about: the
// contact by the sender's address, and the job by a posting URL in the mail,
// else by the sender's company domain, else the contact's own job.
func (s *Service) FindMailLinks(ctx context.Context, fromEmail string, urls []string) (MailLinks, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return MailLinks{}, err
	}
	addr := senderAddress(fromEmail)
	repo := store.New(s.pool)
	var links MailLinks

	var contactJob *uuid.UUID
	if addr != "" {
		c, err := repo.FindDuplicate(ctx, owner, &addr, nil)
		switch {
		case err == nil:
			links.ContactID = c.ID.String()
			contactJob = c.JobID
		case !errors.Is(err, store.ErrNotFound):
			return MailLinks{}, err
		}
	}
	if len(urls) > maxLinkURLs {
		urls = urls[:maxLinkURLs]
	}
	if len(urls) > 0 {
		job, err := repo.FindJobByURLs(ctx, owner, urls)
		if err == nil {
			links.JobID = job.ID.String()
			return links, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return MailLinks{}, err
		}
	}
	if domains := companyDomains(addr); len(domains) > 0 {
		job, err := repo.FindJobByCompanyDomains(ctx, owner, domains)
		if err == nil {
			links.JobID = job.ID.String()
			return links, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return MailLinks{}, err
		}
	}
	if contactJob != nil {
		links.JobID = contactJob.String()
	}
	return links, nil
}

// senderAddress returns the bare, lower-cased address of a From value.
func senderAddress(from string) string {
	from = strings.TrimSpace(from)
	if parsed, err := mail.ParseAddress(from); err == nil {
		return strings.ToLower(parsed.Address)
	}
	if strings.Contains(from, "@") && !strings.ContainsAny(from, " <>") {
		return strings.ToLower(from)
	}
	return ""
}

// companyDomains returns the sender's domain and each parent domain down to two
// labels (hr.eu.lumen.example, eu.lumen.example, lumen.example), so mail from a
// subdomain still matches the company's own domain. Free-mail providers give
// none.
func companyDomains(addr string) []string {
	_, domain, ok := strings.Cut(addr, "@")
	if !ok || domain == "" || freeMailDomains[domain] {
		return nil
	}
	labels := strings.Split(domain, ".")
	var out []string
	for i := 0; i+2 <= len(labels); i++ {
		candidate := strings.Join(labels[i:], ".")
		if !freeMailDomains[candidate] {
			out = append(out, candidate)
		}
	}
	return out
}
