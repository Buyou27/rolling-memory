// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import (
	"strings"
	"time"

	"github.com/rocareer/rolling-memory/sdk/go/public/closedset"
)

// DeicticTimeAnnotation is the delivery-layer fact statement produced by the
// deterministic deictic time-expression resolver (Part IV closed set). The
// engine never invents a clock: the reference day must be explicitly declared
// in the query itself (a sentence carrying the @cal:deix:today class together
// with exactly one full date). The annotation only restates the calendar
// interval that the relative class denotes relative to that declared day; it
// is delivered to the downstream reader and never participates in ranking or
// filtering (relative-time ranking injection stays falsified).
type DeicticTimeAnnotation struct {
	Class        string `json:"class"`         // Part IV class tag, e.g. "@cal:deix:lastweek"
	StartDay     string `json:"start_day"`     // inclusive ISO day
	EndDay       string `json:"end_day"`       // inclusive ISO day
	ReferenceDay string `json:"reference_day"` // explicitly declared anchor day
}

// ResolveDeicticTimeAnnotation resolves one relative everyday time expression
// (@cal:deix:*) in the query into its concrete calendar interval:
//
//  1. Reference day: at least one sentence carries the @cal:deix:today class
//     and the today-anchored sentences together declare exactly one distinct
//     full date (natural-language month/day/year form or a structured ISO
//     token). A missing or ambiguous reference abstains (fail-open to silence,
//     never to a guessed clock).
//  2. Target class: deictic classes found in the query minus the today marker
//     itself; exactly one distinct class must remain (two competing relative
//     frames abstain).
//  3. Interval: pure closed-set calendar arithmetic (ISO-8601 weeks start on
//     Monday; month and year units use calendar boundaries).
//
// Daypart classes (@cal:daypart:*) carry no calendar interval and are not
// annotated here.
func (a *TermExtractor) ResolveDeicticTimeAnnotation(queryText string) *DeicticTimeAnnotation {
	if a == nil || a.enc == nil || queryText == "" {
		return nil
	}
	classes := make(map[string]bool, 2)
	refDays := make(map[string]bool, 2)
	hasTodayAnchor := false
	for _, sent := range splitSentences(CanonicalizeApostrophe(queryText)) {
		sentHasToday := false
		addClass := func(cls string) {
			if cls == "" {
				return
			}
			if cls == "@cal:deix:today" {
				sentHasToday = true
				return
			}
			classes[cls] = true
		}
		// Single-token surfaces: CJK bigram/span tokens (昨天 / 上周 / 上个月) and
		// one-word expressions (yesterday / morgen / 지난주 ...).
		for _, w := range SplitWords(sent) {
			addClass(closedset.LookupEverydayTimeClass(strings.ToLower(strings.Trim(w, ",.;:!?()[]{}\"'"))))
		}
		// Whitespace-adjacent multi-token surfaces concatenate into the closed
		// set's joined forms ("last week" -> lastweek, "year before last" ->
		// yearbeforelast, "the week before last" via its 3-token tail). The
		// Part IV table is query-side only, so these windows are its intended
		// reachability contract and need no decoder pair-fusion parity.
		rawToks := strings.Fields(strings.TrimSpace(sent))
		tokenLows := make([]string, 0, len(rawToks))
		for _, rt := range rawToks {
			tokenLows = append(tokenLows, strings.ToLower(strings.Trim(rt, ",.;:!?()[]{}\"'")))
		}
		for size := 2; size <= 3; size++ {
			for i := 0; i+size <= len(tokenLows); i++ {
				if tokenLows[i] == "" {
					continue
				}
				joined := tokenLows[i]
				ok := true
				for j := 1; j < size; j++ {
					if tokenLows[i+j] == "" {
						ok = false
						break
					}
					joined += tokenLows[i+j]
				}
				if ok {
					addClass(closedset.LookupEverydayTimeClass(joined))
				}
			}
		}
		daySet := make(map[string]bool, 2)
		for _, rt := range rawToks {
			// The extractor carries its own structural guards (minimum length,
			// minimum digit count, adjacent numeric spans), so every token may
			// be offered without a local prefilter.
			lt := strings.ToLower(strings.Trim(rt, ",.;:!?()[]{}\"'"))
			for _, iso := range ExtractStructuredDateHarmonics(lt) {
				daySet[iso] = true
			}
		}
		// Reference-day extraction runs the document-side scanning mode: the
		// query-side beacon gates (long-query comma skip, short-query window)
		// exist to avoid fabricating target beacons and must not suppress the
		// explicitly declared clock here. ("Today is December 13, 2023, ..."
		// is the canonical opener and must parse.)
		scanSentenceNaturalDates(rawToks, 0, 1, false, false, "", func(iso string, _, _ int) {
			daySet[iso] = true
		})
		if sentHasToday {
			hasTodayAnchor = true
			for d := range daySet {
				refDays[d] = true
			}
		}
	}
	if !hasTodayAnchor || len(refDays) != 1 || len(classes) != 1 {
		return nil
	}
	var refDay, class string
	for d := range refDays {
		refDay = d
	}
	for c := range classes {
		class = c
	}
	ref, err := time.Parse("2006-01-02", refDay)
	if err != nil {
		return nil
	}
	start, end, ok := deicticCalendarInterval(class, ref)
	if !ok {
		return nil
	}
	return &DeicticTimeAnnotation{
		Class:        class,
		StartDay:     start.Format("2006-01-02"),
		EndDay:       end.Format("2006-01-02"),
		ReferenceDay: refDay,
	}
}

