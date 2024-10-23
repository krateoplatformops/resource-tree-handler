package webservice

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httputil"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func debugLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Log request
		requestIdentifier := c.Request.Method + " " + c.Request.RequestURI
		requestDump, err := httputil.DumpRequest(c.Request, true)
		if err != nil {
			log.Error().Err(err).Msg("Failed to dump request")
		} else {
			log.Debug().Msgf("Incoming request on %s:\n%s", requestIdentifier, string(requestDump))
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

		log.Debug().Msgf("Outgoing response for %s:\n%s", requestIdentifier, responseDump)
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
