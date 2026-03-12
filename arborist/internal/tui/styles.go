package tui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// =============================================================================
// k9s Stock Color Palette (hex values from the stock k9s skin)
// =============================================================================
// See colors.md for the full k9s color reference.
// All colors use hex strings for true-color precision and direct k9s fidelity.

var (
	// Core k9s palette
	ColorBlack         = lipgloss.Color("#000000")
	ColorWhite         = lipgloss.Color("#ffffff")
	ColorAqua          = lipgloss.Color("#00ffff") // titles, section headers
	ColorDodgerBlue    = lipgloss.Color("#1e90ff") // menu keys, unfocused borders
	ColorLightSkyBlue  = lipgloss.Color("#87ceeb") // table cell text, focused borders
	ColorOrange        = lipgloss.Color("#ffa500") // logo
	ColorPapayaWhip    = lipgloss.Color("#ffefd5") // record counts
	ColorSeaGreen      = lipgloss.Color("#2e8b57") // filter mode
	ColorCadetBlue     = lipgloss.Color("#5f9ea0") // body fg, secondary/inactive text
	ColorOrangeRed     = lipgloss.Color("#ff4500") // errors, failed status
	ColorDarkOrange    = lipgloss.Color("#ff8c00") // pending, warnings
	ColorLightSlateGray = lipgloss.Color("#778899") // completed, muted/N/A
	ColorMediumPurple  = lipgloss.Color("#9370db") // PodCliqueScalingGroup, deleting
	ColorDarkTurquoise = lipgloss.Color("#00ced1") // PodClique
	ColorGreenYellow   = lipgloss.Color("#adff2f") // Pod, modified
	ColorRed           = lipgloss.Color("#ff0000") // error border
	ColorPink       = lipgloss.Color("13") // ANSI color 13 (bright magenta) — matches k9s "fuchsia" which uses tcell ANSI palette
	ColorPaleGreen     = lipgloss.Color("#98fb98") // marked rows (future)
)

// Semantic aliases — map component roles to palette colors.
// Change these to reskin without touching style definitions.
var (
	// Table
	ColorTableHeaderFg = ColorWhite        // k9s: white headers
	ColorTableCellFg   = lipgloss.Color("#76B900") // soft green row text
	ColorCursorFg      = ColorBlack               // black text on cursor
	ColorCursorBg      = lipgloss.Color("#76B900") // soft green cursor bg
	//
	// NOTE on ColorTableCellFg: k9s renders normal row text in lightskyblue, but
	// bubbles/table cannot set a Cell foreground without breaking the Selected row
	// highlight. Each Cell.Render() emits an ANSI reset that kills the Selected
	// style's background between cells. Cell text therefore uses the terminal's
	// default foreground (typically white — visually close to lightskyblue).

	// Borders
	ColorBorderFocused   = lipgloss.Color("#76B900") // soft green focused border
	ColorBorderUnfocused = lipgloss.Color("#4a7500") // deeper nvidia green unfocused border
	ColorBorderError     = ColorRed          // k9s: red error border

	// Section headers (title bar inside frames)
	ColorSectionTitle = ColorAqua       // k9s: aqua resource name
	ColorSectionCount = ColorPapayaWhip // k9s: papayawhip record count
	ColorSectionMuted = ColorCadetBlue  // k9s: cadetblue for inactive labels

	// Logo
	ColorLogo = ColorOrange // k9s: orange logo

	// Menu bar
	ColorMenuKey    = ColorDodgerBlue // k9s: dodgerblue key mnemonics
	ColorMenuAction = ColorWhite      // k9s: white (faint) descriptions

	// Filter
	ColorFilter = ColorSeaGreen // k9s: seagreen filter text

	// Breadcrumb separator / secondary text
	ColorSecondary = ColorCadetBlue // k9s: cadetblue body fg
)

// =============================================================================
// ASCII Art Logo (3D figlet-style, displayed in header like k9s)
// =============================================================================

