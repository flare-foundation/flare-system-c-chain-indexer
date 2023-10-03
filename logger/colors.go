package logger

import "go.uber.org/zap/zapcore"

type Color string

// Colors taken from Avalanche logger colors
// (avalanchego/utils/logging/color.go)
const (
	Black       Color = "\033[0;30m"
	DarkGray    Color = "\033[1;30m"
	Red         Color = "\033[0;31m"
	LightRed    Color = "\033[1;31m"
	Green       Color = "\033[0;32m"
	LightGreen  Color = "\033[1;32m"
	Orange      Color = "\033[0;33m"
	Yellow      Color = "\033[1;33m"
	Blue        Color = "\033[0;34m"
	LightBlue   Color = "\033[1;34m"
	Purple      Color = "\033[0;35m"
	LightPurple Color = "\033[1;35m"
	Cyan        Color = "\033[0;36m"
	LightCyan   Color = "\033[1;36m"
	LightGray   Color = "\033[0;37m"
	White       Color = "\033[1;37m"

	Reset   Color = "\033[0;0m"
	Bold    Color = "\033[;1m"
	Reverse Color = "\033[;7m"
)

var (
	levelToColor = map[zapcore.Level]Color{
		zapcore.FatalLevel: Red,
		zapcore.ErrorLevel: Orange,
		zapcore.WarnLevel:  Yellow,
		zapcore.InfoLevel:  Reset,
		zapcore.DebugLevel: LightBlue,
	}

	levelToCapitalColorString = make(map[zapcore.Level]string, len(levelToColor))
	unknownLevelColor         = Reset
)

func init() {
	for level, color := range levelToColor {
		levelToCapitalColorString[level] = color.Wrap(level.CapitalString())
	}
}

func (lc Color) Wrap(text string) string {
	return string(lc) + text + string(Reset)
}
