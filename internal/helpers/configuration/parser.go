package parser

import (
	"context"
	"io"
	"os"
	"resource-tree-handler/internal/helpers/kube/secrets"

	"gopkg.in/yaml.v2"
	"k8s.io/client-go/rest"
)

type configurationNotParsed struct {
	CacheInvalidationPeriodSeconds int                       `json:"cacheInvalidationPeriodSeconds" yaml:"CacheInvalidationPeriodSeconds"`
	WebServicePort                 int                       `json:"webServicePort" yaml:"webServicePort"`
	EtcdAddress                    string                    `json:"etcdAddress" yaml:"etcdAddress"`
	EtcdPort                       string                    `json:"etcdPort" yaml:"etcdPort"`
	EtcdUsername                   string                    `json:"etcdUsername" yaml:"etcdUsername"`
	EtcdPassword                   secrets.SecretKeySelector `json:"etcdPassword" yaml:"etcdPassword"`
}

type Configuration struct {
	CacheInvalidationPeriodSeconds int    `json:"cacheInvalidationPeriodSeconds" yaml:"CacheInvalidationPeriodSeconds"`
	WebServicePort                 int    `json:"webServicePort" yaml:"webServicePort"`
	EtcdAddress                    string `json:"etcdAddress" yaml:"etcdAddress"`
	EtcdPort                       string `json:"etcdPort" yaml:"etcdPort"`
	EtcdUsername                   string `json:"etcdUsername" yaml:"etcdUsername"`
	EtcdPassword                   string `json:"etcdPassword" yaml:"etcdPassword"`
}

func (c *Configuration) Default() {
	c.CacheInvalidationPeriodSeconds = 300
	c.WebServicePort = 8084
}

func ParseConfigFile(ctx context.Context, rc *rest.Config, filePath string) (Configuration, error) {
	fileReader, err := os.OpenFile(filePath, os.O_RDONLY, 0600)
	if err != nil {
		return Configuration{}, err
	}
	defer fileReader.Close()
	data, err := io.ReadAll(fileReader)
	if err != nil {
		return Configuration{}, err
	}

	parse := configurationNotParsed{}

	err = yaml.Unmarshal(data, &parse)
	if err != nil {
		return Configuration{}, err
	}

	secret, err := secrets.Get(ctx, rc, (*secrets.SecretKeySelector)(&parse.EtcdPassword))
	if err != nil {
		return Configuration{}, err
	}

	result := Configuration{
		CacheInvalidationPeriodSeconds: parse.CacheInvalidationPeriodSeconds,
		WebServicePort:                 parse.WebServicePort,
		EtcdAddress:                    parse.EtcdAddress,
		EtcdPort:                       parse.EtcdPort,
		EtcdUsername:                   parse.EtcdUsername,
		EtcdPassword:                   string(secret.Data[parse.EtcdPassword.Key]),
	}

	return result, nil
}
