package logger

import (
	"errors"
	"flare-ftso-indexer/config"
	"log"
	"os"
	"syscall"

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

func init() {
	sugar = createSugaredLogger(DefaultLoggerConfig())

	config.GlobalConfigCallback.AddCallback(func(config config.GlobalConfig) {
		sugar = createSugaredLogger(config.LoggerConfig())
	})
}

func createSugaredLogger(config config.LoggerConfig) *zap.SugaredLogger {
	atom := zap.NewAtomicLevel()

	var cores []zapcore.Core
	if config.Console {
		cores = append(cores, createConsoleLoggerCore(config, atom))
	}

	if len(config.File) > 0 {
		cores = append(cores, createFileLoggerCore(config, atom))
	}

	core := zapcore.NewTee(cores...)

	logger := zap.New(
		core,
		zap.AddStacktrace(zap.ErrorLevel),
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	)

	defer func() {
		err := logger.Sync()
		if err != nil && !errors.Is(err, syscall.ENOTTY) && !errors.Is(err, syscall.EBADF) {
			log.Print("Failed to sync logger", err)
		}
	}()

	sug := logger.Sugar()

	level, err := zapcore.ParseLevel(config.Level)
	if err != nil {
		sug.Errorf("Wrong level %s", config.Level)
	}

	atom.SetLevel(level)

	return sug
}

func createFileLoggerCore(config config.LoggerConfig, atom zap.AtomicLevel) zapcore.Core {
	w := zapcore.AddSync(&lumberjack.Logger{
		Filename: config.File,
		MaxSize:  config.MaxFileSize,
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

func createConsoleLoggerCore(config config.LoggerConfig, atom zap.AtomicLevel) zapcore.Core {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeLevel = consoleColorLevelEncoder
	encoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout(timeFormat)

	return zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.AddSync(os.Stdout),
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

func DefaultLoggerConfig() config.LoggerConfig {
	return config.LoggerConfig{
		Level: "DEBUG",
	}
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
	sugar.Fatalf(msg, args...)
}
