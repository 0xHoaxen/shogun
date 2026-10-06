package domain

import (
	"slices"
	"strings"
)

// Class is what a piece of mail is. Values match the messages.classification
// CHECK.
type Class string

// The classes.
const (
	ClassApplicationConfirmation Class = "application_confirmation"
	ClassInterviewInvite         Class = "interview_invite"
	ClassRejection               Class = "rejection"
	ClassOffer                   Class = "offer"
	ClassRecruiterOutreach       Class = "recruiter_outreach"
	ClassReply                   Class = "reply"
	ClassOther                   Class = "other"
)

// Classes lists every class, for validating a model's answer.
var Classes = []Class{
	ClassApplicationConfirmation, ClassInterviewInvite, ClassRejection, ClassOffer,
	ClassRecruiterOutreach, ClassReply, ClassOther,
}

// Valid reports whether c is a known class.
func (c Class) Valid() bool { return slices.Contains(Classes, c) }

// Confidence the rules give what they find, and the least a sure verdict needs.
const (
	ruleConfidenceStrong = 0.95
	ruleConfidenceMedium = 0.9
	ruleConfidenceWeak   = 0.7
	// ruleConfidenceNone is the confidence of "other" when nothing matched and
	// nothing links the mail to a job or contact.
	ruleConfidenceNone = 0.6
	// sureAt is the confidence at which a single rule match is trusted without
	// asking the model.
	sureAt = 0.85
)

// phrases are what each class's rule looks for in the subject and snippet,
// lower-cased.
var phrases = map[Class][]string{
	ClassOffer: {
		"pleased to offer", "excited to offer", "offer letter", "job offer", "we would like to offer you",
		"extend an offer",
	},
	ClassRejection: {
		"unfortunately", "not moving forward", "decided to move forward with other", "will not be proceeding",
		"not be moving forward", "we regret to inform", "not selected", "pursue other candidates",
		"other candidates",
	},
	ClassInterviewInvite: {
		"interview", "phone screen", "schedule a call", "schedule a time", "your availability",
		"book a time", "technical screen", "meet with our team",
	},
	ClassApplicationConfirmation: {
		"thank you for applying", "thanks for applying", "we received your application", "application received",
		"application has been received", "application submitted", "successfully applied",
	},
	ClassRecruiterOutreach: {
		"came across your profile", "are you open to", "i'm a recruiter", "i am a recruiter", "exciting opportunity",
		"opportunity at", "your background",
	},
}

var phraseConfidence = map[Class]float32{
	ClassOffer: ruleConfidenceStrong, ClassRejection: ruleConfidenceStrong,
	ClassInterviewInvite: ruleConfidenceMedium, ClassApplicationConfirmation: ruleConfidenceStrong,
	ClassRecruiterOutreach: ruleConfidenceWeak,
}

// guessOrder breaks ties between equally confident matches: a decisive message
// (an offer, a rejection) outweighs the routine one it usually quotes.
var guessOrder = []Class{
	ClassOffer, ClassRejection, ClassInterviewInvite, ClassApplicationConfirmation, ClassReply, ClassRecruiterOutreach,
}

// Signals is what the rules look at: the visible text and what is already known
// about the mail. Bodies are never read, only the subject and the snippet.
type Signals struct {
	Subject string
	Snippet string
	// LinkedContact is set when the sender is one of the owner's contacts.
	LinkedContact bool
	// LinkedJob is set when the mail is tied to a tracked job.
	LinkedJob bool
	// ReplyToUs is set when the thread holds a message the owner sent.
	ReplyToUs bool
}

// Verdict is the rules' answer.
type Verdict struct {
	Class      Class
	Confidence float32
	// Sure is true when the rules settle it and the model need not be asked.
	Sure bool
}

// Classify runs the rules. It is sure when exactly one class matched with
// enough confidence, or when nothing matched and nothing ties the mail to a job
// or contact (so it is probably not about the job search and not worth a model
// call). Conflicting matches, weak matches, and unmatched mail that is tied to
// a job or contact are not sure.
func Classify(s Signals) Verdict {
	text := strings.ToLower(s.Subject + " " + s.Snippet)
	matched := map[Class]float32{}
	for class, words := range phrases {
		for _, w := range words {
			if strings.Contains(text, w) {
				matched[class] = phraseConfidence[class]
				break
			}
		}
	}
	if s.LinkedContact && (s.ReplyToUs || strings.HasPrefix(strings.TrimSpace(strings.ToLower(s.Subject)), "re:")) {
		matched[ClassReply] = ruleConfidenceMedium
	}

	switch len(matched) {
	case 0:
		if s.LinkedContact || s.LinkedJob {
			return Verdict{Class: ClassOther, Confidence: ruleConfidenceNone, Sure: false}
		}
		return Verdict{Class: ClassOther, Confidence: ruleConfidenceNone, Sure: true}
	case 1:
		for class, confidence := range matched {
			return Verdict{Class: class, Confidence: confidence, Sure: confidence >= sureAt}
		}
	}
	// Several classes matched: take the likeliest as the guess, but ask.
	best := Verdict{Class: ClassOther}
	for _, class := range guessOrder {
		if c, ok := matched[class]; ok && c > best.Confidence {
			best = Verdict{Class: class, Confidence: c}
		}
	}
	return best
}
