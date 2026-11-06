package match

import "regexp"

// Offer conditions.
const (
	ConditionNew  = "new"
	ConditionUsed = "used"
)

// usedRe flags second-hand items. It runs on normalised text, so "cũ" is
// already "cu"; \b keeps it from matching inside words like "cuc".
//
// Percent grades ("98%", "95%") are limited to 80–99%, so a sale badge such as
// "giảm 10%" (10% off) is not mistaken for a used-item grade.
var usedRe = regexp.MustCompile(`\bcu\b|\bused\b|\blike ?new\b|\b[89]\d ?%|qua su dung|\b2nd\b`)

// DetectCondition guesses "used" or "new" from an offer's title and URL.
func DetectCondition(title, url string) string {
	if usedRe.MatchString(Normalize(title)) || usedRe.MatchString(Normalize(url)) {
		return ConditionUsed
	}
	return ConditionNew
}
