package config

import (
	"io/ioutil"
	"log"

	"github.com/k0kubun/pp"
	yaml "gopkg.in/yaml.v2"
)

type GeneratorConfig struct {
	Name    string
	Context map[string]string
	Modules []ModuleInstance
}

type ModuleInstance struct {
	Source    string
	Output    string
	Overwrite bool
	Params    map[string]string
}

func LoadGeneratorConfig(filePath string) *GeneratorConfig {
	config := &GeneratorConfig{}

	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Panicf("failed to read config: %v", err)
	}
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Panicf("failed to parse config: %v", err)
	}

	pp.Println("Generator Config:", config)
	return config
}