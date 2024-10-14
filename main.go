package main

import (
	"context"
	"os"
	parser "resource-tree-handler/internal/helpers/configuration"
	"resource-tree-handler/internal/webservice"

	"github.com/rs/zerolog"
)

//const configFilePathDefault = "/config.yaml"

func main() {
	configuration, err := parser.ParseConfig()
	if err != nil {
		configuration.Default()
	}

	// Logger configuration
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(configuration.DebugLevel)

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ctx := logger.WithContext(context.Background())

	if err != nil {
		zerolog.Ctx(ctx).Error().Err(err).Msg("configuration missing")
		zerolog.Ctx(ctx).Info().Msg("using default configuration for webservice")
	}

	// Kubernetes configuration
	/*cfg, err := rest.InClusterConfig()
	if err != nil {
		zerolog.Ctx(ctx).Error().Err(err).Msg("resolving kubeconfig for rest client")
	}*/

	// Configure etcd
	// TODO ...

	webservice.Spinup(configuration.WebServicePort)
}