// ArboristASCII is a compact Unicode half-block rendering of "Arborist"
// using the smblock figlet font. 4 lines tall, ~23 chars wide. Displayed
// in the top-right of the header, styled with ColorLogo (orange).
const ArboristASCII = "" +
	"▞▀▖   ▌        ▗    ▐\n" +
	"▙▄▌▙▀▖▛▀▖▞▀▖▙▀▖▄ ▞▀▘▜▀\n" +
	"▌ ▌▌  ▌ ▌▌ ▌▌  ▐ ▝▀▖▐ ▖\n" +
	"▘ ▘▘  ▀▀ ▝▀ ▘  ▀▘▀▀  ▀"

// =============================================================================
// Logo & Header Styles
// =============================================================================

var (
	LogoStyle = lipgloss.NewStyle().
			Foreground(ColorLogo).
			Bold(true)

	HeaderInfoStyle = lipgloss.NewStyle().
			Foreground(ColorCadetBlue)

	// HeaderLabelStyle is for "Context:", "Cluster:", "Lens:" labels (k9s: orange).
	HeaderLabelStyle = lipgloss.NewStyle().
				Foreground(ColorOrange)

	// HeaderValueStyle is for context/cluster/view values (k9s: white bold).
	HeaderValueStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Bold(true)
)

// =============================================================================
// Section Header Styles (e.g. "Resources [3]", "Events [5]")
// =============================================================================

var (
	SectionHeaderActiveStyle = lipgloss.NewStyle().
					Foreground(ColorTableCellFg).
					Bold(true)

	SectionHeaderInactiveStyle = lipgloss.NewStyle().
					Foreground(ColorTableCellFg).
					Bold(true)

	SectionCountStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Bold(true)
)

// =============================================================================
// Box-Drawing Border Styles
// =============================================================================

var (
	// Focused pane border (lightskyblue)
	ActiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorderFocused)

	// Unfocused pane border (dodgerblue)
	InactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorderUnfocused)

	// Error state border (red)
	ErrorBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorderError)
)

// =============================================================================
// Menu Bar Styles (bottom shortcut line, k9s-style <key>Action)
// =============================================================================

var (
	MenuKeyStyle = lipgloss.NewStyle().
			Foreground(ColorMenuKey).
			Bold(true)

	MenuActionStyle = lipgloss.NewStyle().
			Foreground(ColorMenuAction).
			Faint(true)
)

// =============================================================================
// Resource Type Colors
// =============================================================================

var TypeColors = map[string]lipgloss.Color{
	"PodCliqueSet":          ColorDodgerBlue,    // dodgerblue
	"(PodCliqueSet replica)":   lipgloss.Color("#76B900"),  // soft green
	"PodCliqueScalingGroup": ColorMediumPurple,   // mediumpurple
	"PodClique":             ColorDarkTurquoise,  // darkturquoise
	"Pod":                   ColorGreenYellow,    // greenyellow
}

// =============================================================================
// Event Type Colors (k9s uses per-row event state coloring)
// =============================================================================

var EventTypeColors = map[string]lipgloss.Color{
	"Normal":  lipgloss.Color("#76B900"), // soft green
	"Warning": ColorDarkOrange,   // k9s: pending/warning = darkorange
	"Error":   ColorOrangeRed,    // k9s: error = orangered
}

// =============================================================================
// Status Colors (for Ready column, Phase, etc.)
// =============================================================================
// Follows k9s per-resource colorer pattern.

var PodStatusColors = map[string]lipgloss.Color{
	"Running":   lipgloss.Color("#76B900"),  // soft green
	"Healthy":   lipgloss.Color("#76B900"),  // soft green
	"Scaling":   ColorDarkOrange,    // k9s: pending = darkorange
	"Pending":   ColorDarkOrange,    // k9s: pending = darkorange
	"Failed":    ColorOrangeRed,     // k9s: error = orangered
	"Unhealthy": ColorOrangeRed,     // k9s: error = orangered
	"Completed": ColorLightSlateGray, // k9s: completed = lightslategray
	"Succeeded": ColorLightSlateGray, // k9s: completed = lightslategray
}

// =============================================================================
// Filter Bar Style
// =============================================================================

var FilterBarStyle = lipgloss.NewStyle().
	Foreground(ColorFilter)

// =============================================================================
// Command Bar Style (vim-style ":" command mode)
// =============================================================================

var CommandBarStyle = lipgloss.NewStyle().
	Foreground(ColorAqua)

// =============================================================================
// Autocomplete Suggestion Style (inline ghost text for textinput completions)
// =============================================================================

