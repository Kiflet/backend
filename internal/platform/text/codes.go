package text

// Who reads a code's words (docs/architecture.md, section 11: "Codes and
// their words"). The same status is "Issue" to the office, "Delayed" to
// the customer, and a button, "Report a problem", to the driver.
const (
	Office   = "office"
	Customer = "customer"
	Driver   = "driver"
)

// Kinds of codes with words.
const (
	KindStatus   = "status"
	KindReason   = "reason"
	KindRole     = "role"
	KindAlert    = "alert"
	KindHeld     = "held"    // why a delivery was held for review
	KindInvoice  = "invoice" // an invoice's state, and overdue
	KindPayment  = "payment"
	KindMove     = "move"    // a driver's button for a status
	KindRefusal  = "refusal" // why the server refused a driver's item
	KindRemoval  = "removal" // why a load was taken off a driver's phone
	KindPriority = "priority"
)

// Label is a code's words for an audience, in lang: the audience's own
// (<audience>.<kind>.<code>; the customer's are <kind>.<code>, as the public
// pages name them), else the office's, in lang and then in English. A code
// with no words at all is returned as it is, so prose from an older client
// passes through.
func Label(lang, audience, kind, code string) string {
	if l, ok := label(lang, audience, kind, code); ok {
		return l
	}
	if l, ok := label(English, audience, kind, code); ok {
		return l
	}
	return code
}

// HasLabel reports whether a code has words for the audience in lang
// itself, its own or the office's, without falling back to English.
func HasLabel(lang, audience, kind, code string) bool {
	_, ok := label(lang, audience, kind, code)
	return ok
}

func label(lang, audience, kind, code string) (string, bool) {
	c, ok := catalogues[lang]
	if !ok {
		return "", false
	}
	own := audience + "." + kind + "." + code
	if audience == Customer {
		own = kind + "." + code
	}
	if l, ok := c[own]; ok {
		return l, true
	}
	l, ok := c[Office+"."+kind+"."+code]
	return l, ok
}
