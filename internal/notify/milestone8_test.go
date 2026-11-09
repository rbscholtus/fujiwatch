package notify

// Spec tests for README milestone 8: target-price alerts and the
// weekly summary.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/diff"
	"github.com/rbscholtus/fujiwatch/internal/runner"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

func TestWhatsAppBelowTarget(t *testing.T) {
	msg := &Message{
		Names: map[string]string{"x-t3": "Fujifilm X-T3"},
		Events: []diff.Event{
			// Same listing: new_listing and below_target → one line, the 🎯.
			{Type: diff.NewListing, Shop: "bncamera", URL: "u1", Product: "x-t3", Condition: "used", PriceVND: 14_000_000},
			{Type: diff.BelowTarget, Shop: "bncamera", URL: "u1", Product: "x-t3", Condition: "used", PriceVND: 14_000_000, TargetVND: 15_000_000},
			// Another listing: price_drop then below_target → the 🎯 wins.
			{Type: diff.PriceDrop, Shop: "kyma", URL: "u2", Product: "x-t3", Condition: "used", OldPrice: 16_000_000, PriceVND: 15_000_000},
			{Type: diff.BelowTarget, Shop: "kyma", URL: "u2", Product: "x-t3", Condition: "used", PriceVND: 15_000_000, TargetVND: 15_000_000},
			// Unrelated listing: kept.
			{Type: diff.NewListing, Shop: "zshop", URL: "u3", Product: "x-t3", Condition: "used", PriceVND: 18_000_000},
		},
	}
	text := AlertText(msg, maxWhatsAppChars)
	if !strings.HasPrefix(text, "fujiwatch: 3 alerts") {
		t.Errorf("text = %q", text)
	}
	for _, want := range []string{
		"🎯 Fujifilm X-T3 (used) bncamera: 14.000.000₫, target 15.000.000₫",
		"🎯 Fujifilm X-T3 (used) kyma: 15.000.000₫, target 15.000.000₫",
		"🆕 Fujifilm X-T3 (used) zshop",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "📉") || strings.Count(text, "bncamera") != 1 {
		t.Errorf("duplicate lines for one listing:\n%s", text)
	}
	if s := Subject(msg.Events); !strings.HasPrefix(s, "fujiwatch: 2 below target, 1 price drop") {
		t.Errorf("subject = %q", s)
	}
}

func histRec(at time.Time, shop, cond string, p int64) state.Record {
	return state.Record{TS: at, Shop: shop, URL: "https://" + shop + "/x", PriceVND: p, Condition: cond}
}

// Weekly summary: lowest price over the last 7 Vietnam days vs the 7 before.
func TestWeekly(t *testing.T) {
	now := time.Date(2026, 11, 7, 23, 17, 0, 0, time.UTC) // Sunday Nov 8, 06:17 ICT
	day := func(n int) time.Time { return now.AddDate(0, 0, -n) }
	hist := filepath.Join(t.TempDir(), "history")
	if err := state.AppendHistory(hist, map[string][]state.Record{
		"x-t5": {
			histRec(day(0), "kyma", "new", 37_900_000),
			histRec(day(3), "vjshop", "new", 36_990_000), // this week's low
			histRec(day(6), "kyma", "new", 38_500_000),   // still this week (Nov 2 ICT)
			histRec(day(7), "vjshop", "new", 35_000_000), // last week (Nov 1 ICT)
			histRec(day(10), "kyma", "new", 38_000_000),
			histRec(day(14), "kyma", "new", 1_000_000), // older: ignored
			histRec(day(2), "zshop", "new", 0),         // hidden: ignored
			histRec(day(1), "mayanh24h", "used", 32_990_000),
		},
	}); err != nil {
		t.Fatal(err)
	}
	products := []config.Product{{ID: "x-t5", Name: "Fujifilm X-T5"}, {ID: "x-t3", Name: "Fujifilm X-T3"}}

	rows, err := Weekly(products, hist, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want x-t5 new and used (x-t3 has no history)", rows)
	}
	if r := rows[0]; r.Condition != "new" || r.This.PriceVND != 36_990_000 || r.This.Shop != "vjshop" || r.Last.PriceVND != 35_000_000 {
		t.Errorf("new row = %+v", r)
	}
	if r := rows[1]; r.Condition != "used" || r.This.PriceVND != 32_990_000 || r.Last.PriceVND != 0 {
		t.Errorf("used row = %+v", r)
	}

	// Rendered in the email with the change.
	raw, err := (&Email{From: "f@x", To: []string{"t@x"}}).Render(&Message{Subject: "s", Time: now, Weekly: rows})
	if err != nil {
		t.Fatal(err)
	}
	_, _, text, html := parseEmail(t, raw)
	for _, want := range []string{"THIS WEEK", "Fujifilm X-T5 (new): 36.990.000₫ at vjshop | last week 35.000.000₫ (+5.7%)", "Fujifilm X-T5 (used): 32.990.000₫ at mayanh24h | last week –"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(html, "<h3>This week</h3>") {
		t.Error("html missing weekly table")
	}
}

func TestWeeklyDayAndMessage(t *testing.T) {
	sundayICT := time.Date(2026, 11, 7, 23, 17, 0, 0, time.UTC)
	n := config.Notify{Email: config.EmailChanges, Weekly: "sunday"}
	if !IsWeeklyDay(n, sundayICT) || IsWeeklyDay(n, sundayICT.Add(-24*time.Hour)) || IsWeeklyDay(config.Notify{}, sundayICT) {
		t.Error("IsWeeklyDay wrong")
	}
	// With email: changes, the weekly day still sends.
	if !ShouldEmail(n, nil, sundayICT) || ShouldEmail(n, nil, sundayICT.Add(-24*time.Hour)) {
		t.Error("ShouldEmail on weekly day wrong")
	}

	cfg := &config.Config{
		Watchlist: config.Watchlist{Products: []config.Product{{ID: "x-t5", Name: "Fujifilm X-T5"}}},
		Sources:   config.Sources{Notify: n},
	}
	msg, err := BuildMessage(cfg, &runner.Report{Time: sundayICT}, state.New(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "fujiwatch: no changes · weekly summary" {
		t.Errorf("subject = %q", msg.Subject)
	}
	msg, _ = BuildMessage(cfg, &runner.Report{Time: sundayICT.Add(-24 * time.Hour)}, state.New(), t.TempDir())
	if strings.Contains(msg.Subject, "weekly") || msg.Weekly != nil {
		t.Errorf("non-weekly day message = %+v", msg)
	}
}
