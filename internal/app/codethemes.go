package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// codeColors are the colors of code in a theme for one appearance: its
// text's and its tokens'. Kinds it leaves out show as its text. The
// background is the window's, as Xcode's themes leave it to the editor.
type codeColors struct {
	text   string
	tokens map[editor.TokenKind]string
}

// codeThemeChoice is a theme the Settings window offers, its colors for
// the light appearance and the dark; none for the default's, GitHub's.
type codeThemeChoice struct {
	name         string
	light, dark  *codeColors
	boldKeywords bool
}

// codeThemeChoices are the themes of code, the first the default.
var codeThemeChoices = []codeThemeChoice{
	{name: "GitHub"},
	{name: "Xcode", boldKeywords: true, light: &codeColors{"#000000d9", map[editor.TokenKind]string{
		editor.TokenKeyword:   "#9b2393",
		editor.TokenType:      "#3900a0",
		editor.TokenFunction:  "#6c36a9",
		editor.TokenProperty:  "#6c36a9",
		editor.TokenConstant:  "#6c36a9",
		editor.TokenNumber:    "#1c00cf",
		editor.TokenString:    "#c41a16",
		editor.TokenEscape:    "#1c00cf",
		editor.TokenRegexp:    "#c41a16",
		editor.TokenComment:   "#5d6c79",
		editor.TokenTag:       "#9b2393",
		editor.TokenAttribute: "#815f03",
		editor.TokenMeta:      "#643820",
		editor.TokenHeading:   "#0f68a0",
		editor.TokenLink:      "#0e0eff",
		editor.TokenCode:      "#c41a16",
		editor.TokenInserted:  "#326d74",
		editor.TokenDeleted:   "#c41a16",
		editor.TokenInvalid:   "#c41a16",
	}}, dark: &codeColors{"#ffffffd9", map[editor.TokenKind]string{
		editor.TokenKeyword:   "#ff7ab2",
		editor.TokenType:      "#dabaff",
		editor.TokenFunction:  "#b281eb",
		editor.TokenProperty:  "#b281eb",
		editor.TokenConstant:  "#b281eb",
		editor.TokenNumber:    "#d9c97c",
		editor.TokenString:    "#ff8170",
		editor.TokenEscape:    "#d9c97c",
		editor.TokenRegexp:    "#ff8170",
		editor.TokenComment:   "#7f8c98",
		editor.TokenTag:       "#ff7ab2",
		editor.TokenAttribute: "#cc9768",
		editor.TokenMeta:      "#ffa14f",
		editor.TokenHeading:   "#6bdfff",
		editor.TokenLink:      "#6699ff",
		editor.TokenCode:      "#ff8170",
		editor.TokenInserted:  "#67b7a4",
		editor.TokenDeleted:   "#ff8170",
		editor.TokenInvalid:   "#ff8170",
	}}},
	{name: "Solarized", light: solarized("#657b83", "#93a1a1"), dark: solarized("#839496", "#586e75")},
	{name: "One", light: &codeColors{"#383a42", map[editor.TokenKind]string{
		editor.TokenKeyword:   "#a626a4",
		editor.TokenType:      "#c18401",
		editor.TokenFunction:  "#4078f2",
		editor.TokenProperty:  "#e45649",
		editor.TokenConstant:  "#986801",
		editor.TokenNumber:    "#986801",
		editor.TokenString:    "#50a14f",
		editor.TokenEscape:    "#0184bc",
		editor.TokenRegexp:    "#0184bc",
		editor.TokenComment:   "#a0a1a7",
		editor.TokenOperator:  "#0184bc",
		editor.TokenTag:       "#e45649",
		editor.TokenAttribute: "#986801",
		editor.TokenMeta:      "#4078f2",
		editor.TokenHeading:   "#e45649",
		editor.TokenLink:      "#4078f2",
		editor.TokenCode:      "#50a14f",
		editor.TokenInserted:  "#50a14f",
		editor.TokenDeleted:   "#e45649",
		editor.TokenInvalid:   "#ca1243",
	}}, dark: &codeColors{"#abb2bf", map[editor.TokenKind]string{
		editor.TokenKeyword:   "#c678dd",
		editor.TokenType:      "#e5c07b",
		editor.TokenFunction:  "#61afef",
		editor.TokenProperty:  "#e06c75",
		editor.TokenConstant:  "#d19a66",
		editor.TokenNumber:    "#d19a66",
		editor.TokenString:    "#98c379",
		editor.TokenEscape:    "#56b6c2",
		editor.TokenRegexp:    "#56b6c2",
		editor.TokenComment:   "#7f848e",
		editor.TokenOperator:  "#56b6c2",
		editor.TokenTag:       "#e06c75",
		editor.TokenAttribute: "#d19a66",
		editor.TokenMeta:      "#61afef",
		editor.TokenHeading:   "#e06c75",
		editor.TokenLink:      "#61afef",
		editor.TokenCode:      "#98c379",
		editor.TokenInserted:  "#98c379",
		editor.TokenDeleted:   "#e06c75",
		editor.TokenInvalid:   "#be5046",
	}}},
}

