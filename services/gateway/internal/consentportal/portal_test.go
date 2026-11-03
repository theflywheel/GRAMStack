package consentportal

import (
	"net/http/httptest"
	"strings"
	"testing"

	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

func TestNoticeEscapesMarkupAndKeepsExplicitChoices(t *testing.T) {
	notice := `<script>alert("not code")</script> & exact recipient terms`
	w := httptest.NewRecorder()
	(&Service{}).render(w, view{Title: "Review this data request", RequestID: "request", Review: true, Record: cm.Record{Notice: cm.Notice{Text: notice}}})
	body := w.Body.String()
	if strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "&amp; exact recipient terms") {
		t.Fatal("notice text became executable markup or was lost")
	}
	if strings.Count(body, `name="decision"`) != 2 || !strings.Contains(body, `name="confirmation" value="yes" required`) || strings.Contains(body, " checked") {
		t.Fatal("explicit all-or-nothing choices changed")
	}
	if !strings.Contains(body, `<html lang="en">`) || !strings.Contains(body, "Other languages are not supported") {
		t.Fatal("supported notice language is unclear")
	}
}
