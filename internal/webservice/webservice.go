package webservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	homeAddress    = "/"
	requestAddress = "/compositions/:compositionId"
)

func debugLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Log request
		requestDump, err := httputil.DumpRequest(c.Request, true)
		if err != nil {
			log.Error().Err(err).Msg("Failed to dump request")
		} else {
			log.Debug().Msgf("Incoming request:\n%s", string(requestDump))
		}

		// Create a response writer that captures the response
		blw := &bodyLogWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = blw

		// Process request
		c.Next()

		// Log response
		responseDump := fmt.Sprintf("HTTP/1.1 %d %s\n", c.Writer.Status(), http.StatusText(c.Writer.Status()))
		for k, v := range c.Writer.Header() {
			responseDump += fmt.Sprintf("%s: %s\n", k, v[0])
		}
		responseDump += "\n" + blw.body.String()

		log.Debug().Msgf("Outgoing response:\n%s", responseDump)
	}
}

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func handleHome(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleRequest(c *gin.Context) {
	compositionId := c.Param("compositionId")
	switch c.Request.Method {
	case http.MethodGet:
		err := handleGet(c, compositionId)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Error parsing GET request: %s", err)})
		}
	case http.MethodDelete:
		err := handleDelete(c, compositionId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error parsing DELETE request: %s", err)})
		}
	case http.MethodPost:
		err := handlePost(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error parsing POST request: %s", err)})
		}
	default:
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "Method not allowed"})
	}
}

func handleGet(c *gin.Context, compositionId string) error {
	resourceTreeString, ok := GetFromCache(compositionId)
	if !ok {
		return fmt.Errorf("could not find resource tree for CompositionId %s", compositionId)
	}
	c.JSON(http.StatusOK, resourceTreeString)
	return nil
}

func handleDelete(c *gin.Context, compositionId string) error {
	DeleteFromCache(compositionId)
	c.String(http.StatusOK, "DELETE for CompositionId %s executed", compositionId)
	return nil
}

func handlePost(c *gin.Context) error {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return fmt.Errorf("error reading request body: %w", err)
	}
	defer c.Request.Body.Close()

	var data ResourceTree
	err = json.Unmarshal(body, &data)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %w", err)
	}

	resourceTreeJsonStatus, err := json.Marshal(data.Resources.Status)
	if err != nil {
		return fmt.Errorf("error marshaling resource tree into JSON: %w", err)
	}

	AddToCache(string(resourceTreeJsonStatus), data.CompositionId)
	c.String(http.StatusOK, string(resourceTreeJsonStatus))
	return nil
}

func Spinup(webservicePort int) {
	var r *gin.Engine
	// gin.New() instead of gin.Default() to avoid default logging
	if zerolog.GlobalLevel() == zerolog.DebugLevel {
		r = gin.New()
		r.Use(gin.Recovery())
		r.Use(debugLoggerMiddleware())
	} else {
		gin.SetMode(gin.ReleaseMode)
		r = gin.Default()
	}

	r.GET(homeAddress, handleHome)
	r.Any(requestAddress, handleRequest)
	r.Run(fmt.Sprintf(":%d", webservicePort))
}
