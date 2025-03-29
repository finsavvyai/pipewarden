package logging

import (
	"fmt"

	"github.com/finsavvyai/pipewarden/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is our custom logger
type Logger struct {
	*zap.SugaredLogger
}

// New creates a new logger
func New(cfg *config.LoggingConfig) (*Logger, error) {
	// Determine log level
	level, err := getLogLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	// Create logger config
	zapConfig := zap.NewProductionConfig()
	if !cfg.JSON {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	zapConfig.Level = zap.NewAtomicLevelAt(level)

	// Build logger
	logger, err := zapConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	return &Logger{
		SugaredLogger: logger.Sugar(),
	}, nil
}

// getLogLevel converts string level to zapcore.Level
func getLogLevel(levelStr string) (zapcore.Level, error) {
	switch levelStr {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("invalid log level: %s", levelStr)
	}
}
