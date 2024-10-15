package main

import (
	"os"
	parser "resource-tree-handler/internal/helpers/configuration"
	"resource-tree-handler/internal/webservice"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Debug().Msg("List of environment variables:")
	for _, s := range os.Environ() {
		log.Debug().Msg(s)
	}

	if err != nil {
		log.Error().Err(err).Msg("configuration missing")
		log.Info().Msg("using default configuration for webservice")
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
