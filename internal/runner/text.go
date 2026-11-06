package runner

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/rbscholtus/fujiwatch/internal/diff"
	"github.com/rbscholtus/fujiwatch/internal/price"
)

// EventLabel is a short human label for an event type.
func EventLabel(t diff.Type) string {
	switch t {
	case diff.PriceDrop:
		return "PRICE DROP"
	case diff.PriceRise:
		return "price rise"
	case diff.BackInStock:
		return "BACK IN STOCK"
	case diff.OutOfStock:
		return "out of stock"
	case diff.NewListing:
		return "NEW LISTING"
	case diff.Gone:
		return "gone"
	case diff.AdapterBroken:
		return "BROKEN"
	case diff.BelowTarget:
		return "BELOW TARGET"
	default:
		return string(t)
	}
}

// EventPrice describes the price part of an event, e.g.
// "27.490.000₫ → 25.990.000₫ (-5.5%)".
func EventPrice(e diff.Event) string {
	switch e.Type {
	case diff.PriceDrop, diff.PriceRise:
		pct := float64(e.PriceVND-e.OldPrice) * 100 / float64(e.OldPrice)
		return fmt.Sprintf("%s → %s (%+.1f%%)", price.FormatVND(e.OldPrice), price.FormatVND(e.PriceVND), pct)
	case diff.AdapterBroken:
		return ""
	case diff.BelowTarget:
		return fmt.Sprintf("%s (target %s)", price.FormatVND(e.PriceVND), price.FormatVND(e.TargetVND))
	case diff.BackInStock, diff.OutOfStock, diff.NewListing, diff.Gone:
		return price.FormatVND(e.PriceVND)
	default:
		return price.FormatVND(e.PriceVND)
	}
}

// WriteText writes a plain-text report, used for --dry-run and logs.
func WriteText(w io.Writer, rep *Report, dryRun bool) error {
	var b strings.Builder
	mode := ""
	if dryRun {
		mode = " (dry run: state, history and notifications untouched)"
	}
	fmt.Fprintf(&b, "fujiwatch run %s%s\n", rep.Time.Format("2006-01-02 15:04:05Z"), mode)

	fmt.Fprintf(&b, "\nEvents (%d)\n", len(rep.Events))
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, e := range rep.Events {
		if e.Type == diff.AdapterBroken {
			fmt.Fprintf(tw, "  %s\t%s\t\t\t%s\t%s\n", EventLabel(e.Type), e.Shop, e.Detail, e.URL)
			continue
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s  %s\n", EventLabel(e.Type), e.Shop, e.Product, e.Condition, EventPrice(e), e.Title, e.URL)
	}
	_ = tw.Flush()

	fmt.Fprintf(&b, "\nSources (%d)\n", len(rep.Sources))
	tw = tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, s := range rep.Sources {
		status := "ok"
		if s.Err != "" {
			status = "FAIL"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%d offers, %d matched\t%s\n", status, s.Shop, s.Mode, s.Offers, s.Matched, s.URL)
		if s.Err != "" {
			fmt.Fprintf(tw, "  \t\t\t%s\t\n", s.Err)
		}
	}
	_ = tw.Flush()

	fmt.Fprintf(&b, "\nUnmatched / ambiguous (%d)\n", len(rep.Unmatched))
	tw = tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, u := range rep.Unmatched {
		cands := ""
		if len(u.Candidates) > 0 {
			cands = "[" + strings.Join(u.Candidates, ", ") + "]"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s  %s\n", u.Status, u.Shop, cands, u.Title, u.URL)
	}
	_ = tw.Flush()

	_, err := io.WriteString(w, b.String())
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
