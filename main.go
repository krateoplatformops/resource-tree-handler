package main

import (
	"os"
	cacheHelper "resource-tree-handler/internal/cache"
	parser "resource-tree-handler/internal/helpers/configuration"
	kubeHelper "resource-tree-handler/internal/helpers/kube/client"
	"resource-tree-handler/internal/ssemanager"
	"resource-tree-handler/internal/webservice"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/rest"
)

func main() {
	configuration, err := parser.ParseConfig()
	if err != nil {
		configuration.Default()
	}

	// Logger configuration
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(configuration.DebugLevel)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if err != nil {
		log.Error().Err(err).Msg("configuration missing")
		log.Info().Msg("using default configuration for webservice")
	}

	log.Debug().Msg("List of environment variables:")
	for _, s := range os.Environ() {
		log.Debug().Msg(s)
	}

	kubeHelper.PLURALIZER_URL = configuration.PluralizerUrl

	// Kubernetes configuration
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Error().Err(err).Msg("resolving kubeconfig for rest client")
		return
	}

	dynClient, err := kubeHelper.NewDynamicClient(config)
	if err != nil {
		log.Error().Err(err).Msg("obtaining dynamic client for kubernetes")
		return
	}

	// Initialize cache object
	cache := cacheHelper.NewThreadSafeCache()

	// Start client to receive SSE events from eventsse
	log.Info().Msgf("starting SSE client on %s", configuration.SSEUrl)
	sse := &ssemanager.SSE{
		Config: config,
		Cache:  cache,
	}
	sse.Spinup(configuration.SSEUrl) // only initialization and go routines, non-blocking

	// // Start webservice to serve endpoints
	w := webservice.Webservice{
		Config:         config,
		WebservicePort: configuration.WebServicePort,
		Cache:          cache,
		DynClient:      dynClient,
		SSE:            sse,
	}
	w.Spinup() // blocks main thread
}