var AutocompleteSuggestionStyle = lipgloss.NewStyle().
	Foreground(ColorLightSlateGray)

// =============================================================================
// Topology Display Styles
// =============================================================================

var (
	// Inherited topology values (parenthesized) — cadetblue (muted)
	TopologyInheritedStyle = lipgloss.NewStyle().
				Foreground(ColorCadetBlue)

	// Explicit topology values — white (prominent)
	TopologyExplicitStyle = lipgloss.NewStyle().
				Foreground(ColorWhite)

	// "N/A" topology values — lightslategray (dimmed)
	TopologyNAStyle = lipgloss.NewStyle().
			Foreground(ColorLightSlateGray)

)

// =============================================================================
// Breadcrumb Styles
// =============================================================================

var BreadcrumbSeparator = lipgloss.NewStyle().
	Foreground(ColorPink).
	SetString(" > ")

var BreadcrumbStyles = map[string]lipgloss.Style{
	"Forest":                lipgloss.NewStyle().Foreground(ColorTableCellFg),
	"PodCliqueSet":          lipgloss.NewStyle().Foreground(ColorPink),
	"(PodCliqueSet replica)":   lipgloss.NewStyle().Foreground(ColorPink),
	"PodCliqueScalingGroup": lipgloss.NewStyle().Foreground(ColorPink),
	"PodClique":             lipgloss.NewStyle().Foreground(ColorPink),
	"Pod":                   lipgloss.NewStyle().Foreground(ColorPink),
}

// =============================================================================
// Footnote Styles (e.g. "¹ GPU: Grove/Other/Total" below topology tables)
// =============================================================================

var FootnoteStyle = lipgloss.NewStyle().
	Foreground(ColorCadetBlue).
	Faint(true).
	PaddingLeft(1)

// =============================================================================
// Error Log Styles
// =============================================================================

var (
	// ErrorLogTimestampStyle — muted (cadetblue) for the timestamp
	ErrorLogTimestampStyle = lipgloss.NewStyle().
				Foreground(ColorCadetBlue)

	// ErrorLogMessageStyle — orange-red for the error text
	ErrorLogMessageStyle = lipgloss.NewStyle().
				Foreground(ColorOrangeRed)
)

// =============================================================================
// YAML Search Highlight Style
// =============================================================================

var YAMLSearchHighlightStyle = lipgloss.NewStyle().
	Background(ColorDarkOrange).
	Foreground(ColorBlack).
	Bold(true)

// =============================================================================
// Shared Table Styles
// =============================================================================

// TableFgStyle wraps an entire table View() to set the default foreground color
// for cell text. We cannot set Cell.Foreground directly because each
// Cell.Render() emits an ANSI reset that breaks the Selected row's background.
// Wrapping the whole view inherits the foreground into unstyled cells while
// Selected (which sets its own Foreground/Background) overrides it cleanly.
var TableFgStyle = lipgloss.NewStyle().Foreground(ColorTableCellFg)

// ArboristTableStyles returns the standard k9s-inspired table styles used
// across all arborist table views. Call once per table at creation time.
// The Selected style intentionally omits Padding — cells already have their
// own Padding(0,1), and Selected wraps the entire joined row. Adding padding
// here would shift the selected row right relative to other rows.
// Use ArboristTableStylesWithWidth to set Selected.Width so the highlight
// background spans the full row.
func ArboristTableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Bold(false).
		Foreground(ColorTableHeaderFg).
		Padding(0, 1)
	// Cell intentionally has no Foreground — see TableFgStyle.
	// Setting Foreground here causes each Cell.Render() to emit an ANSI reset
	// that kills the Selected style's background between cells.
	s.Cell = s.Cell.
		Padding(0, 1)
	s.Selected = s.Selected.
		Foreground(ColorCursorFg).
		Background(ColorCursorBg).
		Bold(true)
	return s
}

// ArboristTableStylesWithWidth returns table styles with the Selected style's
// Width set so the cursor highlight background spans the full row.
// Call this after determining the table's content width (typically m.width - 2
// for the frame content area inside the border).
func ArboristTableStylesWithWidth(rowWidth int) table.Styles {
	s := ArboristTableStyles()
	s.Selected = s.Selected.Width(rowWidth)
	return s
}