// deicticCalendarInterval maps a Part IV deictic class to its inclusive
// calendar interval relative to the reference day. ISO-8601 week convention
// (Monday start) is a closed-set standard, not a tuned parameter.
func deicticCalendarInterval(class string, ref time.Time) (start, end time.Time, ok bool) {
	d := time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, time.UTC)
	day := func(offset int) time.Time { return d.AddDate(0, 0, offset) }
	switch class {
	case "@cal:deix:today", "@cal:deix:tonight":
		return d, d, true
	case "@cal:deix:yesterday", "@cal:deix:lastnight":
		return day(-1), day(-1), true
	case "@cal:deix:tomorrow":
		return day(1), day(1), true
	case "@cal:deix:beforeyesterday":
		return day(-2), day(-2), true
	case "@cal:deix:aftertomorrow":
		return day(2), day(2), true
	}
	monday := day(-((int(d.Weekday()) + 6) % 7))
	switch class {
	case "@cal:deix:thisweek":
		return monday, monday.AddDate(0, 0, 6), true
	case "@cal:deix:lastweek":
		return monday.AddDate(0, 0, -7), monday.AddDate(0, 0, -1), true
	case "@cal:deix:nextweek":
		return monday.AddDate(0, 0, 7), monday.AddDate(0, 0, 13), true
	case "@cal:deix:weekbeforelast":
		return monday.AddDate(0, 0, -14), monday.AddDate(0, 0, -8), true
	}
	first := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
	switch class {
	case "@cal:deix:thismonth":
		return first, first.AddDate(0, 1, -1), true
	case "@cal:deix:lastmonth":
		return first.AddDate(0, -1, 0), first.AddDate(0, 0, -1), true
	case "@cal:deix:nextmonth":
		return first.AddDate(0, 1, 0), first.AddDate(0, 2, -1), true
	case "@cal:deix:monthbeforelast":
		return first.AddDate(0, -2, 0), first.AddDate(0, -1, -1), true
	}
	jan1 := time.Date(d.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	dec31 := time.Date(d.Year(), time.December, 31, 0, 0, 0, 0, time.UTC)
	switch class {
	case "@cal:deix:thisyear":
		return jan1, dec31, true
	case "@cal:deix:lastyear":
		return jan1.AddDate(-1, 0, 0), dec31.AddDate(-1, 0, 0), true
	case "@cal:deix:nextyear":
		return jan1.AddDate(1, 0, 0), dec31.AddDate(1, 0, 0), true
	case "@cal:deix:yearbeforelast":
		return jan1.AddDate(-2, 0, 0), dec31.AddDate(-2, 0, 0), true
	}
	return time.Time{}, time.Time{}, false
}