// solarized are Solarized's accents, as its Vim theme gives them, over
// the text and comments of an appearance.
func solarized(text, comment string) *codeColors {
	const (
		yellow = "#b58900"
		orange = "#cb4b16"
		red    = "#dc322f"
		violet = "#6c71c4"
		blue   = "#268bd2"
		cyan   = "#2aa198"
		green  = "#859900"
	)
	return &codeColors{text, map[editor.TokenKind]string{
		editor.TokenKeyword:   green,
		editor.TokenOperator:  green,
		editor.TokenType:      yellow,
		editor.TokenFunction:  blue,
		editor.TokenProperty:  blue,
		editor.TokenConstant:  cyan,
		editor.TokenNumber:    cyan,
		editor.TokenString:    cyan,
		editor.TokenEscape:    red,
		editor.TokenRegexp:    red,
		editor.TokenComment:   comment,
		editor.TokenTag:       blue,
		editor.TokenAttribute: yellow,
		editor.TokenMeta:      orange,
		editor.TokenHeading:   blue,
		editor.TokenLink:      violet,
		editor.TokenCode:      cyan,
		editor.TokenInserted:  green,
		editor.TokenDeleted:   red,
		editor.TokenInvalid:   red,
	}}
}

// codeThemes are the choices' themes, light and dark, made once so that
// a pass hands the editors the same theme as the one before.
var codeThemes = func() map[string][2]*editor.Theme {
	themes := map[string][2]*editor.Theme{}
	for _, ch := range codeThemeChoices {
		themes[ch.name] = [2]*editor.Theme{ch.theme(ch.light), ch.theme(ch.dark)}
	}
	return themes
}()

// theme is the choice's theme for colors, the default's where it has
// none, with comments in italics.
func (ch codeThemeChoice) theme(colors *codeColors) *editor.Theme {
	ct := &editor.Theme{} // the default's colors fill those left zero
	ct.Tokens[editor.TokenComment].Italic = true
	ct.Tokens[editor.TokenEmphasis].Italic = true
	ct.Tokens[editor.TokenHeading].Bold = true
	ct.Tokens[editor.TokenStrong].Bold = true
	ct.Tokens[editor.TokenKeyword].Bold = ch.boldKeywords
	if colors == nil {
		return ct
	}
	ct.Text = ui.Hex(colors.text)
	for k := range ct.Tokens {
		ct.Tokens[k].Color = ct.Text
	}
	for k, hex := range colors.tokens {
		ct.Tokens[k].Color = ui.Hex(hex)
	}
	return ct
}

// codeTheme is the theme of code the user chose, for the window's
// appearance.
func codeTheme(name string, dark bool) *editor.Theme {
	themes, ok := codeThemes[name]
	if !ok {
		themes = codeThemes[codeThemeChoices[0].name]
	}
	if dark {
		return themes[1]
	}
	return themes[0]
}
