// Package numfmt writes the number of a list item in one of the number
// formats of WordprocessingML (ECMA-376 Part 1, 17.18.59 ST_NumberFormat):
// 4 as "iv", "D", "4th", "Four" and so on.
//
// The limits are Word's, from Microsoft's notes on the standard
// ([MS-OI29500] 2.1.1690): text formats write nothing past 999,999, the
// Chicago symbols nothing past 116, hexadecimal nothing past 65,535, and no
// format anything past 2,147,483,647.
package numfmt

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Max is the largest number Word writes.
const Max = 2147483647

// maxRepeat bounds formats that repeat a symbol as the number grows, such as
// letters (27 is "AA") and Roman numerals (5000 is "MMMMM"). Word sets no
// limit we know of; past this the number is written in decimal, so that a
// list starting at a huge number cannot make a huge label.
const maxRepeat = 32

// Format writes n in the named format. custom is the w:format attribute of
// a "custom" format, such as "001, 002, 003, ...". It reports false for a
// format it does not know, and then writes n in decimal.
//
// "bullet" is not a number format: the label of a bullet is its pattern as
// it stands, and the caller handles it. "none" writes nothing.
func Format(n int, format, custom string) (string, bool) {
	if n < 0 || n > Max {
		return "", true
	}
	dec := strconv.Itoa(n)
	switch format {
	case "", "decimal", "decimalHalfWidth", "bahtText", "dollarText":
		// Word writes the last two as decimal.
		return dec, true
	case "none":
		return "", true
	case "decimalZero":
		if n < 10 {
			return "0" + dec, true
		}
		return dec, true
	case "upperRoman":
		return roman(n, dec), true
	case "lowerRoman":
		return strings.ToLower(roman(n, dec)), true
	case "upperLetter":
		return letters(n, 'A', dec), true
	case "lowerLetter":
		return letters(n, 'a', dec), true
	case "ordinal":
		return dec + ordinalSuffix(n), true
	case "cardinalText":
		return capital(words(n, false)), true
	case "ordinalText":
		return capital(words(n, true)), true
	case "hex":
		if n > 65535 {
			return "", true
		}
		return strings.ToUpper(strconv.FormatInt(int64(n), 16)), true
	case "chicago":
		if n < 1 || n > 116 {
			return "", true
		}
		return strings.Repeat(string("*†‡§"[chicagoIndex[(n-1)%4]:chicagoIndex[(n-1)%4+1]]), (n-1)/4+1), true
	case "numberInDash":
		return "- " + dec + " -", true
	case "decimalFullWidth", "decimalFullWidth2":
		return shiftDigits(dec, '０'), true
	case "decimalEnclosedCircle":
		return enclosedCircle(n, 50, dec), true
	case "decimalEnclosedCircleChinese":
		return enclosedCircle(n, 10, dec), true
	case "decimalEnclosedFullstop":
		return enclosed(n, 0x2488, dec), true
	case "decimalEnclosedParen":
		return enclosed(n, 0x2474, dec), true
	case "ideographDigital":
		return digits(dec, "〇一二三四五六七八九"), true
	case "taiwaneseDigital":
		return digits(dec, "○一二三四五六七八九"), true
	case "chineseCounting", "taiwaneseCounting":
		// Word names this format "一, 十, 一○○": counted up to 99,
		// then written digit by digit ([MS-DOCX] 2.4).
		if n > 999999 {
			return "", true
		}
		if n >= 100 {
			return digits(dec, "○一二三四五六七八九"), true
		}
		return counting(n, true), true
	case "chineseCountingThousand", "taiwaneseCountingThousand":
		if n > 999999 {
			return "", true
		}
		return counting(n, true), true
	case "chineseLegalSimplified":
		return legal(n, "零壹贰叁肆伍陆柒捌玖", "拾佰仟万", dec), true
	case "ideographLegalTraditional":
		return legal(n, "零壹貳參肆伍陸柒捌玖", "拾佰仟萬", dec), true
	case "japaneseLegal":
		return legal(n, "零壱弐参四伍六七八九", "拾百阡萬", dec), true
	case "japaneseCounting":
		if n > 999999 {
			return "", true
		}
		return counting(n, false), true
	case "ideographTraditional":
		return cycle(n, "甲乙丙丁戊己庚辛壬癸", dec), true
	case "ideographZodiac":
		return cycle(n, "子丑寅卯辰巳午未申酉戌亥", dec), true
	case "aiueo":
		return cycle(n, "ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜｦﾝ", dec), true
	case "aiueoFullWidth":
		return cycle(n, "アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン", dec), true
	case "ganada":
		return cycle(n, "가나다라마바사아자차카타파하", dec), true
	case "chosung":
		return cycle(n, "ㄱㄴㄷㄹㅁㅂㅅㅇㅈㅊㅋㅌㅍㅎ", dec), true
	case "hebrew1":
		return hebrew(n, dec), true
	case "arabicAlpha":
		if n < 1 || n > 28 {
			return dec, true
		}
		return cycle(n, "أبتثجحخدذرزسشصضطظعغفقكلمنهوي", dec), true
	case "custom":
		if w := zeroWidth(custom); w > 0 {
			if len(dec) < w {
				return strings.Repeat("0", w-len(dec)) + dec, true
			}
			return dec, true
		}
	}
	return dec, false
}

