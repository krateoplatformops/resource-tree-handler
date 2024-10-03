package main

import (
	"context"
	"os"
	parser "resource-tree-handler/internal/helpers/configuration"
	"resource-tree-handler/internal/webservice"

	"github.com/rs/zerolog"
	"k8s.io/client-go/rest"
)

const configFilePathDefault = "/config.yaml"

func main() {
	// Logger configuration
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ctx := logger.WithContext(context.Background())

	// Kubernetes configuration
	cfg, err := rest.InClusterConfig()
	if err != nil {
		zerolog.Ctx(ctx).Error().Err(err).Msg("resolving kubeconfig for rest client")
	}

	// Parse webservice configuration
	configFilePath := os.Getenv("CONFIG_PATH")
	if configFilePath == "" {
		configFilePath = configFilePathDefault
	}
	configuration, err := parser.ParseConfigFile(ctx, cfg, configFilePath)
	if err != nil {
		zerolog.Ctx(ctx).Error().Err(err).Msg("configuration parsing missing")
		zerolog.Ctx(ctx).Info().Msg("using default configuration for webservice")
		configuration.Default()
	}

	// Configure Cache
	// TODO ...

	// Configure etcd
	// TODO ...

	webservice.Spinup(configuration.WebServicePort)
}
