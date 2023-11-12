package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/alecthomas/kong"
	"github.com/bloxapp/ssv-rewards/pkg/rewards"
	"github.com/joho/godotenv"
	"github.com/mattn/go-colorable"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Globals struct {
	LogLevel string `env:"LOG_LEVEL" enum:"debug,info,warn,error" default:"info"                                                            help:"Log level."`
	Postgres string `env:"POSTGRES"                               default:"postgres://user:1234@localhost:5432/ssv-rewards?sslmode=disable" help:"PostgreSQL connection string."`
}

type CLI struct {
	Globals
	Sync SyncCmd `cmd:"" help:"Syncs historical data necessary to calculate rewards."`
	Calc CalcCmd `cmd:"" help:"Calculates rewards."`
}

func main() {
	// Parse .env file.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}

	// Parse CLI.
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("ssv-rewards"),
		kong.Description("Calculates SSV rewards."),
		kong.UsageOnError(),
		kong.Vars{
			"version": "0.0.1",
		},
	)

	// Setup logger.
	logLevel, err := zapcore.ParseLevel(cli.Globals.LogLevel)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to parse log level: %w", err))
	}
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(colorable.NewColorableStdout()),
		logLevel,
	))

	// Parse the rewards plan.
	data, err := os.ReadFile("rewards.yaml")
	if err != nil {
		logger.Fatal("failed to read rewards.yaml", zap.Error(err))
	}
	plan, err := rewards.ParseYAML(data)
	if err != nil {
		logger.Fatal("failed to parse rewards plan", zap.Error(err))
	}

	// Connect to the PostgreSQL database.
	db, err := sql.Open("postgres", cli.Globals.Postgres)
	if err != nil {
		logger.Fatal("failed to connect to PostgreSQL", zap.Error(err))
	}
	logger.Info("Connected to PostgreSQL")

	// Run the CLI.
	err = ctx.Run(logger, db, plan)
	ctx.FatalIfErrorf(err)
}
