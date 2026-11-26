package numfmt

import "testing"

func TestFormat(t *testing.T) {
	cases := []struct {
		n      int
		format string
		want   string
	}{
		{0, "decimal", "0"}, {42, "decimal", "42"}, {7, "", "7"},
		{7, "decimalZero", "07"}, {10, "decimalZero", "10"},
		{4, "upperRoman", "IV"}, {1994, "upperRoman", "MCMXCIV"}, {3999, "lowerRoman", "mmmcmxcix"}, {4001, "upperRoman", "MMMMI"},
		{0, "upperRoman", "0"}, {40000, "upperRoman", "40000"},
		{1, "upperLetter", "A"}, {26, "lowerLetter", "z"}, {27, "lowerLetter", "aa"}, {28, "upperLetter", "BB"}, {53, "upperLetter", "AAA"},
		{0, "lowerLetter", "0"}, {26*32 + 1, "lowerLetter", "833"},
		{1, "ordinal", "1st"}, {2, "ordinal", "2nd"}, {3, "ordinal", "3rd"}, {4, "ordinal", "4th"},
		{11, "ordinal", "11th"}, {12, "ordinal", "12th"}, {13, "ordinal", "13th"}, {21, "ordinal", "21st"}, {112, "ordinal", "112th"},
		{1, "cardinalText", "One"}, {21, "cardinalText", "Twenty-one"}, {100, "cardinalText", "One hundred"},
		{1999, "cardinalText", "One thousand nine hundred ninety-nine"}, {999999, "cardinalText", "Nine hundred ninety-nine thousand nine hundred ninety-nine"},
		{1000000, "cardinalText", ""}, {0, "cardinalText", "Zero"},
		{1, "ordinalText", "First"}, {12, "ordinalText", "Twelfth"}, {20, "ordinalText", "Twentieth"}, {21, "ordinalText", "Twenty-first"},
		{112, "ordinalText", "One hundred twelfth"}, {100, "ordinalText", "One hundredth"}, {45, "ordinalText", "Forty-fifth"},
		{255, "hex", "FF"}, {65536, "hex", ""},
		{1, "chicago", "*"}, {2, "chicago", "†"}, {3, "chicago", "‡"}, {4, "chicago", "§"}, {5, "chicago", "**"}, {6, "chicago", "††"},
		{116, "chicago", "§§§§§§§§§§§§§§§§§§§§§§§§§§§§§"}, {117, "chicago", ""},
		{3, "numberInDash", "- 3 -"},
		{12, "decimalFullWidth", "１２"},
		{1, "decimalEnclosedCircle", "①"}, {20, "decimalEnclosedCircle", "⑳"}, {21, "decimalEnclosedCircle", "㉑"}, {50, "decimalEnclosedCircle", "㊿"}, {51, "decimalEnclosedCircle", "51"},
		{10, "decimalEnclosedCircleChinese", "⑩"}, {11, "decimalEnclosedCircleChinese", "11"},
		{3, "decimalEnclosedFullstop", "⒊"}, {3, "decimalEnclosedParen", "⑶"},
		{10, "ideographDigital", "一〇"}, {100, "taiwaneseDigital", "一○○"},
		{10, "chineseCounting", "十"}, {11, "chineseCounting", "十一"}, {21, "chineseCounting", "二十一"}, {100, "chineseCounting", "一○○"}, {105, "taiwaneseCounting", "一○五"},
		{100, "chineseCountingThousand", "一百"}, {105, "chineseCountingThousand", "一百零五"},
		{110, "chineseCountingThousand", "一百一十"}, {10001, "chineseCountingThousand", "一万零一"}, {15000, "chineseCountingThousand", "一万五千"},
		{1, "chineseLegalSimplified", "壹"}, {3, "chineseLegalSimplified", "叁"}, {10, "chineseLegalSimplified", "壹拾"}, {105, "chineseLegalSimplified", "壹佰零伍"},
		{1010, "chineseLegalSimplified", "壹仟零壹拾"}, {20000, "chineseLegalSimplified", "贰万"}, {10001, "chineseLegalSimplified", "壹万零壹"},
		{2, "ideographLegalTraditional", "貳"}, {2, "japaneseLegal", "弐"},
		{105, "japaneseCounting", "百五"}, {1000, "japaneseCounting", "千"}, {21, "japaneseCounting", "二十一"},
		{1, "ideographTraditional", "甲"}, {11, "ideographTraditional", "甲"}, {12, "ideographZodiac", "亥"},
		{1, "aiueo", "ｱ"}, {2, "aiueoFullWidth", "イ"}, {1, "ganada", "가"}, {15, "ganada", "가"}, {14, "chosung", "ㅎ"},
		{15, "hebrew1", "טו"}, {16, "hebrew1", "טז"}, {1, "hebrew1", "א"}, {120, "hebrew1", "קכ"},
		{1, "arabicAlpha", "أ"}, {28, "arabicAlpha", "ي"},
		{5, "none", ""},
		{5, "bahtText", "5"},
		{Max + 1, "decimal", ""},
	}
	for _, c := range cases {
		got, ok := Format(c.n, c.format, "")
		if got != c.want || !ok {
			t.Errorf("Format(%d, %q) = %q, %v; want %q", c.n, c.format, got, ok, c.want)
		}
	}
}

func TestCustomAndUnknown(t *testing.T) {
	if got, ok := Format(8, "custom", "001, 002, 003, ..."); got != "008" || !ok {
		t.Errorf("custom = %q, %v", got, ok)
	}
	if got, ok := Format(12345, "custom", "0001, 0002, 0003, ..."); got != "12345" || !ok {
		t.Errorf("custom wider = %q, %v", got, ok)
	}
	for _, f := range []string{"custom", "thaiNumbers", "russianLower", "no such format"} {
		if got, ok := Format(12, f, "а, б, в, ..."); got != "12" || ok {
			t.Errorf("%s = %q, %v; want decimal and false", f, got, ok)
		}
	}
}

func FuzzFormat(f *testing.F) {
	for _, s := range []string{"decimal", "upperRoman", "lowerLetter", "cardinalText", "ordinalText", "chineseCounting", "japaneseCounting", "hebrew1", "chicago", "custom"} {
		f.Add(12345, s, "001, 002")
	}
	f.Fuzz(func(t *testing.T, n int, format, custom string) {
		s, _ := Format(n, format, custom)
		if len(s) > 400 {
			t.Fatalf("Format(%d, %q) is %d bytes", n, format, len(s))
		}
	})
}
