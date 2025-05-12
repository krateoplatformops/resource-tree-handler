package configuration

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
)

type Configuration struct {
	WebServicePort int           `json:"webServicePort" yaml:"webServicePort"`
	SSEUrl         string        `json:"sseURL" yaml:"sseURL"`
	DebugLevel     zerolog.Level `json:"debugLevel" yaml:"debugLevel"`
}

func (c *Configuration) Default() {
	c.WebServicePort = 8085
	c.DebugLevel = zerolog.DebugLevel
}

func ParseConfig() (Configuration, error) {
	port, err := strconv.Atoi(os.Getenv("RESOURCE_TREE_HANDLER_API_PORT"))
	if err != nil {
		return Configuration{}, err
	}

	sseUrl := os.Getenv("URL_SSE")
	if sseUrl == "" {
		return Configuration{}, fmt.Errorf("SSE URL cannot be empty")
	}

	debugLevel := zerolog.InfoLevel
	switch strings.ToLower(os.Getenv("DEBUG_LEVEL")) {
	case "debug":
		debugLevel = zerolog.DebugLevel
	case "info":
		debugLevel = zerolog.InfoLevel
	case "error":
		debugLevel = zerolog.ErrorLevel
	}
	return Configuration{
		WebServicePort: port,
		SSEUrl:         sseUrl,
		DebugLevel:     debugLevel,
	}, nil
}

// func ParseConfigFile(ctx context.Context, rc *rest.Config, filePath string) (Configuration, error) {
// 	fileReader, err := os.OpenFile(filePath, os.O_RDONLY, 0600)
// 	if err != nil {
// 		return Configuration{}, err
// 	}
// 	defer fileReader.Close()
// 	data, err := io.ReadAll(fileReader)
// 	if err != nil {
// 		return Configuration{}, err
// 	}

// 	parse := configurationNotParsed{}

// 	err = yaml.Unmarshal(data, &parse)
// 	if err != nil {
// 		return Configuration{}, err
// 	}

// 	secret, err := secrets.Get(ctx, rc, (*secrets.SecretKeySelector)(&parse.EtcdPassword))
// 	if err != nil {
// 		return Configuration{}, err
// 	}

// 	result := Configuration{
// 		WebServicePort: parse.WebServicePort,
// 		EtcdAddress:    parse.EtcdAddress,
// 		EtcdPort:       parse.EtcdPort,
// 		EtcdUsername:   parse.EtcdUsername,
// 		EtcdPassword:   string(secret.Data[parse.EtcdPassword.Key]),
// 	}

// 	return result, nil
// }
