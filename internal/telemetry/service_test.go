package telemetry

import "testing"

func TestSelectSpansCanonicalizesAndRejectsInvalidIdentifiers(t *testing.T) {
	spans := []Span{{SpanID: "940043a3733d8025"}, {SpanID: "0123456789abcdef"}}

	selected := selectSpans(spans, []string{"940043A3733D8025", " 940043a3733d8025 "})
	if len(selected) != 1 || selected[0].SpanID != "940043a3733d8025" {
		t.Fatalf("canonical selection = %#v", selected)
	}
	if selected := selectSpans(spans, []string{"not-a-span-id"}); len(selected) != 0 {
		t.Fatalf("invalid span selection = %#v", selected)
	}
}
