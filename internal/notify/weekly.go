package notify

import (
	"strings"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// WeekRow compares one product's lowest price this week with last week.
type WeekRow struct {
	Product   string
	Name      string
	Condition string
	This      Low // last 7 Vietnam days, including today
	Last      Low // the 7 days before that
}

// Low is the lowest price seen in a period (Price 0: none seen).
type Low struct {
	PriceVND int64
	Shop     string
	URL      string
}

// IsWeeklyDay reports whether now (Vietnam time) is the weekly-summary day.
func IsWeeklyDay(n config.Notify, now time.Time) bool {
	return n.Weekly != "" && strings.EqualFold(now.In(saigon).Weekday().String(), n.Weekly)
}

// Weekly builds the summary from history, per product (watchlist order)
// and condition. Rows with no prices in either week are left out.
func Weekly(products []config.Product, historyDir string, now time.Time) ([]WeekRow, error) {
	today := dayStart(now)
	thisFrom := today.AddDate(0, 0, -6)
	lastFrom := today.AddDate(0, 0, -13)
	end := today.AddDate(0, 0, 1)

	var rows []WeekRow
	for _, p := range products {
		recs, err := state.ReadHistory(historyDir, p.ID)
		if err != nil {
			return nil, err
		}
		for _, cond := range []string{config.ConditionNew, config.ConditionUsed} {
			row := WeekRow{Product: p.ID, Name: p.Name, Condition: cond}
			for _, r := range recs {
				if r.Condition != cond || r.PriceVND <= 0 {
					continue
				}
				switch t := r.TS; {
				case !t.Before(thisFrom) && t.Before(end):
					lower(&row.This, r)
				case !t.Before(lastFrom) && t.Before(thisFrom):
					lower(&row.Last, r)
				}
			}
			if row.This.PriceVND > 0 || row.Last.PriceVND > 0 {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func lower(l *Low, r state.Record) {
	if l.PriceVND == 0 || r.PriceVND < l.PriceVND {
		*l = Low{PriceVND: r.PriceVND, Shop: r.Shop, URL: r.URL}
	}
}

// dayStart is midnight (Vietnam time) of the day containing t.
func dayStart(t time.Time) time.Time {
	l := t.In(saigon)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, saigon)
}
