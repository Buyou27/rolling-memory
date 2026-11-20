// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package ids implements deterministic identifiers, normalization, time
// parsing and small helpers.
//
// Everything here is pure stdlib and side-effect free; the same inputs must
// produce the same ids on every machine (idempotency depends on it).
package ids

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rocareer/rolling-memory/internal/clock"
	"github.com/rocareer/rolling-memory/internal/speech"
	"github.com/rocareer/rolling-memory/internal/text"
	"github.com/rocareer/rolling-memory/sdk/go/public/tokenzr"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

var cjkRanges = [][2]rune{
	{0x3400, 0x4DBF},
	{0x4E00, 0x9FFF},
	{0xF900, 0xFAFF},
	{0x3040, 0x30FF},
	{0xAC00, 0xD7AF},
}

// isTokenFilteredByDelivery reports whether tok carries only closed-class glue
// information. The judgement is purely mathematical (128k BPE merge-tree prior ×
// Shannon self-information gain), with zero static word lists:
//   - Latin/Digit tokens are glue unless they reach the primary gain tier;
//   - CJK tokens are glue only inside the low-prior band (closed-class particles,
//     pronouns and interrogatives cluster there, content words and n-grams do not).
//
// Single-character CJK date units are rescued by TokenizeAll's numeral-adjacency
// rule instead of any vocabulary.
func isTokenFilteredByDelivery(tok string) bool {
	if tok == "" {
		return true
	}
	if text.IsPureCJK(tok) {
		return tokenzr.TokenPriorBits(tok) < cjkGluePriorBits
	}
	// Short Latin identifiers (AI / Go / db / K8s) carry specific-instance value even
	// when they sit below the gain tier, so they are never treated as glue.
	if text.StringShannonBits(tok) < latinGlueMinBits {
		return false
	}
	return !text.IsHighGainToken(tok)
}

// latinGlueMinBits is the 6.0b short-identifier floor of the Shannon bit field:
// tokens below it are acronym/code surfaces rather than closed-class morphemes.
const latinGlueMinBits = 6.0

// cjkGluePriorBits is the closed-class band of the 128k BPE merge-tree prior:
// morphemes below it are near-deterministic continuations, morphemes above it
// carry compositional information. The unit is bits, not a script-specific count.
const cjkGluePriorBits = 11.0

// pySpaceChars is the character class behind standard str.isspace and re \s.
const pySpaceChars = `\t\n\v\f\r\x{001C}-\x{001F} \x{0085}\x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}`

const pySpaceClass = "[" + pySpaceChars + "]"

var (
	tokenRE = regexp.MustCompile(`[A-Za-z0-9_]+|[\x{3040}-\x{30ff}\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}\x{f900}-\x{faff}\x{ac00}-\x{d7af}]+`)
	dateRE  = regexp.MustCompile(`^([0-9]{4})[-/年]([0-9]{1,2})[-/月]([0-9]{1,2})日?$`)

	monthDayRE = regexp.MustCompile(`^([0-9]{1,2})月([0-9]{1,2})日?$`)

	relDaysBeforeRe     = regexp.MustCompile(`^([0-9]+)` + pySpaceClass + `*天前$`)
	relDaysAfterRe      = regexp.MustCompile(`^([0-9]+)` + pySpaceClass + `*天后$`)
	relWeeksBefore      = regexp.MustCompile(`^([0-9]+)` + pySpaceClass + `*(?:周|个星期)前$`)
	relWeeksAfter       = regexp.MustCompile(`^([0-9]+)` + pySpaceClass + `*(?:周|个星期)后$`)
	relWeekMonthRe      = regexp.MustCompile(`^(last|this|next)` + pySpaceClass + `+(week|month)$`)
	relWeekendRe        = regexp.MustCompile(`^(?:this past|last)` + pySpaceClass + `+weekend$`)
	relLastWeekday      = regexp.MustCompile(`^last` + pySpaceClass + `+(monday|tuesday|wednesday|thursday|friday|saturday|sunday)$`)
	relDurationOffsetRe = regexp.MustCompile(`^(?:for` + pySpaceClass + `+(?:about` + pySpaceClass + `+|around` + pySpaceClass + `+)?([0-9]{1,2}|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)` + pySpaceClass + `+(months?|years?)(?:` + pySpaceClass + `+now)?|([0-9]{1,2}|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)` + pySpaceClass + `+(months?|years?)` + pySpaceClass + `+(?:now|ago))$`)
)

