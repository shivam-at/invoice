package invoice

import (
	"fmt"
	"strings"
)

var ones = []string{
	"", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
	"Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen", "Seventeen", "Eighteen", "Nineteen",
}

var tens = []string{"", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety"}

func twoDigitWords(n int) string {
	if n < 20 {
		return ones[n]
	}
	word := tens[n/10]
	if n%10 != 0 {
		word += " " + ones[n%10]
	}
	return word
}

func threeDigitWords(n int) string {
	if n < 100 {
		return twoDigitWords(n)
	}
	word := ones[n/100] + " Hundred"
	if n%100 != 0 {
		word += " " + twoDigitWords(n%100)
	}
	return word
}

// amountInWordsINR renders a rupee amount using the Indian numbering system
// (crore/lakh/thousand), matching the original app's declaration line, e.g.
// "Two Thousand Five Hundred Seventeen Rupees and Twenty Paise Only".
func amountInWordsINR(amount float64) string {
	rupees := int64(amount)
	paise := int(round2((amount-float64(rupees))*100) + 0.0001) // guard against float rounding

	var parts []string
	n := rupees
	if n == 0 {
		parts = append(parts, "Zero")
	} else {
		crore := n / 10000000
		n %= 10000000
		lakh := n / 100000
		n %= 100000
		thousand := n / 1000
		n %= 1000
		remainder := int(n)

		if crore > 0 {
			parts = append(parts, threeDigitWords(int(crore))+" Crore")
		}
		if lakh > 0 {
			parts = append(parts, threeDigitWords(int(lakh))+" Lakh")
		}
		if thousand > 0 {
			parts = append(parts, threeDigitWords(int(thousand))+" Thousand")
		}
		if remainder > 0 {
			parts = append(parts, threeDigitWords(remainder))
		}
	}

	words := strings.Join(parts, " ") + " Rupees"
	if paise > 0 {
		words += fmt.Sprintf(" and %s Paise", threeDigitWords(paise))
	}
	return words + " Only"
}
