package main

import (
	"bytes"
	"os"
	"time"

	"github.com/evbuehl/livestreamScheduler/lib/googleApi"
	"github.com/xtfly/log4g"
	"github.com/xtfly/log4g/api"
	"gopkg.in/yaml.v3"
)

type livestreamConfigYaml struct {
	Title       string   `yaml:"Title"`
	Category    string   `yaml:"Category"`
	PlaylistIDs []string `yaml:"PlaylistIDs"`
}

type livestreamConfig struct {
	livestreamConfigYaml
	BroadcastID string
	Date        string
	Category    string
	PlaylistIDs []string
	Thumbnail   string
}

type thumbnails struct {
	Queue string `yaml:"Queue"`
	Done  string `yaml:"Done"`
}

type configYaml struct {
	LogLevel         string               `yaml:"LogLevel"`
	MailLevel        string               `yaml:"MailLevel"`
	MailAddress      string               `yaml:"MailAddress"`
	CreationDistance string               `yaml:"CreationDistance"`
	RegionCode       string               `yaml:"RegionCode"`
	Thumbnails       thumbnails           `yaml:"Thumbnails"`
	Template         livestreamConfigYaml `yaml:"Template"`
}

type configStruct struct {
	configYaml
	CreationDistance time.Duration
	Template         livestreamConfig
}

var youtubeCategoryMap = map[string]string{}

func getCategoryMap() error {
	call := googleApi.YoutubeService.VideoCategories.List([]string{"snippet"}).RegionCode(config.RegionCode)

	if response, err := call.Do(); err != nil {
		return err
	} else {
		for _, c := range response.Items {
			youtubeCategoryMap[c.Snippet.Title] = c.Id
		}

		return nil
	}
}

func loadYaml() configYaml {
	config := configYaml{}

	yamlFile, err := os.ReadFile("config.yaml")
	if err != nil {
		logger.Critical("Error opening config-file: %q", err)
		panic(err)
	}

	reader := bytes.NewReader(yamlFile)

	dec := yaml.NewDecoder(reader)
	dec.KnownFields(true)
	err = dec.Decode(&config)
	if err != nil {
		logger.Critical("Error parsing config-file: %v", err)
		panic(err)
	}

	return config
}

func loadConfig(config configYaml) configStruct {
	duration, err := time.ParseDuration(config.CreationDistance)

	if err != nil {
		logger.Critical("can't parse CreationDistance %v", err)

		panic(err)
	}

	return configStruct{
		configYaml:       config,
		CreationDistance: duration,
		Template: livestreamConfig{
			livestreamConfigYaml: config.Template,
			Category:             youtubeCategoryMap[config.Template.Category],
		},
	}
}

var config configStruct

var logger api.Logger

func init() {
	configYaml := loadYaml()

	// initialize the logger
	cfg := &api.Config{
		Loggers: []api.CfgLogger{
			{Name: "root", Level: configYaml.LogLevel, OutputNames: []string{"console", "log", "mail"}},
		},
		Formats: []api.CfgFormat{
			{"type": "text", "name": "std", "layout": "[%{time}] [%{level}] >> %{msg}\n"},
		},
		Outputs: []api.CfgOutput{
			{"type": "console", "name": "console", "format": "std"},
			{"type": "time_rolling_file", "name": "log", "file": "logs/livestreamScheduler.log", "pattern": "2006-01-02", "backups": "7", "format": "std"},
			{"type": "size_rolling_file", "name": "mail", "file": "mail.log", "format": "std", "threshold": configYaml.MailLevel},
		},
	}

	if err := log4g.GetManager().SetConfig(cfg); err != nil {
		panic(err)
	}

	logger = log4g.GetLogger("default")

	// get the youtube category map
	getCategoryMap()

	// now parse the configYaml
	config = loadConfig(configYaml)
}