// UTCNow returns the current Unix time in whole seconds.
func UTCNow() int64 {
	return clock.Unix()
}

// SHA256Hex returns the lowercase hex SHA-256 digest of value.
func SHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// StableJSON mirrors json.dumps(v, ensure_ascii=False, sort_keys=True,
// separators=(",", ":")).
func StableJSON(value any) string {
	encoded, err := PyJSONDump(value, false, true, true)
	if err != nil {
		return ""
	}
	return encoded
}

// IsCJKChar reports whether char falls in the tokenizer CJK ranges.
func IsCJKChar(char rune) bool {
	for _, span := range cjkRanges {
		if span[0] <= char && char <= span[1] {
			return true
		}
	}
	return false
}

// HasCJK reports whether text contains at least one CJK character.
func HasCJK(text string) bool {
	for _, char := range text {
		if IsCJKChar(char) {
			return true
		}
	}
	return false
}

// NormalizeWithMap NFKC/casefolds text and collapses whitespace, keeping a
// char map. indexMap[i] is the index in the original text that produced
// normalized character i; the map translates a normalized substring match
// back to a verbatim anchor span in the original evidence.
func NormalizeWithMap(text string) (string, []int) {
	var out strings.Builder
	out.Grow(len(text))
	mapping := make([]int, 0, len(text))
	pendingSpace := false
	runeIndex := 0
	folder := cases.Fold()
	for _, char := range text {
		index := runeIndex
		runeIndex++
		if isPySpace(char) {
			if out.Len() > 0 {
				pendingSpace = true
			}
			continue
		}
		if pendingSpace {
			out.WriteByte(' ')
			mapping = append(mapping, index)
			pendingSpace = false
		}
		folded := folder.String(norm.NFKC.String(string(char)))
		for _, result := range folded {
			out.WriteRune(result)
			mapping = append(mapping, index)
		}
	}
	return out.String(), mapping
}

// NormalizeText normalizes text and strips surrounding whitespace.
func NormalizeText(text string) string {
	normalized, _ := NormalizeWithMap(text)
	return pyTrimSpace(normalized)
}

// Tokenize implements the deterministic query tokenizer for CJK + Latin text
// with the legacy 48-term golden cap preserved for contract compatibility.
func Tokenize(text string) []string {
	unique := TokenizeAll(text)
	if len(unique) > 48 {
		unique = unique[:48]
	}
	return unique
}

// adjacentToNumeral reports whether the previous or next surface token is a pure
// numeral. A numeral-adjacent single-character CJK unit is a calendar or classifier
// unit (year/month/day/measure word), so it stays as structural context even when
// its standalone prior sits inside the closed-class band.
func adjacentToNumeral(matches []string, idx int) bool {
	for _, j := range []int{idx - 1, idx + 1} {
		if j < 0 || j >= len(matches) {
			continue
		}
		s := matches[j]
		if s == "" {
			continue
		}
		allDigits := true
		for _, r := range s {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return true
		}
	}
	return false
}

// TokenizeAll extracts all unique CJK + Latin query terms without the legacy
// 48-term position truncation (which previously cut off Chinese sentences after
// ~25 characters due to bigram/trigram expansion), bounded only by a 512-term
// (TokenTier512) anti-DoS ceiling.
func TokenizeAll(text string) []string {
	normalized := NormalizeText(text)
	terms := make([]string, 0, 16)
	matches := tokenRE.FindAllString(normalized, -1)
	for idx, match := range matches {
		token := cases.Lower(language.Und).String(match)
		if HasCJK(token) {
			length := utf8.RuneCountInString(token)
			if length == 1 {
				if !isTokenFilteredByDelivery(token) || adjacentToNumeral(matches, idx) {
					terms = append(terms, token)
				}
				continue
			}
			if !isTokenFilteredByDelivery(token) && length <= 12 {
				terms = append(terms, token)
			}
			runes := []rune(token)
			for index := 0; index < len(runes)-1; index++ {
				bigram := string(runes[index : index+2])
				if !isTokenFilteredByDelivery(bigram) {
					terms = append(terms, bigram)
				}
			}
			if length > 3 {
				for index := 0; index < len(runes)-2; index++ {
					trigram := string(runes[index : index+3])
					if !isTokenFilteredByDelivery(trigram) {
						terms = append(terms, trigram)
					}
				}
			}
		} else if utf8.RuneCountInString(token) >= 2 {
			if !isTokenFilteredByDelivery(token) {
				terms = append(terms, token)
			}
		}
	}
	seen := make(map[string]struct{}, len(terms))
	unique := make([]string, 0, len(terms))
	for _, term := range terms {
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		unique = append(unique, term)
		if len(unique) >= 512 {
			break
		}
	}
	return unique
}

