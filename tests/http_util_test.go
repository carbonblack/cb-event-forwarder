package tests

import (
	"github.com/carbonblack/cb-event-forwarder/pkg/outputs"
	"github.com/google/go-cmp/cmp"
	"testing"
)

func TestNewUploadEvent(t *testing.T) {
	for _, test := range []struct {
		desc                     string
		eventSeq                 int64
		eventText                string
		eventTextAsJsonByteArray bool
		expectedUploadEvent      outputs.UploadEvent
	}{
		{
			desc:                     "eventTextAsJsonByteArray false, empty eventText",
			eventSeq:                 1,
			eventText:                "",
			eventTextAsJsonByteArray: false,
			expectedUploadEvent: outputs.UploadEvent{
				EventSeq:  1,
				EventText: "",
			},
		},
		{
			desc:                     "eventTextAsJsonByteArray false, non-empty eventText",
			eventSeq:                 1,
			eventText:                "some event",
			eventTextAsJsonByteArray: false,
			expectedUploadEvent: outputs.UploadEvent{
				EventSeq:  1,
				EventText: "some event",
			},
		},
		{
			desc:                     "eventTextAsJsonByteArray true, empty eventText",
			eventSeq:                 1,
			eventText:                "",
			eventTextAsJsonByteArray: true,
			expectedUploadEvent: outputs.UploadEvent{
				EventSeq:  1,
				EventText: "",
			},
		},
		{
			desc:                     "eventTextAsJsonByteArray true, non-empty eventText",
			eventSeq:                 1,
			eventText:                "some event",
			eventTextAsJsonByteArray: true,
			expectedUploadEvent: outputs.UploadEvent{
				EventSeq:  1,
				EventText: "c29tZSBldmVudA==",
			},
		},
	} {
		test := test // capture range variable.
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			gotUploadEvent := outputs.NewUploadEvent(test.eventSeq, test.eventText, test.eventTextAsJsonByteArray)
			if diff := cmp.Diff(gotUploadEvent, test.expectedUploadEvent); diff != "" {
				t.Errorf("uploadEvent different from expected, diff: %s", diff)
			}
		})
	}
}
