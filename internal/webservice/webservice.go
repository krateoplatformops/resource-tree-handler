package webservice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog"
)

const (
	homeAddress    = "/"
	requestAddress = "/compositions/"
)

func handleHome(w http.ResponseWriter, r *http.Request) {
	html := `<html><body>Resource Tree generator Web Service home page</body></html>`
	fmt.Fprint(w, html)
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ctx := logger.WithContext(r.Context())
	zerolog.Ctx(ctx).Info().Msgf("received request on endpoint: %s\nrequest type: %s", r.URL.Path, r.Method)

	// Extract compositionId from the URL
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 || parts[1] != "compositions" {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}
	compositionId := parts[2]

	switch r.Method {
	case http.MethodGet:
		err := handleGet(w, compositionId, ctx)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("error while managing GET request")
			http.Error(w, fmt.Sprintf("Error parsing GET request: %s", err), http.StatusNotFound)
		}
	case http.MethodDelete:
		err := handleDelete(w, compositionId, ctx)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("error while managing DELETE request")
			http.Error(w, fmt.Sprintf("Error parsing DELETE request: %s", err), http.StatusInternalServerError)
		}
	case http.MethodPost:
		err := handlePost(w, r, ctx)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("error while managing POST request")
			http.Error(w, fmt.Sprintf("Error parsing POST request: %s", err), http.StatusInternalServerError)
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGet(w http.ResponseWriter, compositionId string, ctx context.Context) error {
	zerolog.Ctx(ctx).Info().Msgf("GET request for CompositionId: %s", compositionId)
	resourceTreeString, ok := GetFromCache(compositionId)
	if !ok {
		return fmt.Errorf("could not find resource tree for CompositionId %s", compositionId)
	}

	fmt.Fprintf(w, "%s", resourceTreeString)
	return nil
}

func handleDelete(w http.ResponseWriter, compositionId string, ctx context.Context) error {
	zerolog.Ctx(ctx).Info().Msgf("DELETE request for CompositionId: %s", compositionId)
	DeleteFromCache(compositionId)
	fmt.Fprintf(w, "DELETE request for CompositionId %s", compositionId)
	return nil
}

func handlePost(w http.ResponseWriter, r *http.Request, ctx context.Context) error {
	zerolog.Ctx(ctx).Info().Msg("POST request received")

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("error reading request body: %w", err)
	}
	defer r.Body.Close()

	// Parse JSON
	var data ResourceTree
	err = json.Unmarshal(body, &data)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %w", err)
	}

	resourceTreeJson, err := json.Marshal(data.Resources)
	if err != nil {
		return fmt.Errorf("error marshaling resource tree into JSON: %w", err)
	}

	AddToCache(string(resourceTreeJson), data.CompositionId)
	fmt.Fprint(w, string(resourceTreeJson))
	return nil
}

func Spinup(webservicePort int) {
	http.HandleFunc(homeAddress, handleHome)
	http.HandleFunc(requestAddress, handleRequest)
	http.ListenAndServe(fmt.Sprintf(":%d", webservicePort), nil)
}