var chicagoIndex = [...]int{0, 1, 4, 7, 9} // byte offsets of the four symbols in "*†‡§"

func roman(n int, dec string) string {
	if n < 1 || n/1000 > maxRepeat {
		return dec
	}
	var sb strings.Builder
	sb.WriteString(strings.Repeat("M", n/1000))
	n %= 1000
	for _, step := range []struct {
		v int
		s string
	}{{900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for n >= step.v {
			sb.WriteString(step.s)
			n -= step.v
		}
	}
	return sb.String()
}

// letters writes 1 as A, 26 as Z, 27 as AA, 28 as BB: Word repeats the
// letter rather than counting in base 26.
func letters(n int, a rune, dec string) string {
	if n < 1 || (n-1)/26+1 > maxRepeat {
		return dec
	}
	return strings.Repeat(string(a+rune((n-1)%26)), (n-1)/26+1)
}

// cycle writes 1 as the first symbol of the set, and starts again after
// the last one, as Word does for the kana and Hangul sequences.
func cycle(n int, set, dec string) string {
	if n < 1 {
		return dec
	}
	count := utf8.RuneCountInString(set)
	i := (n - 1) % count
	for _, r := range set {
		if i == 0 {
			return string(r)
		}
		i--
	}
	return dec
}

func ordinalSuffix(n int) string {
	switch {
	case n%100 >= 11 && n%100 <= 13:
		return "th"
	case n%10 == 1:
		return "st"
	case n%10 == 2:
		return "nd"
	case n%10 == 3:
		return "rd"
	}
	return "th"
}

var (
	ones = [...]string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
		"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	tens     = [...]string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
	ordinals = map[string]string{"one": "first", "two": "second", "three": "third", "five": "fifth", "eight": "eighth",
		"nine": "ninth", "twelve": "twelfth"}
)

// words writes n in English words, "twenty-one" or "twenty-first". Word
// writes nothing past 999,999.
func words(n int, ordinal bool) string {
	if n > 999999 {
		return ""
	}
	var parts []string
	if n >= 1000 {
		parts = append(parts, below1000(n/1000), "thousand")
		n %= 1000
	}
	if n > 0 || len(parts) == 0 {
		parts = append(parts, below1000(n))
	}
	s := strings.Join(parts, " ")
	if !ordinal {
		return s
	}
	// The last word becomes an ordinal: "twenty-one" to "twenty-first".
	cut := max(strings.LastIndexAny(s, " -")+1, 0)
	last := s[cut:]
	switch o, ok := ordinals[last]; {
	case ok:
		last = o
	case strings.HasSuffix(last, "y"):
		last = last[:len(last)-1] + "ieth"
	default:
		last += "th"
	}
	return s[:cut] + last
}

func below1000(n int) string {
	var parts []string
	if n >= 100 {
		parts = append(parts, ones[n/100], "hundred")
		n %= 100
		if n == 0 {
			return strings.Join(parts, " ")
		}
	}
	switch {
	case n < 20:
		parts = append(parts, ones[n])
	case n%10 == 0:
		parts = append(parts, tens[n/10])
	default:
		parts = append(parts, tens[n/10]+"-"+ones[n%10])
	}
	return strings.Join(parts, " ")
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func shiftDigits(dec string, zero rune) string {
	var sb strings.Builder
	for _, r := range dec {
		sb.WriteRune(zero + r - '0')
	}
	return sb.String()
}

// enclosedCircle writes ① to ⑳, then ㉑ to ㊿, up to limit, and decimal
// after that.
func enclosedCircle(n, limit int, dec string) string {
	switch {
	case n == 0:
		return "⓪"
	case n > limit:
		return dec
	case n <= 20:
		return string(rune(0x2460 + n - 1))
	case n <= 35:
		return string(rune(0x3251 + n - 21))
	}
	return string(rune(0x32B1 + n - 36))
}

// enclosed writes 1 to 20 with the twenty characters from first, and
// decimal after that.
func enclosed(n int, first rune, dec string) string {
	if n < 1 || n > 20 {
		return dec
	}
	return string(first + rune(n-1))
}

const cjkDigits = "〇一二三四五六七八九"

func cjkDigit(d int) string {
	r := []rune(cjkDigits)
	return string(r[d])
}

// digits writes each decimal digit with the character for it in set.
func digits(dec, set string) string {
	glyphs := []rune(set)
	var sb strings.Builder
	for _, r := range dec {
		sb.WriteRune(glyphs[r-'0'])
	}
	return sb.String()
}

// legal writes n in the financial characters that cannot be altered by
// adding a stroke: 壹拾 for 10 where everyday writing has 十. Every digit
// is written before its unit, and a gap is written once as zero.
func legal(n int, set, units, dec string) string {
	if n < 1 || n > 99999999 {
		if n == 0 {
			return string([]rune(set)[0])
		}
		return dec
	}
	d, u := []rune(set), []rune(units)
	var sb strings.Builder
	group := func(n int, inner bool) {
		gap := inner
		for i, v := range []int{1000, 100, 10, 1} {
			x := n / v % 10
			if x == 0 {
				gap = gap || sb.Len() > 0
				continue
			}
			if gap && sb.Len() > 0 {
				sb.WriteRune(d[0])
			}
			gap = false
			sb.WriteRune(d[x])
			if v > 1 {
				sb.WriteRune(u[2-i])
			}
		}
	}
	if n >= 10000 {
		group(n/10000, false)
		sb.WriteRune(u[3])
		group(n%10000, true)
	} else {
		group(n, false)
	}
	return sb.String()
}

// counting writes n with the ideographic units ten, hundred, thousand and
// ten thousand. Chinese writes zero (零) for a gap, and one (一) before a
// unit except at the start of a number below twenty; Japanese writes
// neither: 105 is 一百零五 in Chinese and 百五 in Japanese.
func counting(n int, chinese bool) string {
	if n == 0 {
		return "〇"
	}
	if n >= 10000 {
		hi := counting(n/10000, chinese) + "万"
		lo := n % 10000
		if lo == 0 {
			return hi
		}
		if chinese && lo < 1000 {
			hi += "零"
		}
		return hi + counting1e4(lo, chinese, true)
	}
	return counting1e4(n, chinese, false)
}

func counting1e4(n int, chinese, inner bool) string {
	units := []string{"千", "百", "十", ""}
	values := []int{1000, 100, 10, 1}
	var sb strings.Builder
	gap := false
	for i, v := range values {
		d := n / v % 10
		if d == 0 {
			gap = sb.Len() > 0 || inner
			continue
		}
		if gap && chinese && sb.Len() > 0 {
			sb.WriteString("零")
		}
		gap = false
		switch {
		case units[i] == "":
			sb.WriteString(cjkDigit(d))
		case d == 1 && !chinese:
			sb.WriteString(units[i])
		case d == 1 && chinese && v == 10 && sb.Len() == 0 && !inner:
			sb.WriteString(units[i]) // 十一, not 一十一
		default:
			sb.WriteString(cjkDigit(d) + units[i])
		}
	}
	return sb.String()
}

// hebrew writes n in Hebrew letters that stand for numbers, up to 999. 15
// and 16 are written 9+6 and 9+7, as they always are.
func hebrew(n int, dec string) string {
	if n < 1 || n > 999 {
		return dec
	}
	hundreds := []string{"", "ק", "ר", "ש", "ת", "תק", "תר", "תש", "תת", "תתק"}
	tensH := []string{"", "י", "כ", "ל", "מ", "נ", "ס", "ע", "פ", "צ"}
	onesH := []string{"", "א", "ב", "ג", "ד", "ה", "ו", "ז", "ח", "ט"}
	s := hundreds[n/100]
	switch r := n % 100; r {
	case 15:
		s += "טו"
	case 16:
		s += "טז"
	default:
		s += tensH[r/10] + onesH[r%10]
	}
	return s
}

// zeroWidth reads a custom format such as "001, 002, 003, ..." and returns
// the width its numbers are padded to with zeros, or 0 for any other kind
// of custom format.
func zeroWidth(custom string) int {
	first, _, _ := strings.Cut(custom, ",")
	first = strings.TrimSpace(first)
	if len(first) < 2 || len(first) > 8 || strings.Trim(first, "0") != "1" || !strings.HasSuffix(first, "1") {
		return 0
	}
	return len(first)
}
