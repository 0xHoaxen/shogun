package domain

import "testing"

func TestClassifySureCases(t *testing.T) {
	tests := []struct {
		name string
		in   Signals
		want Class
	}{
		{"application confirmation", Signals{Subject: "Thank you for applying to Lumen"}, ClassApplicationConfirmation},
		{"confirmation in the snippet", Signals{Subject: "Update", Snippet: "We received your application for Backend Engineer."}, ClassApplicationConfirmation},
		{"rejection", Signals{Subject: "Your application", Snippet: "Unfortunately we will not be moving forward."}, ClassRejection},
		{"offer", Signals{Subject: "We are pleased to offer you the role"}, ClassOffer},
		{"interview invite", Signals{Subject: "Interview with Lumen", Snippet: "Can you share your availability?"}, ClassInterviewInvite},
		{"case does not matter", Signals{Subject: "THANKS FOR APPLYING"}, ClassApplicationConfirmation},
		{"reply from a contact in our thread", Signals{Subject: "Quick question", LinkedContact: true, ReplyToUs: true}, ClassReply},
		{"re: from a contact", Signals{Subject: "Re: coffee?", LinkedContact: true}, ClassReply},
		{"newsletter tied to nothing", Signals{Subject: "Your weekly digest", Snippet: "Ten tips"}, ClassOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.in)

			if got.Class != tt.want || !got.Sure || got.Confidence < sureAt-0.3 {
				t.Fatalf("got %+v, want a sure %s", got, tt.want)
			}
		})
	}
}

func TestClassifyAsksTheModelWhenTheRulesAreNotSure(t *testing.T) {
	tests := []struct {
		name string
		in   Signals
		best Class
	}{
		{"two classes at once", Signals{Subject: "Thanks for applying", Snippet: "Unfortunately we are not moving forward"}, ClassRejection},
		{"a reply that mentions an interview", Signals{Subject: "Re: hello", Snippet: "about the interview", LinkedContact: true}, ClassInterviewInvite},
		{"recruiter outreach is only a weak match", Signals{Subject: "Exciting opportunity at Acme"}, ClassRecruiterOutreach},
		{"nothing matches but the sender is a contact", Signals{Subject: "Hi there", LinkedContact: true}, ClassOther},
		{"nothing matches but a job is linked", Signals{Subject: "Update", LinkedJob: true}, ClassOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.in)

			if got.Sure || got.Class != tt.best {
				t.Fatalf("got %+v, want an unsure guess of %s", got, tt.best)
			}
		})
	}
}

func TestReplyNeedsAKnownContact(t *testing.T) {
	got := Classify(Signals{Subject: "Re: hello", ReplyToUs: true})

	if got.Class == ClassReply {
		t.Fatalf("a stranger's mail was called a reply: %+v", got)
	}
}

func TestClassValid(t *testing.T) {
	for _, c := range Classes {
		if !c.Valid() {
			t.Errorf("%s should be valid", c)
		}
	}
	if Class("spam").Valid() || Class("").Valid() {
		t.Error("unknown classes must be invalid")
	}
}
