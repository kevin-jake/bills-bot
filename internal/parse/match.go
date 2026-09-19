package parse

import (
	"slices"
	"strings"
	"unicode"
)

// Candidate is a bill a typed name might mean.
type Candidate struct {
	ID      int64
	Name    string
	Aliases []string
	// Last4 is the card's last four digits, matched like a name of its own: the digits are
	// what someone reads off a statement, and typing them is quicker than "hsbc mastercard".
	Last4 string
}

// phrases are every form of a Candidate's name that a typed name may be compared against.
func (c Candidate) phrases() []string {
	all := append([]string{c.Name}, c.Aliases...)
	if c.Last4 != "" {
		all = append(all, c.Last4)
	}
	return all
}

// Scores MatchBill gives a candidate phrase, best first.
const (
	// scoreExact: the typed name is the phrase, "bpi cc" for BPI CC.
	scoreExact = 100
	// scoreTokens: every typed word is one of the phrase's, "pldt" for Internet PLDT.
	scoreTokens = 80
	// scorePrefix: every typed word begins one of the phrase's, "rcbc air" for RCBC Visa
	// Airmiles. A word has to be two letters or more to count as a beginning.
	scorePrefix = 60
)

// minPrefix is the shortest typed word that may stand for a longer one.
const minPrefix = 2

// synonyms are the household's own abbreviations, rewritten on both sides before comparing
// so that "bdo home loan" and "bdo hl" name the same bill.
var synonyms = [][2]string{
	{"credit card", "cc"},
	{"home loan", "hl"},
	{"psbank", "psb"},
}

// MatchBill returns the candidates name fits best: one when it is clear, several when they
// tie, none when nothing fits. A candidate's aliases count as much as its name.
func MatchBill(name string, candidates []Candidate) []Candidate {
	typed := tokens(name)
	if len(typed) == 0 {
		return nil
	}

	var best []Candidate
	bestScore := 0
	for _, c := range candidates {
		score := 0
		for _, phrase := range c.phrases() {
			score = max(score, scorePhrase(typed, tokens(phrase)))
		}
		switch {
		case score == 0 || score < bestScore:
		case score > bestScore:
			best, bestScore = []Candidate{c}, score
		default:
			best = append(best, c)
		}
	}
	return best
}

// Mentions reports whether any word in name, three letters or more, begins a word of some
// candidate. It tells a mistyped bill ("bdo jbc 5000") from chat that happens to end in a
// number ("see you at 5"), so that only the first is told nothing matched.
func Mentions(name string, candidates []Candidate) bool {
	for _, word := range tokens(name) {
		if len([]rune(word)) < 3 {
			continue
		}
		for _, c := range candidates {
			for _, phrase := range c.phrases() {
				for _, candidate := range tokens(phrase) {
					if strings.HasPrefix(candidate, word) {
						return true
					}
				}
			}
		}
	}
	return false
}

func scorePhrase(typed, phrase []string) int {
	if len(phrase) == 0 {
		return 0
	}
	if strings.Join(typed, " ") == strings.Join(phrase, " ") {
		return scoreExact
	}
	score := scoreTokens
	for _, word := range typed {
		switch {
		case slices.Contains(phrase, word):
		case len([]rune(word)) >= minPrefix && anyHasPrefix(phrase, word):
			score = scorePrefix
		default:
			return 0
		}
	}
	return score
}

// tokens normalises a name into words: lowercase, anything but letters and digits as a
// separator, and the household's abbreviations applied.
func tokens(s string) []string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s)
	s = " " + strings.Join(strings.Fields(s), " ") + " "
	for _, pair := range synonyms {
		s = strings.ReplaceAll(s, " "+pair[0]+" ", " "+pair[1]+" ")
	}
	return strings.Fields(s)
}

func anyHasPrefix(words []string, prefix string) bool {
	for _, w := range words {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}
