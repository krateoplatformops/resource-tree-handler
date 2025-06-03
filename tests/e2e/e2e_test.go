package test

import (
	"context"
	"fmt"
	"io"
	"os"
	"resource-tree-handler/apis"
	"resource-tree-handler/internal/cache"
	"resource-tree-handler/internal/helpers/configuration"
	"resource-tree-handler/internal/helpers/kube/client"
	"resource-tree-handler/internal/ssemanager"
	"resource-tree-handler/internal/webservice"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	e2eutils "sigs.k8s.io/e2e-framework/pkg/utils"
	"sigs.k8s.io/e2e-framework/support/kind"
)

type contextKey string

var (
	testenv env.Environment
)

const (
	testNamespace   = "resource-tree-handler-test"
	crdsPath        = "./manifests/crds"
	deploymentsPath = "./manifests/deployments"
	toTest          = "./manifests/to_test"

	testName       = "test-2905"
	testResource   = "applicationgroups"
	testKind       = "ApplicationGroup"
	testApiVersion = "composition.krateo.io/v1"
)

func TestMain(m *testing.M) {
	testenv = env.New()
	kindClusterName := "krateo-test"

	// Use pre-defined environment funcs to create a kind cluster prior to test run
	testenv.Setup(
		envfuncs.CreateCluster(kind.NewProvider(), kindClusterName),
		envfuncs.CreateNamespace(testNamespace),
		envfuncs.SetupCRDs(crdsPath, "*"),
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			// setup Krateo's helm
			if p := e2eutils.RunCommand("helm repo add krateo https://charts.krateo.io"); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while adding repository: %s %v", p.Out(), p.Err())
			}

			if p := e2eutils.RunCommand("helm repo update krateo"); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while updating helm: %s %v", p.Out(), p.Err())
			}

			// install nothing else
			return ctx, nil
		},
	)

	// Use pre-defined environment funcs to teardown kind cluster after tests
	testenv.Finish(
		envfuncs.DeleteNamespace(testNamespace),
		envfuncs.TeardownCRDs(crdsPath, "*"),
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			// nothing to cleanup
			return ctx, nil
		},
		envfuncs.DestroyCluster(kindClusterName),
	)

	// launch package tests
	os.Exit(testenv.Run(m))
}

func TestResourceTreeHandler(t *testing.T) {
	createSingle := features.New("Create single").
		WithLabel("type", "Resource tree").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r, err := resources.New(c.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}

			// Start resource tree handler
			go startTestManager(c.Client().RESTConfig())

			ctx = context.WithValue(ctx, contextKey("client"), r)

			err = decoder.DecodeEachFile(
				ctx, os.DirFS(deploymentsPath), "*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			err = decoder.DecodeEachFile(
				ctx, os.DirFS(toTest), "*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			time.Sleep(5 * time.Second)
			return ctx
		}).
		Assess("Value", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			portNumber := 8085 // default for resource-tree-handler

			unstructuredObj, err := client.GetObj(ctx, &apis.Reference{
				ApiVersion: testApiVersion,
				Kind:       testKind,
				Resource:   testResource,
				Name:       testName,
				Namespace:  testNamespace,
			}, c.Client().RESTConfig())
			if err != nil {
				t.Fatal("could not get ApplicationGroup CR")
			}

			unstructured.SetNestedSlice(unstructuredObj.Object, []interface{}{
				map[string]interface{}{
					"lastTransitionTime": "2025-05-30T14:34:04Z",
					"message":            "Composition values updated.",
					"reason":             "Available",
					"status":             "True",
					"type":               "Ready",
				},
			}, "status", "conditions")

			unstructured.SetNestedSlice(unstructuredObj.Object, []interface{}{
				map[string]interface{}{
					"apiVersion": "resourcetrees.krateo.io/v1",
					"name":       testName,
					"namespace":  testNamespace,
					"resource":   "compositionreferences",
				},
			}, "status", "managed")

			dynClient, err := client.NewDynamicClient(c.Client().RESTConfig())
			if err != nil {
				t.Fatal("error while creating dynClient", err)
			}
			updateStatus(ctx, unstructuredObj, "applicationgroups", dynClient)

			log.Debug().Msgf("curl -s %s:%d/compositions/%s", "localhost", portNumber, unstructuredObj.GetUID())

			predictedOutput1 := `[{"version":"resourcetrees.krateo.io/v1","kind":"CompositionReference","namespace":"resource-tree-handler-test","name":"test-2905","parentRefs":[{}]`
			predictedOutput2 := `{"version":"resourcetrees.krateo.io/v1","kind":"CompositionReference","namespace":"resource-tree-handler-test","name":"test-2905",`
			resultString := `{"message":"Job for composition a2567429-b648-427d-946e-949c0d57b612 has been queued"}`

			for strings.Contains(resultString, "has been queued") {

				p := e2eutils.RunCommand(fmt.Sprintf("curl -s %s:%d/compositions/%s", "localhost", portNumber, unstructuredObj.GetUID()))
				if p.Err() != nil {
					t.Fatal(fmt.Errorf("error with curl: %s %v", p.Out(), p.Err()))
				}

				// Now the resource-tree-handler will create the resource tree
				resultStringBuilder := new(strings.Builder)
				_, err = io.Copy(resultStringBuilder, p.Out())
				if err != nil {
					t.Fatal(err)
				}
				resultString = resultStringBuilder.String()

				time.Sleep(5 * time.Second)
			}
			if !strings.Contains(strings.Replace(resultString, " ", "", -1), strings.Replace(predictedOutput1, " ", "", -1)) || !strings.Contains(strings.Replace(resultString, " ", "", -1), strings.Replace(predictedOutput2, " ", "", -1)) {
				log.Error().Msg("unexpected resource tree")
				log.Error().Msgf("expected output should contain 1: %s", predictedOutput1)
				log.Error().Msgf("expected output should contain 2: %s", predictedOutput2)
				log.Error().Msgf("actual output: %s", resultString)
				t.Fatal(fmt.Errorf("unexpected output"))
			}
			return ctx
		}).Feature()

	// test feature
	testenv.Test(t, createSingle)
}

// startTestManager starts the controller manager with the given config
func startTestManager(config *rest.Config) error {
	configuration, err := configuration.ParseConfig()
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

	// Kubernetes configuration from parameter

	// Initialize cache object
	cache := cache.NewThreadSafeCache()

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
		SSE:            sse,
	}
	w.Spinup() // blocks main thread
	return nil
}

func updateStatus(ctx context.Context, objToUpdate *unstructured.Unstructured, Resource string, dynClient *dynamic.DynamicClient) error {
	gv, _ := schema.ParseGroupVersion(objToUpdate.GetAPIVersion())
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: Resource,
	}
	_, err := dynClient.Resource(gvr).Namespace(objToUpdate.GetNamespace()).UpdateStatus(ctx, objToUpdate, metav1.UpdateOptions{})
	return err
}
