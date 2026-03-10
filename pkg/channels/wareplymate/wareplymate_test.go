package wareplymate

import "testing"

func TestNormalizeJID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "phone jid", in: "918088997070@s.whatsapp.net", want: "918088997070"},
		{name: "lid jid", in: "118971063918781@lid", want: "118971063918781"},
		{name: "plus and separators", in: "+91-80889 97070@s.whatsapp.net", want: "918088997070"},
		{name: "device suffix", in: "918088997070:12@s.whatsapp.net", want: "918088997070"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeJID(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeJID(%q)=%q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClassifySenderRole(t *testing.T) {
	t.Parallel()

	c := &WAReplyMateChannel{ownerIDs: map[string]struct{}{"918088997070": {}}}

	tests := []struct {
		name       string
		msg        dbMessage
		wantRole   string
		wantReason string
	}{
		{
			name: "owner by sender jid",
			msg: dbMessage{
				senderID: "918088997070@s.whatsapp.net",
				chatID:   "919999999999@s.whatsapp.net",
			},
			wantRole:   "owner",
			wantReason: "sender_jid_match",
		},
		{
			name: "owner by self message",
			msg: dbMessage{
				senderID: "919999999999@s.whatsapp.net",
				chatID:   "919999999999@s.whatsapp.net",
				isFromMe: 1,
			},
			wantRole:   "owner",
			wantReason: "self_message",
		},
		{
			name: "non owner even when chat id matches owner",
			msg: dbMessage{
				senderID: "919999999999@s.whatsapp.net",
				chatID:   "918088997070@s.whatsapp.net",
			},
			wantRole:   "non_owner",
			wantReason: "no_owner_match",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotRole, gotReason := c.classifySenderRole(tc.msg)
			if gotRole != tc.wantRole || gotReason != tc.wantReason {
				t.Fatalf("classifySenderRole()=(%q,%q), want (%q,%q)", gotRole, gotReason, tc.wantRole, tc.wantReason)
			}
		})
	}
}
