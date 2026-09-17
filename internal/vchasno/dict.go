package vchasno

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The Vchasno API speaks in numeric codes. Keeping the documented dictionaries
// here lets every tool answer with the meaning next to the code, so a model
// never has to guess what 7004 or category 23 stands for.

// StatusMeaning maps a document status code to its documented meaning.
var StatusMeaning = map[int]string{
	7000: "Uploaded to Vchasno, counterparty not set yet",
	7001: "Ready to sign and send (counterparty data present)",
	7002: "Sent to the counterparty for the first signature",
	7003: "Partially signed by the owner, or signed but not sent",
	7004: "Signed by the owner, waiting for the counterparty",
	7006: "Rejected by the counterparty",
	7007: "Partially signed by the recipient, or signed but not sent",
	7008: "Fully signed by every expected party",
	7010: "Sent to the signers (multilateral document)",
	7011: "Revoked (EDI documents only)",
}

// StatusName is the short machine-friendly name of a status code.
var StatusName = map[int]string{
	7000: "uploaded",
	7001: "ready_to_send",
	7002: "sent_for_first_signature",
	7003: "owner_partially_signed",
	7004: "waiting_for_counterparty",
	7006: "rejected",
	7007: "recipient_partially_signed",
	7008: "signed",
	7010: "multilateral_in_progress",
	7011: "revoked",
}

// statusAliases lets a tool accept "signed" or "rejected" instead of a code.
var statusAliases = map[string]int{}

func init() {
	for code, name := range StatusName {
		statusAliases[name] = code
	}
	statusAliases["finished"] = 7008
	statusAliases["done"] = 7008
	statusAliases["new"] = 7000
	statusAliases["waiting"] = 7004
	statusAliases["pending"] = 7004
}

// ParseStatus accepts a numeric code or one of the friendly names above.
func ParseStatus(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	if code, ok := statusAliases[s]; ok {
		return code, nil
	}
	return 0, fmt.Errorf("unknown status %q: use a code (7000…7011) or a name (%s)", s, strings.Join(StatusNames(), ", "))
}

// StatusNames lists the accepted friendly status names, sorted.
func StatusNames() []string {
	out := make([]string, 0, len(statusAliases))
	for name := range statusAliases {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// PublicCategories are the document types every company shares. A company may
// define more; list_document_categories returns the full, live list.
var PublicCategories = map[int]string{
	0:     "Тип не обрано",
	1:     "Акт наданих послуг",
	2:     "Рахунок",
	3:     "Договір",
	4:     "Додаткова угода до договору",
	5:     "Видаткова накладна",
	6:     "Товарно-транспортна накладна",
	7:     "EDI документи",
	8:     "Довіреність",
	9:     "Специфікація",
	10:    "Заява",
	11:    "Акт приймання-передачі",
	12:    "Повернення",
	13:    "Замовлення",
	14:    "Додаток",
	15:    "Інше",
	16:    "Акт звіряння",
	17:    "Протокол розбіжностей",
	18:    "Звіт",
	19:    "Лист",
	20:    "Протокол",
	21:    "Наказ",
	22:    "Акт повернення",
	23:    "Розрахунок коригування",
	24:    "Акт коригування",
	25:    "Гарантійний лист",
	26:    "Бухгалтерська довідка",
	27:    "Акт списання",
	28:    "Коригуюча накладна",
	29:    "Авансовий звіт",
	30:    "Акт інвентаризації",
	3616:  "Кошторис",
	3617:  "Технічне завдання",
	5386:  "Фінансова довідка",
	5387:  "Фінансова звітність",
	6662:  "Оферта",
	7932:  "Прибуткова накладна",
	8672:  "Заява на повернення коштів",
	9765:  "Видаткова накладна на повернення",
	9766:  "Звіт комісіонера",
	11486: "Додаток до акту",
	14664: "Заявка на перевезення",
	17436: "Ліцензія",
	18891: "Кредитна заява",
}

// ReviewStates are the accepted values of the review_state filter.
var ReviewStates = []string{"without_any", "pending", "approved", "rejected"}

// SDStatuses are the accepted values of the structured-data status filter.
var SDStatuses = []string{"pending", "awaiting_validation", "confirmed", "downloaded", "error"}

// AccessPeriods are the only day counts a public link accepts.
var AccessPeriods = []int{1, 3, 5, 7, 14, 30}

// Enrich fills the fields this server derives from the raw API answer:
// the status meaning, the category title and the amount in hryvnias.
func (d *Document) Enrich(categories map[int]string) {
	if d == nil {
		return
	}
	if m, ok := StatusMeaning[d.Status]; ok {
		d.StatusMeaning = m
	}
	if d.Category != nil {
		if title, ok := categories[*d.Category]; ok {
			d.CategoryTitle = title
		} else if title, ok := PublicCategories[*d.Category]; ok {
			d.CategoryTitle = title
		}
	}
	if d.Amount != nil {
		s := FormatKopiykas(*d.Amount)
		d.AmountUAH = &s
	}
}

// FormatKopiykas renders an integer amount in kopiykas as hryvnias.
func FormatKopiykas(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d.%02d", v/100, v%100)
	if neg {
		s = "-" + s
	}
	return s
}

// ParseAmountUAH converts "1234.56" or "1234,56" hryvnias into kopiykas.
func ParseAmountUAH(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	parts := strings.SplitN(s, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	var frac int64
	if len(parts) == 2 {
		f := (parts[1] + "00")[:2]
		frac, err = strconv.ParseInt(f, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("not a number: %q", s)
		}
	}
	total := whole*100 + frac
	if neg {
		total = -total
	}
	return total, nil
}