// SQLiteFileURI returns path in the `file:`-prefixed URI form SQLite DSNs use,
// percent-encoding the URI metacharacters '?' and '#' when present. Without the
// encoding the driver silently parses the remainder as query or fragment and
// opens a different file. Windows paths always take the percent-encoded form
// with the drive letter or share kept inside the URI path (`file:///C:/...`,
// `file:////server/share/...`): an unanchored `C:\...` renders as
// `file://C:%5C...`, the drive letter becomes the URI authority, and the
// driver rejects the DSN with "invalid uri authority".
func SQLiteFileURI(path string) string {
	if uriPath, ok := WindowsURIPath(path); ok {
		return (&url.URL{Scheme: "file", Path: uriPath}).String()
	}
	if !strings.ContainsAny(path, "?#") {
		return "file:" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// WindowsURIPath reports the file: URI path form of a Windows drive or UNC
// path, rewriting the separators to forward slashes; other paths return
// ok=false unchanged.
func WindowsURIPath(path string) (string, bool) {
	if len(path) >= 2 && path[1] == ':' {
		if c := path[0]; 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' {
			return "/" + strings.ReplaceAll(path, `\`, "/"), true
		}
	}
	if strings.HasPrefix(path, `\\`) {
		return strings.ReplaceAll(path, `\`, "/"), true
	}
	return "", false
}

func localMidnight(value time.Time) int64 {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local).Unix()
}

// ParseTimeOptions controls ParseTime. TimeRef nil means "use now"; a
// non-nil zero is a valid explicit reference.
type ParseTimeOptions struct {
	TimeRef *int64
	Default int64
}

// ParseTime parses common absolute/relative time expressions into Unix
// seconds. Unknown input returns opts.Default; callers must not invent a time.
func ParseTime(value any, opts ParseTimeOptions) int64 {
	if value == nil {
		return opts.Default
	}
	switch typed := value.(type) {
	case bool:
		return opts.Default
	case string:
		if typed == "" {
			return opts.Default
		}
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64:
		number := int64(0)
		switch n := typed.(type) {
		case int:
			number = int64(n)
		case int8:
			number = int64(n)
		case int16:
			number = int64(n)
		case int32:
			number = int64(n)
		case int64:
			number = n
		case uint:
			number = int64(n)
		case uint8:
			number = int64(n)
		case uint16:
			number = int64(n)
		case uint32:
			number = int64(n)
		case uint64:
			number = int64(n)
		case float32:
			number = int64(n)
		case float64:
			number = int64(n)
		}
		if number > 10_000_000_000 {
			number /= 1000
		}
		return number
	}

	text := pyTrimSpace(fmt.Sprint(value))
	if text == "" {
		return opts.Default
	}
	if isASCIIDigits(text) {
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return opts.Default
		}
		if number > 10_000_000_000 {
			number /= 1000
		}
		return number
	}

	reference := clock.Unix()
	if opts.TimeRef != nil {
		reference = *opts.TimeRef
	}
	referenceDT := time.Unix(reference, 0).In(time.Local)
	today := time.Date(referenceDT.Year(), referenceDT.Month(), referenceDT.Day(), 0, 0, 0, 0, time.Local)

	lowered := cases.Lower(language.Und).String(text)
	relativeDays := map[string]int{
		"今天": 0, "今日": 0, "昨天": -1, "昨日": -1, "前天": -2,
		"明天": 1, "明日": 1, "后天": 2, "大后天": 3,
		"上周": -7, "上星期": -7, "本周": 0, "这周": 0, "下周": 7, "下星期": 7,
		"today": 0, "yesterday": -1, "tomorrow": 1,
	}
	if days, ok := relativeDays[text]; ok {
		return localMidnight(today.AddDate(0, 0, days))
	}
	if days, ok := relativeDays[lowered]; ok {
		return localMidnight(today.AddDate(0, 0, days))
	}
	switch text {
	case "去年", "last year":
		return localMidnight(replaceYear(today, today.Year()-1))
	case "今年", "this year":
		return localMidnight(time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, time.Local))
	case "明年", "next year":
		return localMidnight(time.Date(today.Year()+1, time.January, 1, 0, 0, 0, 0, time.Local))
	}

	if match := relDaysBeforeRe.FindStringSubmatch(text); match != nil {
		days, _ := strconv.Atoi(match[1])
		return localMidnight(today.AddDate(0, 0, -days))
	}
	if match := relDaysAfterRe.FindStringSubmatch(text); match != nil {
		days, _ := strconv.Atoi(match[1])
		return localMidnight(today.AddDate(0, 0, days))
	}
	if match := relWeeksBefore.FindStringSubmatch(text); match != nil {
		weeks, _ := strconv.Atoi(match[1])
		return localMidnight(today.AddDate(0, 0, -7*weeks))
	}
	if match := relWeeksAfter.FindStringSubmatch(text); match != nil {
		weeks, _ := strconv.Atoi(match[1])
		return localMidnight(today.AddDate(0, 0, 7*weeks))
	}

	// English relative week / month / weekday forms; resolution still needs
	// an explicit time_ref — the engine never guesses.
	if match := relWeekMonthRe.FindStringSubmatch(lowered); match != nil {
		direction, unit := match[1], match[2]
		if unit == "week" {
			offset := map[string]int{"last": -7, "this": 0, "next": 7}[direction]
			return localMidnight(today.AddDate(0, 0, offset))
		}
		months := map[string]int{"last": -1, "this": 0, "next": 1}[direction]
		monthIndex := today.Year()*12 + int(today.Month()-1) + months
		year := monthIndex / 12
		month := time.Month(monthIndex%12 + 1)
		lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.Local).Day()
		day := today.Day()
		if day > lastDay {
			day = lastDay
		}
		return localMidnight(time.Date(year, month, day, 0, 0, 0, 0, time.Local))
	}
	if relWeekendRe.MatchString(lowered) {
		weekday := pythonWeekday(today)
		daysBack := ((weekday-5)%7 + 7) % 7
		if daysBack == 0 {
			daysBack = 7
		}
		return localMidnight(today.AddDate(0, 0, -daysBack))
	}
	if match := relDurationOffsetRe.FindStringSubmatch(lowered); len(match) == 5 {
		numStr, unitStr := match[1], match[2]
		if numStr == "" {
			numStr, unitStr = match[3], match[4]
		}
		if n := parseSmallCount(numStr); n > 0 {
			if strings.HasPrefix(unitStr, "month") {
				monthIndex := today.Year()*12 + int(today.Month()-1) - n
				year := monthIndex / 12
				month := time.Month(monthIndex%12 + 1)
				return localMidnight(time.Date(year, month, 1, 0, 0, 0, 0, time.Local))
			}
			if strings.HasPrefix(unitStr, "year") {
				return localMidnight(time.Date(today.Year()-n, today.Month(), 1, 0, 0, 0, 0, time.Local))
			}
		}
	}
	if match := relLastWeekday.FindStringSubmatch(lowered); match != nil {
		target := map[string]int{
			"monday": 0, "tuesday": 1, "wednesday": 2, "thursday": 3,
			"friday": 4, "saturday": 5, "sunday": 6,
		}[match[1]]
		weekday := pythonWeekday(today)
		daysBack := ((weekday-target)%7 + 7) % 7
		if daysBack == 0 {
			daysBack = 7
		}
		return localMidnight(today.AddDate(0, 0, -daysBack))
	}

	if match := dateRE.FindStringSubmatch(text); match != nil {
		year, _ := strconv.Atoi(match[1])
		month, _ := strconv.Atoi(match[2])
		day, _ := strconv.Atoi(match[3])
		if parsed, ok := dateFromYMD(year, month, day); ok {
			return localMidnight(parsed)
		}
		return opts.Default
	}
	if match := monthDayRE.FindStringSubmatch(text); match != nil {
		month, _ := strconv.Atoi(match[1])
		day, _ := strconv.Atoi(match[2])
		if parsed, ok := dateFromYMD(today.Year(), month, day); ok {
			return localMidnight(parsed)
		}
		return opts.Default
	}

	candidate := strings.ReplaceAll(text, "Z", "+00:00")
	if parsed, ok := parseISO(candidate); ok {
		return unixSeconds(parsed)
	}
	if ext, ok := parseExtendedTime(text, today); ok {
		return ext
	}
	return opts.Default
}

// FormatTimeSeconds formats epoch as a local YYYY-MM-DD HH:MM:SS timestamp with
// its numeric UTC offset. Recall delivery labels use this shape so a reader
// never has to guess the zone behind a clock reading, and same-day moments stay
// distinguishable instead of collapsing into one date.
func FormatTimeSeconds(epoch int64, empty string) string {
	if epoch == 0 {
		return empty
	}
	return time.Unix(epoch, 0).In(time.Local).Format("2006-01-02 15:04:05 -07:00")
}

// TraceHash returns the first 32 hex characters of the trace identity.
func TraceHash(scope, session string, turn int64, text string) string {
	return SHA256Hex(strings.Join([]string{scope, session, strconv.FormatInt(turn, 10), text}, "\x1f"))[:32]
}

// StripInjected removes harness-injected context blocks and non-speech protocol
// envelopes from transcript text. The rules live in `internal/speech` (form-only
// protocol vocabulary, zero word lists) so ingest paths and the local vault client strip
// conversation turns identically.
func StripInjected(text string) string {
	return speech.StripConversationNoise(text)
}

// Truncate shortens stripped text to limit characters, appending suffix.
func Truncate(text string, limit int, suffix string) string {
	value := pyTrimSpace(text)
	length := utf8.RuneCountInString(value)
	if length <= limit {
		return value
	}
	suffixLength := utf8.RuneCountInString(suffix)
	if limit <= suffixLength {
		return runeSlice(value, 0, limit)
	}
	return strings.TrimRightFunc(runeSlice(value, 0, limit-suffixLength), isPySpace) + suffix
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`(?:ghp|gho|ghu|ghs)_[A-Za-z0-9_]{8,}`),
	regexp.MustCompile(`(?i)Bearer` + pySpaceClass + `+[A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`access_token=[^&` + pySpaceChars + `]+`),
	regexp.MustCompile(`token=[A-Za-z0-9._\-]{8,}`),
}

// Redact masks credential-looking substrings.
func Redact(text string) string {
	value := text
	for _, pattern := range secretPatterns {
		value = redactPattern(value, pattern)
	}
	return value
}

// IsLoopbackHost reports whether host names the loopback interface.
func IsLoopbackHost(host string) bool {
	if host == "" {
		return false
	}
	candidate := strings.ToLower(strings.Trim(host, "[]"))
	if candidate == "localhost" || strings.HasSuffix(candidate, ".localhost") {
		return true
	}
	parsed := net.ParseIP(candidate)
	return parsed != nil && parsed.IsLoopback()
}

// URLHost returns the lowercased hostname of raw, if parseable.
func URLHost(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := parsed.Hostname()
	if host == "" {
		return "", false
	}
	return strings.ToLower(host), true
}

func parseSmallCount(s string) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	switch s {
	case "one":
		return 1
	case "two":
		return 2
	case "three":
		return 3
	case "four":
		return 4
	case "five":
		return 5
	case "six":
		return 6
	case "seven":
		return 7
	case "eight":
		return 8
	case "nine":
		return 9
	case "ten":
		return 10
	case "eleven":
		return 11
	case "twelve":
		return 12
	}
	return 0
}

// Mean returns the arithmetic mean, or 0 for an empty slice. Summation uses
// standard 3.12+ Neumaier compensation to match float results bit-for-bit.
func Mean(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	return pySum(values) / float64(len(values))
}

func pySum(values []float64) float64 {
	total := 0.0
	compensation := 0.0
	for _, value := range values {
		next := total + value
		if math.Abs(total) >= math.Abs(value) {
			compensation += (total - next) + value
		} else {
			compensation += (value - next) + total
		}
		total = next
	}
	return total + compensation
}

// --------------------------------------------------------------------- helpers

// isPySpace delegates to the shared protocol/space vocabulary in `internal/speech`.
func isPySpace(char rune) bool {
	return speech.IsPySpace(char)
}

func pyTrimSpace(value string) string {
	return speech.TrimSpace(value)
}

// PyRound mirrors Python's round(value, digits) as a rendered-and-reparsed
// decimal, which is the form every delivered receipt and fixture asserts on.
func PyRound(value float64, digits int) float64 {
	text := strconv.FormatFloat(value, 'f', digits, 64)
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return value
	}
	return parsed
}

// QuotePyString renders a string as a single-quoted Python literal for
// diagnostic text. It escapes backslashes and single quotes only; callers that
// need full Python repr semantics (newlines, control bytes) keep their own
// renderer.
func QuotePyString(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	return "'" + escaped + "'"
}

// DerefString dereferences an optional string for value rendering: nil becomes
// the empty string.
func DerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// OptionalNonEmptyString is OptionalString that additionally treats the empty
// string as absent, so an explicitly blank field never renders as a value.
func OptionalNonEmptyString(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func dateFromYMD(year, month, day int) (time.Time, bool) {
	if month < 1 || month > 12 || day < 1 || year < 1 || year > 9999 {
		return time.Time{}, false
	}
	value := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	if value.Year() != year || int(value.Month()) != month || value.Day() != day {
		return time.Time{}, false
	}
	return value, true
}

func replaceYear(value time.Time, year int) time.Time {
	day := value.Day()
	if value.Month() == time.February && day == 29 && !isLeapYear(year) {
		day = 28
	}
	return time.Date(year, value.Month(), day, 0, 0, 0, 0, time.Local)
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

func pythonWeekday(value time.Time) int {
	return (int(value.Weekday()) + 6) % 7
}

// unixSeconds mirrors standard int(datetime.timestamp()): truncation toward zero
// rather than Go's floor for pre-epoch fractional seconds.
func unixSeconds(value time.Time) int64 {
	seconds := value.Unix()
	if value.Nanosecond() > 0 && seconds < 0 {
		seconds++
	}
	return seconds
}

// parseISO mirrors the documented datetime.fromisoformat subset: dates, basic
// dates, ISO week dates, and date-time values with an optional local offset.
func parseISO(text string) (time.Time, bool) {
	if parsed, ok := parseISODate(text); ok {
		return parsed, true
	}
	separator := strings.IndexAny(text, "T ")
	if separator < 0 {
		return time.Time{}, false
	}
	datePart := text[:separator]
	timePart := text[separator+1:]
	date, ok := parseISODate(datePart)
	if !ok {
		return time.Time{}, false
	}
	year, month, day := date.Date()

	hours, minutes, seconds, nanoseconds, offset, zoned, ok := parseISOTime(timePart)
	if !ok {
		return time.Time{}, false
	}
	location := time.Local
	if zoned {
		location = time.FixedZone("", offset)
	}
	return time.Date(year, month, day, hours, minutes, seconds, nanoseconds, location), true
}

func parseISODate(text string) (time.Time, bool) {
	if len(text) == 10 && text[4] == '-' && text[7] == '-' {
		year, month, day, ok := splitDigits(text, []int{4, 2, 2}, []byte{'-', '-'})
		if !ok {
			return time.Time{}, false
		}
		return dateFromYMD(year, month, day)
	}
	if len(text) == 8 && isASCIIDigits(text) {
		year, _ := strconv.Atoi(text[0:4])
		month, _ := strconv.Atoi(text[4:6])
		day, _ := strconv.Atoi(text[6:8])
		return dateFromYMD(year, month, day)
	}
	if len(text) == 10 && text[4] == '-' && text[5] == 'W' && text[8] == '-' {
		year, err1 := strconv.Atoi(text[0:4])
		week, err2 := strconv.Atoi(text[6:8])
		weekday, err3 := strconv.Atoi(text[9:10])
		if err1 != nil || err2 != nil || err3 != nil || week < 1 || week > 53 || weekday < 1 || weekday > 7 {
			return time.Time{}, false
		}
		jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.Local)
		weekOne := jan4.AddDate(0, 0, -(pythonWeekday(jan4)))
		return weekOne.AddDate(0, 0, (week-1)*7+(weekday-1)), true
	}
	return time.Time{}, false
}

func parseISOTime(text string) (hours, minutes, seconds, nanoseconds, offset int, zoned, ok bool) {
	if len(text) < 5 || text[2] != ':' {
		return 0, 0, 0, 0, 0, false, false
	}
	hourValue, err := strconv.Atoi(text[0:2])
	if err != nil {
		return 0, 0, 0, 0, 0, false, false
	}
	minuteValue, err := strconv.Atoi(text[3:5])
	if err != nil {
		return 0, 0, 0, 0, 0, false, false
	}
	position := 5
	secondValue := 0
	if len(text) >= position+3 && text[position] == ':' {
		secondValue, err = strconv.Atoi(text[position+1 : position+3])
		if err != nil {
			return 0, 0, 0, 0, 0, false, false
		}
		position += 3
	}
	nanoValue := 0
	if len(text) > position && (text[position] == '.' || text[position] == ',') {
		position++
		start := position
		for position < len(text) && text[position] >= '0' && text[position] <= '9' {
			position++
		}
		if start == position {
			return 0, 0, 0, 0, 0, false, false
		}
		fraction := text[start:position]
		if len(fraction) > 9 {
			fraction = fraction[:9]
		}
		for len(fraction) < 9 {
			fraction += "0"
		}
		nanoValue, _ = strconv.Atoi(fraction)
	}
	if position < len(text) {
		offset, ok = parseISOOffset(text[position:])
		if !ok {
			return 0, 0, 0, 0, 0, false, false
		}
		zoned = true
	}
	if hourValue < 0 || hourValue > 23 || minuteValue < 0 || minuteValue > 59 ||
		secondValue < 0 || secondValue > 59 {
		return 0, 0, 0, 0, 0, false, false
	}
	return hourValue, minuteValue, secondValue, nanoValue, offset, zoned, true
}

func parseISOOffset(text string) (int, bool) {
	if text == "" || (text[0] != '+' && text[0] != '-') {
		return 0, false
	}
	sign := 1
	if text[0] == '-' {
		sign = -1
	}
	rest := text[1:]
	var hourText, minuteText string
	switch {
	case len(rest) == 4 && rest[2] == ':':
		hourText, minuteText = rest[:2], rest[3:]
	case len(rest) == 5 && rest[2] == ':':
		hourText, minuteText = rest[:2], rest[3:5]
	case len(rest) == 4:
		hourText, minuteText = rest[:2], rest[2:]
	case len(rest) == 8 && rest[2] == ':' && rest[5] == ':':
		hourText, minuteText = rest[:2], rest[3:5]
		seconds, err := strconv.Atoi(rest[6:8])
		if err != nil || seconds > 59 {
			return 0, false
		}
	default:
		return 0, false
	}
	hours, err := strconv.Atoi(hourText)
	if err != nil || hours > 23 {
		return 0, false
	}
	minutes := 0
	if minuteText != "" {
		if len(minuteText) != 2 {
			return 0, false
		}
		minutes, err = strconv.Atoi(minuteText)
		if err != nil || minutes > 59 {
			return 0, false
		}
	}
	return sign * (hours*3600 + minutes*60), true
}

func splitDigits(text string, widths []int, separators []byte) (int, int, int, bool) {
	values := make([]int, len(widths))
	position := 0
	for index, width := range widths {
		if index > 0 {
			if position >= len(text) || text[position] != separators[index-1] {
				return 0, 0, 0, false
			}
			position++
		}
		if position+width > len(text) {
			return 0, 0, 0, false
		}
		value, err := strconv.Atoi(text[position : position+width])
		if err != nil {
			return 0, 0, 0, false
		}
		values[index] = value
		position += width
	}
	if position != len(text) || len(values) != 3 {
		return 0, 0, 0, false
	}
	return values[0], values[1], values[2], true
}

func isPyWord(char rune) bool {
	return speech.IsPyWord(char)
}

func runeSlice(value string, start, end int) string {
	runes := []rune(value)
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start > end {
		start = end
	}
	return string(runes[start:end])
}

func redactPattern(text string, pattern *regexp.Regexp) string {
	var out strings.Builder
	last := 0
	offset := 0
	for offset <= len(text) {
		location := pattern.FindStringIndex(text[offset:])
		if location == nil {
			break
		}
		start := offset + location[0]
		end := offset + location[1]
		if start > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:start])
			if isPyWord(previous) {
				_, size := utf8.DecodeRuneInString(text[start:])
				if size <= 0 {
					size = 1
				}
				offset = start + size
				continue
			}
		}
		out.WriteString(text[last:start])
		out.WriteString("***")
		last = end
		offset = end
	}
	if last == 0 {
		return text
	}
	out.WriteString(text[last:])
	return out.String()
}
