package logger

import (
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	sugar *zap.SugaredLogger
)

const (
	timeFormat = "[01-02|15:04:05.000]"
)

func InitializeLogger(console bool, file, level string, maxFileSize int) {
	if sugar != nil {
		sugar.Infof("Logger already initialized, skipping re-initialization.")
		return
	}

	sugar = createSugaredLogger(console, file, level, maxFileSize)
}

func createSugaredLogger(console bool, file, levelName string, maxFileSize int) *zap.SugaredLogger {
	atom := zap.NewAtomicLevel()

	var cores []zapcore.Core
	if console {
		cores = append(cores, createConsoleLoggerCore(atom))
	}

	if len(file) > 0 {
		cores = append(cores, createFileLoggerCore(file, maxFileSize, atom))
	}

	core := zapcore.NewTee(cores...)

	logger := zap.New(
		core,
		zap.AddStacktrace(zap.ErrorLevel),
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	)

	sug := logger.Sugar()

	level, err := zapcore.ParseLevel(levelName)
	if err != nil {
		sug.Errorf("Wrong level %s", levelName)
	}

	atom.SetLevel(level)
	sug.Infof("Set log level to %s", level)

	return sug
}

func SyncFileLogger() {
	sugar.Infof("Syncing file logger.")
	err := sugar.Sync()
	if err != nil {
		sugar.Infof("Failed to sync logger: %v", err)
	}
}

func createFileLoggerCore(file string, maxFileSize int, atom zap.AtomicLevel) zapcore.Core {
	w := zapcore.AddSync(&lumberjack.Logger{
		Filename: file,
		MaxSize:  maxFileSize,
	})

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeLevel = fileLevelEncoder
	encoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout(timeFormat)

	return zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		w,
		atom,
	)
}

type noSyncWriterWrapper struct {
	io.Writer
}

func (n noSyncWriterWrapper) Sync() error {
	return nil
}

func createConsoleLoggerCore(atom zap.AtomicLevel) zapcore.Core {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeLevel = consoleColorLevelEncoder
	encoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout(timeFormat)

	return zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		noSyncWriterWrapper{os.Stdout},
		atom,
	)
}

func consoleColorLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	s, ok := levelToCapitalColorString[l]
	if !ok {
		s = unknownLevelColor.Wrap(l.CapitalString())
	}

	enc.AppendString(s)
}

func fileLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(l.CapitalString())
}

func Warn(msg string, args ...interface{}) {
	sugar.Warnf(msg, args...)
}

func Error(msg string, args ...interface{}) {
	sugar.Errorf(msg, args...)
}

func Info(msg string, args ...interface{}) {
	sugar.Infof(msg, args...)
}

func Debug(msg string, args ...interface{}) {
	sugar.Debugf(msg, args...)
}

func Fatal(msg string, args ...interface{}) {
	SyncFileLogger()
	sugar.Fatalf(msg, args...)
}
