package parser

import (
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
)

// type configurationNotParsed struct {
// 	WebServicePort int `json:"webServicePort" yaml:"webServicePort"`
// 	EtcdAddress    string                    `json:"etcdAddress" yaml:"etcdAddress"`
// 	EtcdPort       string                    `json:"etcdPort" yaml:"etcdPort"`
// 	EtcdUsername   string                    `json:"etcdUsername" yaml:"etcdUsername"`
// 	EtcdPassword   secrets.SecretKeySelector `json:"etcdPassword" yaml:"etcdPassword"`
// }

type Configuration struct {
	WebServicePort int           `json:"webServicePort" yaml:"webServicePort"`
	DebugLevel     zerolog.Level `json:"debugLevel" yaml:"debugLevel"`
	// EtcdAddress    string `json:"etcdAddress" yaml:"etcdAddress"`
	// EtcdPort       string `json:"etcdPort" yaml:"etcdPort"`
	// EtcdUsername   string `json:"etcdUsername" yaml:"etcdUsername"`
	// EtcdPassword   string `json:"etcdPassword" yaml:"etcdPassword"`
}

func (c *Configuration) Default() {
	c.WebServicePort = 8084
	c.DebugLevel = zerolog.InfoLevel
}

func ParseConfig() (Configuration, error) {
	port, err := strconv.Atoi(os.Getenv("RESOURCE_TREE_HANDLER_PORT"))
	if err != nil {
		return Configuration{}, err
	}

	switch strings.ToLower(os.Getenv("DEBUG_LEVEL")) {
	case "debug":
		return Configuration{WebServicePort: port, DebugLevel: zerolog.DebugLevel}, nil
	case "info":
		return Configuration{WebServicePort: port, DebugLevel: zerolog.InfoLevel}, nil
	case "error":
		return Configuration{WebServicePort: port, DebugLevel: zerolog.ErrorLevel}, nil
	}
	return Configuration{WebServicePort: port, DebugLevel: zerolog.DebugLevel}, nil
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
